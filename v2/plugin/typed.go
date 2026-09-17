package plugin

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Typed access to the untyped bus.
//
// Go methods cannot have type parameters, so bus.Bus.Request can never be
// generic and no amount of redesign will make it so. The typed layer is
// therefore FREE FUNCTIONS taking the bus: it adds no method to bus.Bus, so the
// substrate contract stays exactly the size it is, and a second substrate does
// not have to implement a generic API to be usable.
//
// Descriptors are opaque. Every field of Descriptor is unexported and the only
// way to build one is NewDescriptor, which generated code calls. A call site
// cannot hand-assemble Command{name: "svc.files.v1.delete"} and cannot type an
// operation name at all, so a subject never drifts from the contract that
// declared it. Stated plainly so nobody over-trusts it: that stops DRIFT, it is
// not a security boundary. The constructors are exported and callable — they
// must be, because generated code lives in another package — and security is
// host-side: the subject must be in the plugin's grants and its schema hash
// must match what the host registered, so a forged descriptor buys nothing.
//
// v2 exposes Rev, SchemaHash, Kind and Subject accessors that v1 deliberately
// withheld. v1 withheld them because Name plus those three round-trip straight
// back into NewDescriptor and undo the opacity; v2 grants them because the host
// now needs to read a descriptor's subject and revision to build a ServiceSpec
// and to report what is mounted, and opacity that the host has to work around
// is opacity nobody keeps. Drift protection is unaffected: a generated
// descriptor is still the only way to GET one.
//
// Payload — the per-package generated union constraint — is unchanged in
// principle and deliberately absent from THIS package. A union type set is
// closed to the package that declares it, so a consumer package cannot join
// plugin.Payload without plugin importing it back; that is why each consumer
// package generates its own Payload union over its own classified types and
// constrains its own generated wrappers with it. The functions here take `any`
// because this package classifies no payloads of its own: it is the mechanism
// those generated wrappers are built over, and constraining it to a union this
// package declared would exclude every consumer from it.
//
// The `any` is LOAD-BEARING, not an oversight to be tightened later. The
// author-facing API stays closed because the generated wrapper a consumer calls
// is constrained to that consumer's own union and delegates here; narrowing
// these functions would break that delegation and close the mechanism to
// everyone outside this package, which is the one thing it must not be.

// Kind names what a descriptor addresses. It exists so a descriptor built for
// one shape cannot be handed to a function meant for another without the
// mismatch being visible in a log or a refusal.
type Kind string

const (
	// KindCommand is a request/response operation.
	KindCommand Kind = "command"
	// KindEvent is a published payload with no reply.
	KindEvent Kind = "event"
	// KindStream is a durable, replayable subject.
	KindStream Kind = "stream"
	// KindBucket is a key/value bucket with one payload type.
	KindBucket Kind = "bucket"
	// KindService is a named group of endpoints.
	KindService Kind = "service"
)

// Descriptor is an opaque, generated operation identity. A consumer package
// builds its own typed wrappers over one; nothing hand-assembles it, because
// every field is unexported.
type Descriptor struct {
	kind       Kind
	name       string
	rev        uint32
	schemaHash string
	subject    bus.Subject
}

// NewDescriptor is called by generated code only. It is exported because that
// generated code lives in another package.
//
// A hand-built descriptor with an empty hash is not refused here. A panic in a
// constructor that generated code calls at package init is a bad failure mode
// for a class the guards already fail closed on: such a descriptor stamps no
// schema header, and a host that stamps one refuses it in every direction.
func NewDescriptor(kind Kind, name string, rev uint32, schemaHash string, subject bus.Subject) Descriptor {
	return Descriptor{kind: kind, name: name, rev: rev, schemaHash: schemaHash, subject: subject}
}

// Name is the operation name, for registry keys, logs and diagnostics.
func (d Descriptor) Name() string { return d.name }

// Rev is the contract revision. The host admits by (name, revision, hash).
func (d Descriptor) Rev() uint32 { return d.rev }

// SchemaHash is the payload hash stamped on every send and checked on every
// receive.
func (d Descriptor) SchemaHash() string { return d.schemaHash }

// Kind reports what the descriptor addresses.
func (d Descriptor) Kind() Kind { return d.kind }

// Subject is the bus address this descriptor resolves to.
func (d Descriptor) Subject() bus.Subject { return d.subject }

