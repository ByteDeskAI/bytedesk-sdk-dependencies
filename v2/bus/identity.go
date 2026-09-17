package bus

import "slices"

// Identity and Grants live here, not in package plugin, for one reason: a
// substrate has to know who is calling and what they may do, and a substrate
// that imported the plugin package to find that out would invert the layering.
// Package plugin aliases these types, so a plugin author still writes
// plugin.Identity.

// Role names the class of principal a connection belongs to. Grants are
// per-principal; role is what the host used to compile them, and what a
// handler reads when it wants to know whether it is being called by the host,
// the browser, orchestration or a peer plugin.
type Role string

const (
	// RoleHost is the gateway itself.
	RoleHost Role = "host"
	// RolePlugin is a first- or third-party plugin.
	RolePlugin Role = "plugin"
	// RoleUI is a browser session.
	RoleUI Role = "ui"
	// RoleOrchestration is an agent-orchestration client.
	RoleOrchestration Role = "orch"
)

// Lease is an opaque subject lease: the host-owned handle that says a human
// principal is attached to this generation. A plugin may compare two leases
// and may pass one back to the host. It may not parse one, and there is
// nothing in it to parse.
type Lease struct {
	token string
}

// NewLease wraps a host-minted lease token. Hosts and transports call it.
func NewLease(token string) Lease { return Lease{token: token} }

// Token returns the opaque token for transport. It is not an identifier a
// plugin may interpret.
func (l Lease) Token() string { return l.token }

// IsZero reports that no lease is attached: the principal is autonomous.
func (l Lease) IsZero() bool { return l.token == "" }

// Equal compares two leases. Equality is the only operation a lease supports.
func (l Lease) Equal(o Lease) bool { return l.token == o.token }

// GrantKind names which list of the effective grants Can reads.
type GrantKind string

const (
	// GrantPublish is the right to publish to a subject.
	GrantPublish GrantKind = "publish"
	// GrantSubscribe is the right to receive a subject.
	GrantSubscribe GrantKind = "subscribe"
	// GrantRequest is the right to call a subject and wait for a reply.
	GrantRequest GrantKind = "request"
	// GrantServe is the right to mount an endpoint on a subject.
	GrantServe GrantKind = "serve"
)

// AssetKind names a provisioned asset class.
type AssetKind string

const (
	// AssetStream is a durable stream.
	AssetStream AssetKind = "stream"
	// AssetKV is a key/value bucket.
	AssetKV AssetKind = "kv"
	// AssetObjects is an object bucket.
	AssetObjects AssetKind = "objects"
)

// Grants are the EFFECTIVE patterns for one principal in one generation:
// what the manifest asked for, intersected with the ceiling and the operator's
// consent, after the permanently-ineligible families were removed.
//
// There is no narrowing. A request that is not covered is refused with the
// reason; it is never silently shrunk to the part that was allowed, because a
// caller that believes it subscribed to a family and receives part of it has
// no way to notice.
type Grants struct {
	Publish   []Pattern
	Subscribe []Pattern
	Request   []Pattern
	Serves    []Pattern

	Streams []string
	KV      []string
	Objects []string
}

// Can is a UX preflight, not an authorisation. It answers "will this be
// refused?" locally so a plugin can disable a control or pick another path
// without a round trip. The host enforces on every call whatever Can said, so
// a stale or fabricated Grants can only mislead its own plugin.
func (g Grants) Can(kind GrantKind, s Subject) bool {
	return MatchedByAny(g.list(kind), s)
}

// CanPattern reports whether a whole pattern is covered — the question
// Subscribe asks, since a subscription addresses a family rather than one
// subject.
func (g Grants) CanPattern(kind GrantKind, p Pattern) bool {
	return CoveredByAny(g.list(kind), p)
}

// CanUse reports whether the named stream, KV bucket or object bucket was
// provisioned for this principal.
func (g Grants) CanUse(kind AssetKind, name string) bool {
	switch kind {
	case AssetStream:
		return slices.Contains(g.Streams, name)
	case AssetKV:
		return slices.Contains(g.KV, name)
	case AssetObjects:
		return slices.Contains(g.Objects, name)
	}
	return false
}

// Clone deep-copies, so a caller cannot widen its own grants by appending to a
// slice it was handed.
func (g Grants) Clone() Grants {
	return Grants{
		Publish:   slices.Clone(g.Publish),
		Subscribe: slices.Clone(g.Subscribe),
		Request:   slices.Clone(g.Request),
		Serves:    slices.Clone(g.Serves),
		Streams:   slices.Clone(g.Streams),
		KV:        slices.Clone(g.KV),
		Objects:   slices.Clone(g.Objects),
	}
}

// IsZero reports that nothing at all is granted.
func (g Grants) IsZero() bool {
	return len(g.Publish) == 0 && len(g.Subscribe) == 0 && len(g.Request) == 0 &&
		len(g.Serves) == 0 && len(g.Streams) == 0 && len(g.KV) == 0 && len(g.Objects) == 0
}

func (g Grants) list(kind GrantKind) []Pattern {
	switch kind {
	case GrantPublish:
		return g.Publish
	case GrantSubscribe:
		return g.Subscribe
	case GrantRequest:
		return g.Request
	case GrantServe:
		return g.Serves
	}
	return nil
}

// Identity is who this bus is bound to. It is set once per generation by the
// host at Bind and is read-only afterwards.
type Identity struct {
	PluginID   string
	Generation string
	Lease      Lease
	Role       Role
	Grants     Grants
}

// Autonomous reports that no human principal is attached to this generation.
func (i Identity) Autonomous() bool { return i.Lease.IsZero() }

// String is the attribution an operator reads in a refusal: the principal and
// the generation, never the lease token.
func (i Identity) String() string {
	if i.PluginID == "" {
		return "<unbound>"
	}
	if i.Generation == "" {
		return i.PluginID
	}
	return i.PluginID + "@" + i.Generation
}

// Clone deep-copies the grants.
func (i Identity) Clone() Identity {
	i.Grants = i.Grants.Clone()
	return i
}
