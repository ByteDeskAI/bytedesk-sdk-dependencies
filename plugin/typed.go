package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// Typed access to the untyped Host (ADR 0025 §7).
//
// Go methods cannot have type parameters, so Host.Request can never be generic.
// The typed layer is therefore free functions wrapping Host: it adds no Host
// method, keeps every out-of-process plugin source-compatible, and leaves
// TestHostMethodSetIsPinned untouched.
//
// There are two layers here and they are not alternatives:
//
//   - The mechanism — Descriptor, Invoke, Observe, Publish, HandleRaw. A union
//     is closed to the package that declares it, so a consumer package cannot
//     join plugin.Payload without plugin importing it back. The mechanism is
//     what a consumer's own generated wrappers are built over, and it is why no
//     import cycle is possible: plugin imports nothing of a consumer's.
//   - The typed API for contracts whose types live HERE — Command, Event, Call,
//     Emit, On, Handle, constrained to plugin.Payload. Host-exposed contracts
//     are host-classified; a plugin's own payloads are the plugin's data.
//
// The typed API is implemented on the mechanism, so there is one code path.
// Neither layer is author-facing untyped: an author calls Call, or their own
// package's generated Call. Invoke's any is the marshalling boundary that
// already exists inside Call — json.Marshal takes any — and generated wrappers
// are its only callers.
//
// There is deliberately NO exported untyped call. An operation whose payloads
// this package does not classify is reached by generating that package's own
// wrappers, not by reaching for an escape hatch: an exported one would be the
// path of least resistance and would silently give up the classification
// guarantee at every call site that took it.
//
// Descriptors are opaque. Command, Event and Descriptor carry unexported name,
// rev and schemaHash and are built only by the constructors generated code
// calls, so a call site cannot hand-assemble Command{Name: "cmd.files.v1.delete"}
// and cannot type an operation name at all. Stated plainly so nobody
// over-trusts it: that stops drift, it is not a security boundary. The
// constructors are exported and callable — they must be, since generated code
// lives in another package. Security is host-side — the operation must be
// eligible, within ceiling and consent, and its schema hash must match what the
// host registered — so a forged descriptor buys nothing.

// Reserved envelope headers. The bd- prefix belongs to the host: a plugin
// cannot set one, and the host stamps caller identity into them. Headers
// already count against the 64 KiB envelope budget and the 256-header cap, so
// carrying the schema hash here costs nothing new.
const (
	// HeaderSchema carries the per-operation schema hash, checked before the
	// payload is unmarshalled so a mismatched payload is never decoded.
	HeaderSchema = "bd-schema"
	// HeaderCaller and HeaderGeneration are the mandatory workload identity.
	HeaderCaller     = "bd-caller"
	HeaderGeneration = "bd-generation"
	// HeaderSubject is the optional subject lease: an opaque host-owned id the
	// plugin may compare for equality and nothing else. Absent means the call
	// is autonomous, running under workload identity alone.
	HeaderSubject = "bd-subject"
	// HeaderFault carries a typed fault code from a migrated host.
	HeaderFault = "bd-fault"
)

// Fault codes. A caller distinguishes denied from budget-exhausted from
// withdrawn instead of string-matching the host's error text (ADR 0025 §8).
const (
	FaultDenied      = "denied"
	FaultBudget      = "budget"
	FaultWithdrawn   = "withdrawn"
	FaultSchema      = "schema"
	FaultUnhandled   = "unhandled"
	FaultTimeout     = "timeout"
	FaultUnavailable = "unavailable"
)

// Fault is a typed host error. Match it with errors.As and switch on Code.
type Fault struct {
	Code    string
	Op      string
	Message string
	Err     error
}

func (f Fault) Error() string {
	parts := make([]string, 0, 3)
	if f.Op != "" {
		parts = append(parts, f.Op)
	}
	parts = append(parts, f.Code)
	if f.Message != "" {
		parts = append(parts, f.Message)
	}
	return strings.Join(parts, ": ")
}

func (f Fault) Unwrap() error { return f.Err }

// Caller is the principal pair a handler sees: mandatory workload identity, and
// an optional subject lease. A subject-scoped operation invoked with no lease
// must be refused; an autonomous-eligible one proceeds under workload identity
// alone, and its rule sees an empty SubjectLease.
type Caller struct {
	PluginID     string
	Generation   string
	SubjectLease string
}

