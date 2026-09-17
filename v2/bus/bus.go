// Package bus is the substrate-neutral messaging contract of SDK v2.
//
// Every plugin reaches the gateway through this one interface, obtained from
// the embedded plugin.Base as Bus(). There is no other channel and no escape
// hatch: there is deliberately no Raw() accessor, because one would leak the
// broker's own types into every plugin's dependency graph and make the
// substrate unswappable. "All the power" is therefore a completeness
// obligation on this package — every capability the substrate has is reachable
// through an SDK-owned type — and two different tests hold two different
// halves of it.
//
// The module's boundary_test.go fails the build if github.com/nats-io/*
// reaches any exported signature here, and its inventory_test.go reads these
// packages with go/doc and fails when an exported TYPE is declared and not
// registered for that walk — so the boundary gate cannot go stale by omission.
// Neither test looks at capabilities. That is bus/conformance's job: the
// property list is what proves two substrates equivalent, and
// TestCapabilityVocabularyIsExactlyWhatTheSuiteProves requires every name in
// CapabilityNames to be proved by a property or carried in
// conformance.Deferred with its reason.
package bus

import (
	"context"
	"time"
)

// DefaultTimeout bounds a Request that carries neither WithTimeout nor a
// context deadline.
const DefaultTimeout = 5 * time.Second

// DefaultPendingLimit is the per-subscription queue depth. Overflow raises
// FaultSlowConsumer on the subscription; the publisher never blocks and the
// drop is never silent.
//
// The number matters: nats.go defaults a subscription's pending limit to 64 MB,
// which turns a stalled handler into unbounded memory growth instead of a
// loud, countable refusal. Every Subscribe applies this bound unless the caller
// raises it deliberately.
const DefaultPendingLimit = 64

// Handler receives one message. It is called on the substrate's delivery
// goroutine for that subscription, one message at a time and in order, so a
// handler that blocks delays only its own subscription.
type Handler func(ctx context.Context, m *Msg)

// Bus is the IBus: the single messaging surface every plugin has. It is bound
// to the plugin's identity and effective grants, and the host enforces those
// grants on every call regardless of what Identity().Grants().Can() reported.
type Bus interface {
	// Publish emits to a concrete subject. Under a broker a core publish is
	// asynchronous, so a grant refusal may surface on the subscription's Err
	// or on the next call for that subject rather than here — use
	// Identity().Grants().Can as a preflight and Request when you need a
	// synchronous answer.
	Publish(ctx context.Context, subject Subject, data []byte, opts ...PublishOpt) error

	// Subscribe delivers every message matching pattern until the
	// Subscription is cancelled or drained.
	Subscribe(ctx context.Context, pattern Pattern, h Handler, opts ...SubOpt) (Subscription, error)

	// Request sends and waits for one reply. With nothing listening it fails
	// fast with FaultNoResponders rather than burning the timeout.
	Request(ctx context.Context, subject Subject, data []byte, opts ...ReqOpt) (*Msg, error)

	// Streams is durable, replayable messaging.
	Streams() Streams
	// KV is the key/value store, versioned and watchable.
	KV() KV
	// Objects stores blobs by reference.
	Objects() Objects
	// Services is request/reply with discovery, versioning and stats.
	Services() Services
	// Schedule is durable scheduled publishing; it replaces v1 Host.Every.
	Schedule() Scheduler
	// Trace propagates correlation and W3C trace context.
	Trace() Trace

	// Capabilities reports what THIS substrate implements. A manifest "needs"
	// entry is checked against it at enable time and fails closed, so a
	// running plugin can rely on what it declared.
	Capabilities() Capabilities

	// Close releases the plugin's connection. The host calls it on revoke; a
	// plugin calls it only in its own tests.
	Close() error
}

// Subscription is a live subscription. Its shape is unchanged from v1.
type Subscription interface {
	// Cancel stops delivery immediately, dropping anything queued.
	Cancel()
	// Drain stops accepting new messages and returns once the queue is
	// delivered or ctx expires.
	Drain(ctx context.Context) error
	// Done closes when the subscription has finished, for any reason.
	Done() <-chan struct{}
	// Err reports why it finished: nil while live and on a clean Cancel or
	// Drain, a Fault when the substrate ended it (FaultSlowConsumer on
	// overflow, FaultDenied when a grant was revoked or never covered the
	// pattern, FaultWithdrawn on close).
	Err() error
}

