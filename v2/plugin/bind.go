package plugin

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"

// Binding is what the host installs at Bind, once per generation, after
// negotiate and before Start.
//
// It is a struct rather than six arguments because it is the unit the host
// reasons about: a generation gets exactly one of these, and the fields either
// all belong to that generation or none of them do.
type Binding struct {
	// Bus is this generation's messaging surface, already bound to Identity and
	// its effective grants. It is required; a Binding without one is refused.
	Bus bus.Bus
	// Logger is the host logger, tagged with the plugin id.
	Logger Logger
	// Profiler is this plugin's profiling switch.
	Profiler Profiler
	// StateDir is where this plugin may persist state.
	StateDir string
	// Identity is who the generation belongs to. Identity.Generation is what
	// Bind uses to tell a re-bind from a new generation.
	Identity bus.Identity
	// Caps are the capabilities NEGOTIATED for this generation, which can be
	// narrower than what the substrate implements. Bus().Capabilities() reports
	// these, not the substrate's own.
	Caps bus.Capabilities
}

// Bind installs the binding on p's embedded Base. It is the ONLY way a Base
// becomes live.
//
// Calling it twice for the same generation is an error, not a silent overwrite.
// That is the whole reason this function returns anything: a stale generation
// that re-binds would quietly take the live one's place, and the live plugin
// would go on publishing into a bus that had been swapped out from under it
// with no event anywhere to explain the silence. A second Bind with the same
// Identity.Generation therefore returns bus.Fault{Code: bus.FaultConflict} and
// leaves the existing binding EXACTLY as it was.
//
// A Bind carrying a DIFFERENT Identity.Generation is allowed and replaces the
// binding, because that is what a new generation is: the host has torn the old
// one down and is installing its successor into the same plugin value. Two
// bindings that differ in every field but the generation string are still the
// same generation and still conflict — the generation is the identity, not the
// contents.
//
// Bind is safe to call concurrently with any accessor on Base.
func Bind(p Bound, b Binding) error {
	if p == nil {
		return bus.Fault{Code: bus.FaultUnavailable, Op: "plugin.Bind", Message: "nil plugin"}
	}
	base := p.base()
	if base == nil {
		return bus.Fault{Code: bus.FaultUnavailable, Op: "plugin.Bind", Message: "plugin has no embedded Base"}
	}
	if b.Bus == nil {
		return bus.Fault{
			Code:    bus.FaultUnavailable,
			Op:      "plugin.Bind",
			Message: "binding for " + b.Identity.String() + " carries no bus",
		}
	}

	base.mu.Lock()
	defer base.mu.Unlock()
	if base.bound && base.identity.Generation == b.Identity.Generation {
		return bus.Fault{
			Code:    bus.FaultConflict,
			Op:      "plugin.Bind",
			Message: "generation " + b.Identity.String() + " is already bound; a second Bind would replace a live binding",
		}
	}

	base.bound = true
	base.bus = capsBus{Bus: b.Bus, caps: b.Caps}
	base.logger = b.Logger
	base.profiler = b.Profiler
	base.stateDir = b.StateDir
	// Clone so a host that keeps its own Grants value and appends to a slice
	// afterwards cannot widen what this plugin's preflight reports.
	base.identity = b.Identity.Clone()
	return nil
}