// Autonomous reports that no human principal is attached to this call.
func (c Caller) Autonomous() bool { return c.SubjectLease == "" }

// Descriptor is an opaque, generated operation identity. A consumer package
// builds its own typed wrappers over one; nothing hand-assembles it, because
// every field is unexported.
type Descriptor struct {
	name       string
	rev        uint32
	schemaHash string
}

// NewDescriptor is called by generated code only. It is exported because that
// generated code lives in another package.
//
// A hand-built descriptor with an empty hash is not refused here — a panic in a
// constructor that generated code calls at package init is a bad failure mode
// for a class the guards already fail-close. Such a descriptor stamps no header
// and is refused by any host that stamps one, in all three directions. See
// TestDescriptorsCannotSkipSchemaVerification.
func NewDescriptor(name string, rev uint32, schemaHash string) Descriptor {
	return Descriptor{name: name, rev: rev, schemaHash: schemaHash}
}

// Name is for registry keys, logs and diagnostics. It does not let a caller
// reconstruct a descriptor: rev and schemaHash stay unexported.
func (d Descriptor) Name() string { return d.name }

// Invoke is the untyped mechanism, and is NOT the author-facing API. Generated
// wrappers call it; an author calls their package's generated Call.
//
// req is marshalled, resp is unmarshalled into, and the schema hash travels in
// a reserved header so the host can refuse a mismatch before it decodes.
func Invoke(ctx context.Context, h Host, d Descriptor, req any, resp any) error {
	if h == nil {
		return Fault{Code: FaultUnavailable, Op: d.name, Message: "no host"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Fault{Code: FaultSchema, Op: d.name, Message: "request does not marshal", Err: err}
	}
	reply, err := h.Request(ctx, envelopeFor(d, body))
	if err != nil {
		return faultFrom(d.name, reply.Headers[HeaderFault], err)
	}
	if code := reply.Headers[HeaderFault]; code != "" {
		return Fault{Code: code, Op: d.name, Message: string(reply.Payload)}
	}
	// Checked when present rather than required: a host that does not yet
	// stamp the header is unmigrated, not incompatible. A present mismatch is
	// a different contract and fails the call rather than mis-decoding it —
	// including when this descriptor carries no hash at all, which is what
	// stops a hand-built one from skipping verification.
	if got := reply.Headers[HeaderSchema]; got != "" && got != d.schemaHash {
		return Fault{Code: FaultSchema, Op: d.name, Message: fmt.Sprintf("reply schema %q does not match %q", got, d.schemaHash)}
	}
	if resp == nil {
		return nil
	}
	if err := json.Unmarshal(reply.Payload, resp); err != nil {
		return Fault{Code: FaultSchema, Op: d.name, Message: "reply does not decode", Err: err}
	}
	return nil
}

// Publish is the fire-and-forget event equivalent of Invoke.
func Publish(h Host, d Descriptor, v any) error {
	if h == nil {
		return Fault{Code: FaultUnavailable, Op: d.name, Message: "no host"}
	}
	body, err := json.Marshal(v)
	if err != nil {
		return Fault{Code: FaultSchema, Op: d.name, Message: "payload does not marshal", Err: err}
	}
	return h.Publish(envelopeFor(d, body))
}

// Observe subscribes to an operation's events and hands the raw payload to fn,
// which a generated wrapper decodes. It degrades to Host.Subscribe on a host
// that does not implement StatusSubscriber — where silence is indistinguishable
// from loss, so periodic cursor reconciliation is mandatory.
func Observe(h Host, d Descriptor, fn func(json.RawMessage)) (Subscription, error) {
	if h == nil || fn == nil {
		return nil, Fault{Code: FaultUnavailable, Op: d.name, Message: "nil host or handler"}
	}
	deliver := func(env bus.Envelope) {
		// A mismatched schema is a different contract revision, not this one.
		if got := env.Headers[HeaderSchema]; got != "" && got != d.schemaHash {
			return
		}
		fn(env.Payload)
	}
	if s, ok := h.(StatusSubscriber); ok {
		return s.SubscribeStatus(d.name, deliver)
	}
	return legacySubscription{cancel: h.Subscribe(d.name, deliver)}, nil
}

// HandleRaw is the server side of the mechanism. It owns the schema check;
// generated wrappers own the decode into the typed Req.
func HandleRaw(r *Registrar, d Descriptor, fn func(context.Context, Caller, json.RawMessage) (json.RawMessage, error)) {
	if r == nil || fn == nil {
		panic("plugin: HandleRaw needs a registrar and a handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.handlers[d.name]; dup {
		panic("plugin: duplicate handler for " + d.name)
	}
	r.handlers[d.name] = func(ctx context.Context, env bus.Envelope) (bus.Envelope, error) {
		// Before decode, per ADR 0025 §7: a mismatched payload is never
		// unmarshalled. Lenient decoding is never compatibility evidence.
		if got := env.Headers[HeaderSchema]; got != d.schemaHash {
			return bus.Envelope{}, Fault{Code: FaultSchema, Op: d.name,
				Message: fmt.Sprintf("schema %q does not match %q (revision %d)", got, d.schemaHash, d.rev)}
		}
		body, err := fn(ctx, CallerOf(env), env.Payload)
		if err != nil {
			return bus.Envelope{}, err
		}
		return bus.Envelope{
			Type:          env.Type,
			CorrelationID: env.ID,
			Headers:       map[string]string{HeaderSchema: d.schemaHash},
			Payload:       body,
		}, nil
	}
}

func envelopeFor(d Descriptor, body []byte) bus.Envelope {
	env := bus.Envelope{Type: d.name, Payload: body}
	// An empty hash stamps nothing rather than an empty header the host would
	// compare against a registered one.
	if d.schemaHash != "" {
		env.Headers = map[string]string{HeaderSchema: d.schemaHash}
	}
	return env
}

// Command names one request/response operation whose types live in this
// package. Built by generated code.
type Command[Req, Resp Payload] struct{ d Descriptor }

// NewCommand builds a command descriptor. Generated code calls this.
func NewCommand[Req, Resp Payload](name string, rev uint32, schemaHash string) Command[Req, Resp] {
	return Command[Req, Resp]{d: NewDescriptor(name, rev, schemaHash)}
}

// Name is for registry keys, logs and diagnostics. There is deliberately no
// Revision or SchemaHash accessor: together with Name they would round-trip
// straight back into NewDescriptor and undo the opacity. Admission keys by
// (name, revision, hash) host-side, where the caller is not trusted anyway.
func (c Command[Req, Resp]) Name() string { return c.d.name }

// Event names one published payload type of this package. Built by generated
// code.
type Event[T Payload] struct{ d Descriptor }

// NewEvent builds an event descriptor. Generated code calls this.
func NewEvent[T Payload](name string, rev uint32, schemaHash string) Event[T] {
	return Event[T]{d: NewDescriptor(name, rev, schemaHash)}
}

// Name is for registry keys, logs and diagnostics; see Command.Name.
func (e Event[T]) Name() string { return e.d.name }

// Subscription reports what a bare cancel function hides. Two failures are
// invisible without it, and the second is worse than the first: a busy
// subscription overflows the 128-deep callback queue and its stream is
// cancelled, and a registration past the 32-per-generation limit silently never
// happens at all — the host returns a no-op cancel without registering, so the
// plugin waits forever for events nobody is sending it.
//
// A consumer that observes Done with a loss error must reconcile by cursor
// before trusting subsequent events.
type Subscription interface {
	Cancel()
	Done() <-chan struct{}
	// Err is nil after Cancel, and reports the loss reason on overflow or
	// refusal.
	Err() error
}

// StatusSubscriber is optional, in the segregated style this SDK already uses
// for HTTPPlugin, Validator, Readier and Negotiator. It cannot live on Host:
// adding a return value to Subscribe would change the pinned Host method set
// and break every deployed out-of-process plugin. New direct and RPC hosts
// implement it; legacy hosts do not.
type StatusSubscriber interface {
	SubscribeStatus(eventType string, h func(bus.Envelope)) (Subscription, error)
}

// neverDone backs the legacy fallback: Done never closes, so a select on it
// blocks rather than reporting a loss that this host cannot detect.
var neverDone = make(chan struct{})

type legacySubscription struct{ cancel func() }

func (s legacySubscription) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}
func (s legacySubscription) Done() <-chan struct{} { return neverDone }
func (s legacySubscription) Err() error            { return nil }

// Call invokes a typed command whose types live in this package.
func Call[Req, Resp Payload](ctx context.Context, h Host, c Command[Req, Resp], req Req) (Resp, error) {
	var resp Resp
	if err := Invoke(ctx, h, c.d, req, &resp); err != nil {
		var zero Resp
		return zero, err
	}
	return resp, nil
}

// Emit publishes a typed event.
func Emit[T Payload](h Host, e Event[T], v T) error { return Publish(h, e.d, v) }

// On subscribes to a typed event.
func On[T Payload](h Host, e Event[T], fn func(T)) (Subscription, error) {
	if fn == nil {
		return nil, Fault{Code: FaultUnavailable, Op: e.d.name, Message: "nil handler"}
	}
	return Observe(h, e.d, func(raw json.RawMessage) {
		var v T
		if json.Unmarshal(raw, &v) != nil {
			return
		}
		fn(v)
	})
}

// Registrar is the service side: decode, schema check and encode happen here
// once instead of in every handler. It satisfies CommandHandler, so a plugin
// returns it from Handles/HandleCommand without writing either. One registrar
// takes handlers from this package and from any consumer package.
type Registrar struct {
	mu       sync.RWMutex
	handlers map[string]func(context.Context, bus.Envelope) (bus.Envelope, error)
}

var _ CommandHandler = (*Registrar)(nil)

func NewRegistrar() *Registrar {
	return &Registrar{handlers: map[string]func(context.Context, bus.Envelope) (bus.Envelope, error){}}
}

// Handles reports the registered command names, sorted.
func (r *Registrar) Handles() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HandleCommand dispatches to the registered typed handler.
func (r *Registrar) HandleCommand(ctx context.Context, env bus.Envelope) (bus.Envelope, error) {
	r.mu.RLock()
	fn, ok := r.handlers[env.Type]
	r.mu.RUnlock()
	if !ok {
		return bus.Envelope{}, Fault{Code: FaultUnhandled, Op: env.Type, Message: "no handler registered"}
	}
	return fn(ctx, env)
}

// Handle registers a typed handler. It panics on a duplicate name: two handlers
// for one operation is a start-up programming error, and last-wins would hide
// it until the wrong one answered a call.
func Handle[Req, Resp Payload](r *Registrar, c Command[Req, Resp], fn func(context.Context, Caller, Req) (Resp, error)) {
	if fn == nil {
		panic("plugin: Handle needs a handler")
	}
	HandleRaw(r, c.d, func(ctx context.Context, caller Caller, raw json.RawMessage) (json.RawMessage, error) {
		var req Req
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, Fault{Code: FaultSchema, Op: c.d.name, Message: "request does not decode", Err: err}
		}
		resp, err := fn(ctx, caller, req)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(resp)
		if err != nil {
			return nil, Fault{Code: FaultSchema, Op: c.d.name, Message: "response does not marshal", Err: err}
		}
		return body, nil
	})
}

