package plugin

import "slices"

// Kit is what a plugin inherits by being a plugin (ADR 0026 A1): the admitted
// Host and the capabilities negotiated for this generation.
//
// A plugin holds a Kit as a field and calls through it. It is never embedded, so
// no method is promoted onto the plugin type; that is the same delegation Registrar
// uses, and the reason embedding was rejected (payload_gen.go).
//
// NewKitFrom does NOT negotiate. ServePlugin, or the in-process host, has already
// negotiated, and Negotiate costs an execution slot. The capabilities passed in are
// a local cache that Can reads. The host still enforces authority on every call
// whatever the cache says, so a fabricated or stale cache can only make Can mislead
// its own plugin (a UX bug). It can never widen what the host allows.
type Kit struct {
	host Host
	caps HostCapabilities
}

// GrantKind names which list of the negotiated grants Can reads.
type GrantKind string

const (
	GrantPublish   GrantKind = "publish"
	GrantSubscribe GrantKind = "subscribe"
	GrantRequest   GrantKind = "request"
)

// NewKitFrom builds a Kit from a host and the capabilities already negotiated for
// it. It copies caps, so a caller changing its own value afterwards cannot change
// what the Kit reports.
func NewKitFrom(h Host, caps HostCapabilities) *Kit {
	caps.Features = slices.Clone(caps.Features)
	caps.Hooks = slices.Clone(caps.Hooks)
	caps.Grants = Permissions{
		Publish:   slices.Clone(caps.Grants.Publish),
		Subscribe: slices.Clone(caps.Grants.Subscribe),
		Request:   slices.Clone(caps.Grants.Request),
	}
	return &Kit{host: h, caps: caps}
}

// Host is the admitted host. Every call made through it is enforced by the host,
// independently of Can.
func (k *Kit) Host() Host {
	if k == nil {
		return nil
	}
	return k.host
}

// Capabilities returns a copy of the negotiated capabilities the Kit was built from.
func (k *Kit) Capabilities() HostCapabilities {
	if k == nil {
		return HostCapabilities{}
	}
	return NewKitFrom(nil, k.caps).caps
}

// Can reports whether the negotiated grants name operation for kind. The host
// matches grants by exact operation name, so Can does too.
//
// true means the operation was granted for this generation, which is necessary
// but not sufficient: the host can still refuse one particular envelope by its
// resource rule. false means a call will be refused. Can never calls the host.
func (k *Kit) Can(kind GrantKind, operation string) bool {
	if k == nil || operation == "" {
		return false
	}
	switch kind {
	case GrantPublish:
		return slices.Contains(k.caps.Grants.Publish, operation)
	case GrantSubscribe:
		return slices.Contains(k.caps.Grants.Subscribe, operation)
	case GrantRequest:
		return slices.Contains(k.caps.Grants.Request, operation)
	}
	return false
}