// Command names one request/response operation. Built by generated code.
type Command[Req, Resp any] struct{ d Descriptor }

// NewCommand builds a command descriptor. Generated code calls this.
func NewCommand[Req, Resp any](name string, rev uint32, schemaHash string, subject bus.Subject) Command[Req, Resp] {
	return Command[Req, Resp]{d: NewDescriptor(KindCommand, name, rev, schemaHash, subject)}
}

// Descriptor returns the underlying opaque identity, for logs, registry keys
// and the ServiceSpec the host builds.
func (c Command[Req, Resp]) Descriptor() Descriptor { return c.d }

// Event names one published payload type. Built by generated code.
type Event[T any] struct{ d Descriptor }

// NewEvent builds an event descriptor. Generated code calls this.
func NewEvent[T any](name string, rev uint32, schemaHash string, subject bus.Subject) Event[T] {
	return Event[T]{d: NewDescriptor(KindEvent, name, rev, schemaHash, subject)}
}

// Descriptor returns the underlying opaque identity.
func (e Event[T]) Descriptor() Descriptor { return e.d }

// StreamDescriptor names one durable subject and the payload type stored on it.
// Built by generated code.
type StreamDescriptor[T any] struct{ d Descriptor }

// NewStreamDescriptor builds a stream descriptor. Generated code calls this.
func NewStreamDescriptor[T any](name string, rev uint32, schemaHash string, subject bus.Subject) StreamDescriptor[T] {
	return StreamDescriptor[T]{d: NewDescriptor(KindStream, name, rev, schemaHash, subject)}
}

// Descriptor returns the underlying opaque identity.
func (s StreamDescriptor[T]) Descriptor() Descriptor { return s.d }

// BucketDescriptor names one KV bucket and the single payload type its values
// carry. Built by generated code.
//
// The bucket NAME is carried in Descriptor.Name rather than in a subject: a KV
// bucket is addressed by name, and the subject field is empty for this kind.
type BucketDescriptor[T any] struct{ d Descriptor }

// NewBucketDescriptor builds a bucket descriptor. Generated code calls this.
func NewBucketDescriptor[T any](name string, rev uint32, schemaHash string) BucketDescriptor[T] {
	return BucketDescriptor[T]{d: NewDescriptor(KindBucket, name, rev, schemaHash, "")}
}

// Descriptor returns the underlying opaque identity.
func (b BucketDescriptor[T]) Descriptor() Descriptor { return b.d }

// ServiceDescriptor names one service. It carries no payload type of its own —
// its endpoints do — so it is not generic.
//
// Its schema hash covers the SHAPE OF THE SERVICE rather than one payload: the
// name, the version and every endpoint's name, subject and request/response
// schemas. That is what makes hashing a service worth doing — adding, removing
// or re-addressing an endpoint changes the service's identity, so a caller
// holding yesterday's endpoint map is refused instead of calling into a subject
// that has moved.
type ServiceDescriptor struct{ d Descriptor }

// NewServiceDescriptor builds a service descriptor. Generated code calls this.
func NewServiceDescriptor(name string, rev uint32, schemaHash string, subject bus.Subject) ServiceDescriptor {
	return ServiceDescriptor{d: NewDescriptor(KindService, name, rev, schemaHash, subject)}
}

// Descriptor returns the underlying opaque identity.
func (s ServiceDescriptor) Descriptor() Descriptor { return s.d }

// Version is the version a ServiceSpec advertises, derived from the descriptor
// revision so the advertised value and the contract cannot disagree.
func (s ServiceDescriptor) Version() string { return serviceVersion(s.d.rev) }

// serviceVersion renders a contract revision as the semantic version a
// ServiceSpec and a manifest ServiceDecl both spell.
//
// It is MAJOR.0.0, not "vN". bus.ServiceSpec.Version is copied verbatim into
// the ServiceInfo that Discover returns, and a manifest declares the same
// service as a semver, so advertising "v3" where the manifest says "3.0.0"
// would make discovery compare two spellings of one version and match neither.
// Nothing in the substrate validates the format, so that mismatch would be
// silent — which is the reason to settle it here rather than find it in wave 2.
//
// A revision IS the major: a revision bump is a breaking change to the
// contract, which is precisely what a major means. The minor and patch are
// pinned at zero because the descriptor genuinely does not know them — they
// belong to the plugin's own release, not to the generated contract. A plugin
// that needs a fuller version advertises it by building its own ServiceSpec and
// mounting through Registrar, rather than through the one-endpoint Serve
// shortcut. Inventing a minor here would be precision this layer does not have.
func serviceVersion(rev uint32) string {
	return strconv.FormatUint(uint64(rev), 10) + ".0.0"
}

