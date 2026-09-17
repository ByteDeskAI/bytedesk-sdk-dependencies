package bus

import "strings"

// HeaderPrefix is reserved to the host. A plugin cannot set a header with this
// prefix; the substrate stamps caller identity into them and strips any
// inbound forgery.
const HeaderPrefix = "bd-"

// Reserved headers.
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
	// HeaderFault carries a typed fault code on a service error reply.
	HeaderFault = "bd-fault"
	// HeaderCorrelation carries the ByteDesk correlation id.
	HeaderCorrelation = "bd-corr"
	// HeaderTraceparent carries W3C trace context alongside bd-corr.
	HeaderTraceparent = "traceparent"
	// HeaderMsgID is the idempotency key for a stream publish.
	HeaderMsgID = "bd-msg-id"
)

// Headers are message headers. Keys are case-insensitive: the transport
// lowercases on ingress, because NATS canonicalises header keys and a contract
// that depended on case would break on the first real broker.
type Headers map[string]string

// Get reads a header case-insensitively.
func (h Headers) Get(k string) string {
	if h == nil {
		return ""
	}
	if v, ok := h[k]; ok {
		return v
	}
	lk := strings.ToLower(k)
	if v, ok := h[lk]; ok {
		return v
	}
	for hk, v := range h {
		if strings.ToLower(hk) == lk {
			return v
		}
	}
	return ""
}

// Clone returns a lowercase-keyed copy. Ingress calls it so every handler sees
// one spelling.
func (h Headers) Clone() Headers {
	if h == nil {
		return nil
	}
	out := make(Headers, len(h))
	for k, v := range h {
		out[strings.ToLower(k)] = v
	}
	return out
}

// IsReserved reports whether k is a host-reserved header key.
func (h Headers) IsReserved(k string) bool { return IsReservedHeader(k) }

// IsReservedHeader reports whether k is in the host-owned "bd-" namespace.
// Reserved means the host defines what the key means, not that a caller can
// never set one — see StripReserved for the narrower rule that actually binds.
func IsReservedHeader(k string) bool {
	return strings.HasPrefix(strings.ToLower(k), HeaderPrefix)
}

// IdentityHeaders are the headers a principal must never be able to set: the
// ones that carry authority. The substrate stamps them from the bound
// credential, and nothing else may write them.
//
// Everything else under "bd-" is metadata. Forging bd-schema fails your own
// call; forging bd-corr corrupts your own trace; forging bd-caller would let
// you act as someone else. Only the last is a security boundary, and this is
// the list of it.
func IdentityHeaders() []string {
	return []string{HeaderCaller, HeaderGeneration, HeaderSubject}
}

// IsIdentityHeader reports whether k is one of the unforgeable three.
func IsIdentityHeader(k string) bool {
	switch strings.ToLower(k) {
	case HeaderCaller, HeaderGeneration, HeaderSubject:
		return true
	}
	return false
}

// StripReserved removes the headers a principal must not be able to set, and
// lowercases the rest. The substrate calls it on anything a caller supplied,
// immediately before stamping the real values, so caller identity cannot be
// forged by writing bd-caller yourself.
//
// It strips the IDENTITY triple and nothing else, and that scope is load
// bearing. Stripping the whole "bd-" namespace was the obvious first design
// and it was wrong: it also removed bd-schema, so the typed layer's schema
// stamp never survived egress and the receive-side check could never fire.
// A check that cannot fire is worse than no check, because it reads as one.
func StripReserved(h Headers) Headers {
	if h == nil {
		return nil
	}
	out := make(Headers, len(h))
	for k, v := range h {
		lk := strings.ToLower(k)
		if IsIdentityHeader(lk) {
			continue
		}
		out[lk] = v
	}
	return out
}

// Msg is one message as a handler sees it.
type Msg struct {
	Subject Subject
	// Reply is set when the sender expects a response. Respond writes to it.
	Reply Subject
	// Headers are lowercase-keyed.
	Headers Headers
	Data    []byte

	// respond is installed by the substrate. A Msg a caller constructed itself
	// has none, and Respond refuses rather than pretending to answer.
	respond func(Headers, []byte) error
}

// NewMsg builds a Msg that can answer through respond. Substrates call it;
// plugin code does not.
func NewMsg(subject, reply Subject, h Headers, data []byte, respond func(Headers, []byte) error) *Msg {
	return &Msg{Subject: subject, Reply: reply, Headers: h.Clone(), Data: data, respond: respond}
}

// Respond answers a request. It returns a Fault when the message was not a
// request, so a handler that answers the wrong message learns about it.
func (m *Msg) Respond(h Headers, data []byte) error {
	if m == nil || m.respond == nil {
		return Fault{Code: FaultUnhandled, Op: string(m.subjectOrEmpty()), Message: "message is not a request"}
	}
	return m.respond(h, data)
}

// RespondFault answers a request with a typed fault. The fault code travels in
// bd-fault so the caller gets a Fault back rather than an opaque payload.
func (m *Msg) RespondFault(f Fault) error {
	return m.Respond(Headers{HeaderFault: f.Code}, []byte(f.Error()))
}

// Correlation returns the bd-corr value, or "".
func (m *Msg) Correlation() string { return m.Headers.Get(HeaderCorrelation) }

func (m *Msg) subjectOrEmpty() Subject {
	if m == nil {
		return ""
	}
	return m.Subject
}

// Caller is the principal pair a handler sees: mandatory workload identity,
// and an optional subject lease. A subject-scoped operation invoked with no
// lease must be refused; an autonomous-eligible one proceeds under workload
// identity alone, and its rule sees an empty SubjectLease.
type Caller struct {
	PluginID     string
	Generation   string
	SubjectLease string
}

// Autonomous reports that no human principal is attached to this call.
func (c Caller) Autonomous() bool { return c.SubjectLease == "" }

// CallerOf reads the caller identity the SUBSTRATE stamped on m.
//
// It never trusts a peer-supplied bd-caller: the transport re-derives caller
// identity from the connection's own credential and overwrites the header on
// ingress, so by the time a handler sees the message the value is the
// substrate's, not the sender's (R7). CallerOf is therefore a read of an
// already-verified field, not a verification.
func CallerOf(m *Msg) Caller {
	if m == nil {
		return Caller{}
	}
	return Caller{
		PluginID:     m.Headers.Get(HeaderCaller),
		Generation:   m.Headers.Get(HeaderGeneration),
		SubjectLease: m.Headers.Get(HeaderSubject),
	}
}