// Capabilities are the substrate's features, not the principal's grants
// (R4): a capability is a property of the bus, a grant is a property of the
// caller. Manifest "needs" is checked against this; Identity().Grants()
// answers the other question.
type Capabilities struct {
	// Durable is streams: Declare, Publish, Consume, Ack, replay by cursor.
	Durable bool
	// KV is the key/value store.
	KV bool
	// Objects is the blob store.
	Objects bool
	// Services is the discoverable request/reply service API.
	Services bool
	// Schedule is durable scheduled publishing.
	Schedule bool
	// Counters is the atomic per-subject counter on a stream.
	Counters bool
	// Batch is atomic multi-message stream publish.
	Batch bool
	// Trace is broker-side message tracing. False until it is built; bd-corr
	// and traceparent propagation work regardless.
	Trace bool

	// MaxPayload is the per-message byte ceiling this principal may publish.
	// It is server-enforced per principal: 64 KiB for a plugin, 1 MiB for the
	// host, UI and orchestration principals (R5).
	MaxPayload int
}

// CapabilityNames is the closed vocabulary a manifest "needs" entry may use.
// Adding a name here is an SDK release; a manifest naming anything else fails
// validation rather than being ignored.
//
// A name added here must also be proved: bus/conformance requires every one of
// these to be covered by a conformance property or listed in
// conformance.Deferred with the reason it is not, so a capability cannot
// become declarable without either a cross-implementation guarantee or a
// visible debt.
func CapabilityNames() []string {
	return []string{"durable", "kv", "objects", "services", "schedule", "counters", "batch", "trace"}
}

// Has reports whether this substrate provides the named capability. An unknown
// name is false — the validator is what refuses it, so a typo fails closed
// here too.
func (c Capabilities) Has(name string) bool {
	switch name {
	case "durable":
		return c.Durable
	case "kv":
		return c.KV
	case "objects":
		return c.Objects
	case "services":
		return c.Services
	case "schedule":
		return c.Schedule
	case "counters":
		return c.Counters
	case "batch":
		return c.Batch
	case "trace":
		return c.Trace
	}
	return false
}

// Missing returns the names in needs this substrate does not provide, in the
// order given. Enable-time capability checking is exactly this call.
func (c Capabilities) Missing(needs []string) []string {
	var out []string
	for _, n := range needs {
		if !c.Has(n) {
			out = append(out, n)
		}
	}
	return out
}

// PublishOptions are the resolved options of one Publish.
type PublishOptions struct {
	Headers Headers
	// MsgID is the idempotency key. On a stream, a repeat within the dedupe
	// window is accepted and not stored twice.
	MsgID string
	// ExpectLastSeq makes the publish conditional on the stream's current
	// sequence; a mismatch is FaultConflict. Zero means unconditional.
	ExpectLastSeq Seq
	// TTL expires the message. Zero means no expiry.
	TTL time.Duration
}

// SubOptions are the resolved options of one Subscribe.
type SubOptions struct {
	// QueueGroup makes this one member of a competing-consumer group: each
	// message goes to exactly one member.
	QueueGroup string
	// PendingLimit bounds the per-subscription queue. Zero means
	// DefaultPendingLimit.
	PendingLimit int
}

// ReqOptions are the resolved options of one Request.
type ReqOptions struct {
	Headers Headers
	// Timeout bounds the wait when ctx has no earlier deadline. Zero means
	// DefaultTimeout.
	Timeout time.Duration
}

// PublishOpt, SubOpt and ReqOpt are separate interfaces so an option that
// makes sense for one call cannot be passed to another. An option meaningful
// to several — WithHeaders — implements several.
type (
	// PublishOpt configures one Publish.
	PublishOpt interface{ applyPublish(*PublishOptions) }
	// SubOpt configures one Subscribe.
	SubOpt interface{ applySub(*SubOptions) }
	// ReqOpt configures one Request.
	ReqOpt interface{ applyReq(*ReqOptions) }
)

type headersOpt struct{ h Headers }

func (o headersOpt) applyPublish(p *PublishOptions) { p.Headers = mergeHeaders(p.Headers, o.h) }
func (o headersOpt) applyReq(r *ReqOptions)         { r.Headers = mergeHeaders(r.Headers, o.h) }

// WithHeaders adds headers to a Publish or a Request. Host-reserved bd-*
// headers are stripped: the substrate stamps those itself.
func WithHeaders(h Headers) interface {
	PublishOpt
	ReqOpt
} {
	return headersOpt{h: StripReserved(h)}
}

// WithSchema and WithCorrelation are the ONLY sanctioned writes into the
// host-reserved "bd-" namespace, and they exist because the alternative was
// worse.
//
// WithHeaders strips every bd- header, which is what keeps bd-caller
// unforgeable. But the typed layer legitimately has to stamp bd-schema, and
// tracing has to stamp bd-corr, and both were silently stripped — leaving a
// schema check that could never fire. Rather than open the namespace back up,
// the two headers a caller MAY set get one narrow typed option each.
//
// The identity triple — bd-caller, bd-generation, bd-subject — deliberately has
// no option and never will. Those carry authority; the substrate stamps them
// from the bound credential and nothing else can write them. Forging bd-schema
// or bd-corr buys a caller nothing: a wrong schema hash fails its own call, and
// a wrong correlation id corrupts its own trace.