// Schema stamping and checking.
//
// Every send stamps bd-schema with the descriptor's hash and every receive
// REQUIRES it before unmarshalling, so a payload from a different contract
// revision — or from a peer that declares no revision at all — is refused
// rather than decoded into a type it does not fit. Lenient decoding is never
// compatibility evidence.
//
// Requiring it, rather than checking it when present, is the whole guarantee.
// An absent header used to be tolerated as "an unstamped peer, not an
// incompatible one", which sounds careful and is not: it means the check never
// fires against exactly the sender that told you least about itself, while
// still reading like a check. bus.WithSchema now writes the header after and
// independently of WithHeaders' reserved-namespace strip, so every sender in
// this layer stamps and there is no unstamped peer left to be lenient towards.
//
// One consequence, deliberately fail-closed: a descriptor carrying an EMPTY
// schema hash stamps nothing, so its own traffic is refused on arrival. A
// hand-built descriptor therefore does not silently opt out of verification —
// it opts out of working, which is the failure an author notices.
//
// The identity triple (bd-caller, bd-generation, bd-subject) is NOT stampable
// and must not become so. A forged schema hash fails the forger's own call; a
// forged caller would buy authority. That asymmetry is why WithSchema exists
// and no WithCaller does.
//
// stamp builds the header map for the one send path that has no option list:
// Msg.Respond takes headers directly.
func stamp(d Descriptor) bus.Headers {
	if d.schemaHash == "" {
		return nil
	}
	return bus.Headers{bus.HeaderSchema: d.schemaHash}
}

// checkSchema refuses before anything is unmarshalled. Absence and mismatch are
// reported separately because they are different author mistakes: one peer
// never stamped, the other stamped a revision you do not speak.
func checkSchema(d Descriptor, h bus.Headers) error {
	rev := " (revision " + strconv.FormatUint(uint64(d.rev), 10) + ")"
	switch got := h.Get(bus.HeaderSchema); {
	case got == "":
		return bus.Fault{
			Code:    bus.FaultSchema,
			Op:      d.name,
			Message: "message carries no " + bus.HeaderSchema + "; want " + strconv.Quote(d.schemaHash) + rev,
		}
	case got != d.schemaHash:
		return bus.Fault{
			Code:    bus.FaultSchema,
			Op:      d.name,
			Message: "schema " + strconv.Quote(got) + " does not match " + strconv.Quote(d.schemaHash) + rev,
		}
	}
	return nil
}

// faultReply turns a service error reply into a Fault. A service answers an
// error with bd-fault set, so the caller gets a typed refusal rather than a
// payload it has to inspect to discover it failed.
//
// It runs BEFORE checkSchema, and the order is load-bearing: an error reply
// carries a fault code and an operator message, not a payload of the declared
// type, so it has no schema hash to stamp. Checking the schema first would
// turn every refusal into FaultSchema and lose the reason.
func faultReply(d Descriptor, m *bus.Msg) error {
	if m == nil {
		return bus.Fault{Code: bus.FaultUnavailable, Op: d.name, Message: "no reply"}
	}
	code := m.Headers.Get(bus.HeaderFault)
	if code == "" {
		return nil
	}
	return bus.Fault{Code: code, Op: d.name, Message: string(m.Data)}
}

// Call invokes a typed command and waits for its reply.
func Call[Req, Resp any](ctx context.Context, b bus.Bus, c Command[Req, Resp], req Req) (Resp, error) {
	var zero Resp
	if b == nil {
		return zero, bus.Fault{Code: bus.FaultUnavailable, Op: c.d.name, Message: "no bus"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return zero, bus.Fault{Code: bus.FaultSchema, Op: c.d.name, Message: "request does not marshal", Err: err}
	}
	reply, err := b.Request(ctx, c.d.subject, body, bus.WithSchema(c.d.schemaHash))
	if err != nil {
		return zero, err
	}
	if err := faultReply(c.d, reply); err != nil {
		return zero, err
	}
	if err := checkSchema(c.d, reply.Headers); err != nil {
		return zero, err
	}
	var resp Resp
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return zero, bus.Fault{Code: bus.FaultSchema, Op: c.d.name, Message: "reply does not decode", Err: err}
	}
	return resp, nil
}