// CallerOf reads the principal pair the host stamped into an envelope. A plugin
// cannot set a bd- header, so these are host-derived and never asserted by the
// caller. Source is the fallback for a host that has not been migrated.
func CallerOf(env bus.Envelope) Caller {
	c := Caller{
		PluginID:     env.Headers[HeaderCaller],
		Generation:   env.Headers[HeaderGeneration],
		SubjectLease: env.Headers[HeaderSubject],
	}
	if c.PluginID == "" {
		c.PluginID = env.Source
	}
	return c
}

// faultFrom types a transport error. A migrated host stamps HeaderFault and
// this reads it; until then the status text the RPC host produces is the only
// signal there is, so it is translated here rather than at every call site.
func faultFrom(op, code string, err error) error {
	if err == nil {
		return nil
	}
	var fault Fault
	if errors.As(err, &fault) {
		return fault
	}
	if code == "" {
		text := err.Error()
		switch {
		case strings.Contains(text, "403"):
			code = FaultDenied
		case strings.Contains(text, "429"):
			code = FaultBudget
		case strings.Contains(text, "404"), strings.Contains(text, "410"):
			code = FaultWithdrawn
		case strings.Contains(text, "context deadline exceeded"), strings.Contains(text, "context canceled"):
			code = FaultTimeout
		default:
			code = FaultUnavailable
		}
	}
	return Fault{Code: code, Op: op, Message: err.Error(), Err: err}
}