type schemaOpt struct{ hash string }

func (o schemaOpt) applyPublish(p *PublishOptions) {
	p.Headers = setHeader(p.Headers, HeaderSchema, o.hash)
}
func (o schemaOpt) applyReq(r *ReqOptions) {
	r.Headers = setHeader(r.Headers, HeaderSchema, o.hash)
}

// WithSchema stamps the operation's schema hash so the receiver can refuse a
// mismatched payload BEFORE unmarshalling it. The typed layer sets it on every
// send; hand-written callers of an untyped Publish do not have one to set.
func WithSchema(hash string) interface {
	PublishOpt
	ReqOpt
} {
	return schemaOpt{hash: hash}
}

type correlationOpt struct{ id string }

func (o correlationOpt) applyPublish(p *PublishOptions) {
	p.Headers = setHeader(p.Headers, HeaderCorrelation, o.id)
}
func (o correlationOpt) applyReq(r *ReqOptions) {
	r.Headers = setHeader(r.Headers, HeaderCorrelation, o.id)
}

// WithCorrelation stamps the ByteDesk correlation id. Trace().Inject is the
// usual way to get one onto a message; this is the explicit form.
func WithCorrelation(id string) interface {
	PublishOpt
	ReqOpt
} {
	return correlationOpt{id: id}
}

type msgIDOpt struct{ id string }

func (o msgIDOpt) applyPublish(p *PublishOptions) { p.MsgID = o.id }

// WithMsgID sets the idempotency key for a stream publish.
func WithMsgID(id string) PublishOpt { return msgIDOpt{id: id} }

type expectSeqOpt struct{ seq Seq }

func (o expectSeqOpt) applyPublish(p *PublishOptions) { p.ExpectLastSeq = o.seq }

// ExpectLastSeq makes a stream publish conditional on the current sequence.
func ExpectLastSeq(seq Seq) PublishOpt { return expectSeqOpt{seq: seq} }

type ttlOpt struct{ d time.Duration }

func (o ttlOpt) applyPublish(p *PublishOptions) { p.TTL = o.d }

// WithTTL expires a published message after d.
func WithTTL(d time.Duration) PublishOpt { return ttlOpt{d: d} }

type timeoutOpt struct{ d time.Duration }

func (o timeoutOpt) applyReq(r *ReqOptions) { r.Timeout = o.d }

// WithTimeout bounds a Request. The context still wins if it expires first.
func WithTimeout(d time.Duration) ReqOpt { return timeoutOpt{d: d} }

type queueGroupOpt struct{ name string }

func (o queueGroupOpt) applySub(s *SubOptions) { s.QueueGroup = o.name }

// QueueGroup makes the subscription a competing consumer: each matching
// message is delivered to exactly one member of the group.
func QueueGroup(name string) SubOpt { return queueGroupOpt{name: name} }

type pendingLimitOpt struct{ n int }

func (o pendingLimitOpt) applySub(s *SubOptions) { s.PendingLimit = o.n }

// PendingLimit raises or lowers this subscription's queue depth. Overflow
// raises FaultSlowConsumer rather than dropping silently.
func PendingLimit(n int) SubOpt { return pendingLimitOpt{n: n} }

// ResolvePublish applies opts over the defaults. Substrates call it.
func ResolvePublish(opts []PublishOpt) PublishOptions {
	var o PublishOptions
	for _, opt := range opts {
		if opt != nil {
			opt.applyPublish(&o)
		}
	}
	return o
}

// ResolveSub applies opts over the defaults. Substrates call it.
func ResolveSub(opts []SubOpt) SubOptions {
	o := SubOptions{PendingLimit: DefaultPendingLimit}
	for _, opt := range opts {
		if opt != nil {
			opt.applySub(&o)
		}
	}
	if o.PendingLimit <= 0 {
		o.PendingLimit = DefaultPendingLimit
	}
	return o
}

// ResolveReq applies opts over the defaults. Substrates call it.
func ResolveReq(opts []ReqOpt) ReqOptions {
	o := ReqOptions{Timeout: DefaultTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt.applyReq(&o)
		}
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	return o
}

func setHeader(h Headers, k, v string) Headers {
	if v == "" {
		return h
	}
	if h == nil {
		h = make(Headers, 1)
	}
	h[k] = v
	return h
}

func mergeHeaders(into, from Headers) Headers {
	if len(from) == 0 {
		return into
	}
	if into == nil {
		into = make(Headers, len(from))
	}
	for k, v := range from {
		into[k] = v
	}
	return into
}