// Emit publishes a typed event. It is fire-and-forget: a grant refusal may
// surface on the next call for that subject rather than here, which is a
// property of the substrate's core publish, not of this layer.
func Emit[T any](ctx context.Context, b bus.Bus, e Event[T], v T) error {
	if b == nil {
		return bus.Fault{Code: bus.FaultUnavailable, Op: e.d.name, Message: "no bus"}
	}
	body, err := json.Marshal(v)
	if err != nil {
		return bus.Fault{Code: bus.FaultSchema, Op: e.d.name, Message: "payload does not marshal", Err: err}
	}
	return b.Publish(ctx, e.d.subject, body, bus.WithSchema(e.d.schemaHash))
}

// On subscribes to a typed event. The handler receives the decoded payload and
// the caller the substrate stamped on the message.
//
// A message whose schema hash does not match is DROPPED rather than delivered
// or reported: it belongs to a different revision of the contract, and this
// subscription is not the place to learn about it. A message that does not
// decode is dropped for the same reason. Neither is silent loss of this
// contract's traffic, because neither was this contract's traffic.
func On[T any](ctx context.Context, b bus.Bus, e Event[T], h func(context.Context, T, bus.Caller)) (bus.Subscription, error) {
	if b == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: e.d.name, Message: "no bus"}
	}
	if h == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: e.d.name, Message: "nil handler"}
	}
	return b.Subscribe(ctx, bus.Pattern(e.d.subject), func(ctx context.Context, m *bus.Msg) {
		if checkSchema(e.d, m.Headers) != nil {
			return
		}
		var v T
		if json.Unmarshal(m.Data, &v) != nil {
			return
		}
		h(ctx, v, bus.CallerOf(m))
	})
}

// Serve mounts a typed command as a one-endpoint service and answers calls to
// it. Decode, schema check and encode happen here once instead of in every
// handler.
//
// Containment is not this function's job and is not skipped: bus.Services.Serve
// refuses an endpoint outside the caller's own namespace or outside the
// manifest's serves list, naming the subject. Registrar below is the local
// preflight for a plugin assembling a multi-endpoint ServiceSpec by hand.
func Serve[Req, Resp any](ctx context.Context, b bus.Bus, c Command[Req, Resp], h func(context.Context, Req, bus.Caller) (Resp, error)) (bus.Service, error) {
	if b == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: c.d.name, Message: "no bus"}
	}
	if h == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: c.d.name, Message: "nil handler"}
	}
	handler := func(ctx context.Context, m *bus.Msg) {
		if err := checkSchema(c.d, m.Headers); err != nil {
			var fault bus.Fault
			if f, ok := err.(bus.Fault); ok {
				fault = f
			}
			_ = m.RespondFault(fault)
			return
		}
		var req Req
		if err := json.Unmarshal(m.Data, &req); err != nil {
			_ = m.RespondFault(bus.Fault{Code: bus.FaultSchema, Op: c.d.name, Message: "request does not decode"})
			return
		}
		resp, err := h(ctx, req, bus.CallerOf(m))
		if err != nil {
			var fault bus.Fault
			if f, ok := err.(bus.Fault); ok {
				fault = f
			} else {
				fault = bus.Fault{Code: bus.FaultUnhandled, Op: c.d.name, Message: err.Error()}
			}
			_ = m.RespondFault(fault)
			return
		}
		body, err := json.Marshal(resp)
		if err != nil {
			_ = m.RespondFault(bus.Fault{Code: bus.FaultSchema, Op: c.d.name, Message: "response does not marshal"})
			return
		}
		_ = m.Respond(stamp(c.d), body)
	}
	return b.Services().Serve(ctx, bus.ServiceSpec{
		Name:    c.d.name,
		Version: serviceVersion(c.d.rev),
		Endpoints: []bus.EndpointSpec{{
			Name:    c.d.name,
			Subject: c.d.subject,
			Handler: handler,
		}},
	})
}

// Stream binds a stream descriptor to a bus and returns a typed view of it.
//
// It is the smallest honest signature: binding does no I/O, so it takes no
// context and cannot fail. Everything that CAN fail — appending, consuming — is
// a method on the result that takes its own context. A constructor that
// returned an error nobody could produce would be a lie a caller then has to
// write a branch for.
//
// Declaring the underlying stream is the host's job, from the manifest. This
// type never declares one, because a stream a plugin could declare for itself
// is a stream the manifest did not have to list.
func Stream[T any](b bus.Bus, s StreamDescriptor[T]) TypedStream[T] {
	return TypedStream[T]{b: b, d: s.d}
}

// TypedStream is a typed view of one durable subject.
type TypedStream[T any] struct {
	b bus.Bus
	d Descriptor
}

// Descriptor returns the underlying opaque identity.
func (s TypedStream[T]) Descriptor() Descriptor { return s.d }

// Append stores one typed message and returns its sequence.
func (s TypedStream[T]) Append(ctx context.Context, v T, opts ...bus.PublishOpt) (bus.Seq, error) {
	if s.b == nil {
		return 0, bus.Fault{Code: bus.FaultUnavailable, Op: s.d.name, Message: "no bus"}
	}
	body, err := json.Marshal(v)
	if err != nil {
		return 0, bus.Fault{Code: bus.FaultSchema, Op: s.d.name, Message: "payload does not marshal", Err: err}
	}
	return s.b.Streams().Publish(ctx, s.d.subject, body, append([]bus.PublishOpt{bus.WithSchema(s.d.schemaHash)}, opts...)...)
}

// Consume delivers stored messages to h until the Consumer is cancelled. The
// handler receives the decoded payload AND the raw *bus.StreamMsg, because
// acknowledgement is the handler's obligation under AckExplicit and this layer
// must not ack on its behalf: an automatic ack would turn every decode failure
// into silent data loss.
//
// A message whose schema hash does not match is Term'd with the reason rather
// than dropped or redelivered forever. It will never decode, so redelivery is
// only a way to hide it.
func (s TypedStream[T]) Consume(ctx context.Context, stream string, spec bus.ConsumerSpec, h func(context.Context, T, *bus.StreamMsg)) (bus.Consumer, error) {
	if s.b == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: s.d.name, Message: "no bus"}
	}
	if h == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: s.d.name, Message: "nil handler"}
	}
	return s.b.Streams().Consume(ctx, stream, spec, func(ctx context.Context, m *bus.StreamMsg) {
		if err := checkSchema(s.d, m.Headers); err != nil {
			_ = m.Term(err.Error())
			return
		}
		var v T
		if err := json.Unmarshal(m.Data, &v); err != nil {
			_ = m.Term("payload does not decode: " + err.Error())
			return
		}
		h(ctx, v, m)
	})
}

// OpenBucket opens a KV bucket and returns a typed view of it.
//
// It is named OpenBucket rather than Bucket because plugin.Bucket already names
// the re-exported bus.Bucket, and a type and a function cannot share an
// identifier. The name is also the more honest of the two: unlike Stream, this
// one performs I/O and can fail.
//
// The schema check for KV happens at OPEN, in the substrate: a KV value carries
// no headers of its own, so the hash lives in the declaring BucketSpec's
// Metadata and bus.KV.Open compares it there. One payload type per bucket is
// the reason that works. This layer cannot re-verify it, because BucketStatus
// does not carry the metadata back.
func OpenBucket[T any](ctx context.Context, b bus.Bus, d BucketDescriptor[T]) (TypedBucket[T], error) {
	if b == nil {
		return TypedBucket[T]{}, bus.Fault{Code: bus.FaultUnavailable, Op: d.d.name, Message: "no bus"}
	}
	raw, err := b.KV().Open(ctx, d.d.name)
	if err != nil {
		return TypedBucket[T]{}, err
	}
	return TypedBucket[T]{bucket: raw, d: d.d}, nil
}

// TypedBucket is a typed view of one KV bucket.
type TypedBucket[T any] struct {
	bucket bus.Bucket
	d      Descriptor
}

// Descriptor returns the underlying opaque identity.
func (t TypedBucket[T]) Descriptor() Descriptor { return t.d }

// Raw returns the untyped bucket, for the operations that do not involve a
// payload at all: Delete, Purge, Keys, Status, History and Watch.
func (t TypedBucket[T]) Raw() bus.Bucket { return t.bucket }

// Get reads the current value and its revision. A missing key is FaultNotFound
// from the substrate.
func (t TypedBucket[T]) Get(ctx context.Context, key string) (T, bus.Revision, error) {
	var zero T
	if t.bucket == nil {
		return zero, 0, bus.Fault{Code: bus.FaultUnavailable, Op: t.d.name, Message: "bucket is not open"}
	}
	entry, err := t.bucket.Get(ctx, key)
	if err != nil {
		return zero, 0, err
	}
	var v T
	if err := json.Unmarshal(entry.Value, &v); err != nil {
		return zero, 0, bus.Fault{Code: bus.FaultSchema, Op: t.d.name, Message: "value at " + strconv.Quote(key) + " does not decode", Err: err}
	}
	return v, entry.Revision, nil
}

// Put writes unconditionally and returns the new revision.
func (t TypedBucket[T]) Put(ctx context.Context, key string, v T) (bus.Revision, error) {
	body, err := t.encode(key, v)
	if err != nil {
		return 0, err
	}
	return t.bucket.Put(ctx, key, body)
}

// Update writes only if the current revision is expect; otherwise
// FaultConflict. This is the compare-and-swap every reconciler needs.
func (t TypedBucket[T]) Update(ctx context.Context, key string, v T, expect bus.Revision) (bus.Revision, error) {
	body, err := t.encode(key, v)
	if err != nil {
		return 0, err
	}
	return t.bucket.Update(ctx, key, body, expect)
}

func (t TypedBucket[T]) encode(key string, v T) ([]byte, error) {
	if t.bucket == nil {
		return nil, bus.Fault{Code: bus.FaultUnavailable, Op: t.d.name, Message: "bucket is not open"}
	}
	body, err := json.Marshal(v)
	if err != nil {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: t.d.name, Message: "value for " + strconv.Quote(key) + " does not marshal", Err: err}
	}
	return body, nil
}

// Registrar is the local containment gate for a plugin assembling a service by
// hand: it refuses an endpoint whose subject is outside the manifest's serves
// list BEFORE the plugin hands the spec to the substrate, and names the subject
// when it does.
//
// The substrate refuses the same thing at Serve, so this is a preflight rather
// than the enforcement — the same relationship Grants.Can has to the host's own
// check. It exists because the error arrives here with the endpoint still in
// hand, where the author can see which of twenty endpoints was wrong.
type Registrar struct {
	mu        sync.Mutex
	serves    []bus.Pattern
	endpoints []bus.EndpointSpec
}

// NewRegistrar builds a registrar over the manifest's serves list. It copies
// the list, so a caller cannot widen its own containment afterwards by
// appending to the slice it passed in.
func NewRegistrar(serves []bus.Pattern) *Registrar {
	return &Registrar{serves: append([]bus.Pattern(nil), serves...)}
}

// Mount records an endpoint after checking it against the serves list. It
// refuses an endpoint with no subject, an endpoint no serves pattern covers,
// and a second endpoint on a subject already mounted.
//
// A duplicate is FaultConflict rather than last-wins: two handlers for one
// subject is a start-up programming error, and last-wins hides it until the
// wrong one answers a call.
func (r *Registrar) Mount(e bus.EndpointSpec) error {
	if e.Subject == "" {
		return bus.Fault{Code: bus.FaultSchema, Op: e.Name, Message: "endpoint has no subject"}
	}
	if e.Handler == nil {
		return bus.Fault{Code: bus.FaultSchema, Op: string(e.Subject), Message: "endpoint has no handler"}
	}
	if !bus.CoveredByAny(r.serves, bus.Pattern(e.Subject)) {
		return bus.Fault{
			Code:    bus.FaultDenied,
			Op:      string(e.Subject),
			Message: "subject " + strconv.Quote(string(e.Subject)) + " is not in the manifest serves list",
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, have := range r.endpoints {
		if have.Subject == e.Subject {
			return bus.Fault{
				Code:    bus.FaultConflict,
				Op:      string(e.Subject),
				Message: "subject " + strconv.Quote(string(e.Subject)) + " is already mounted",
			}
		}
	}
	r.endpoints = append(r.endpoints, e)
	return nil
}

// Endpoints returns the mounted endpoints in mount order, as a copy.
func (r *Registrar) Endpoints() []bus.EndpointSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bus.EndpointSpec(nil), r.endpoints...)
}
