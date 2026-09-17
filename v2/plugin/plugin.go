// Package plugin is the v2 plugin contract: what a plugin embeds, what the host
// installs into it, and the typed layer over the bus.
//
// The shape changed in one decisive way from v1. A v1 plugin was handed a Host
// at Start and called through it; every capability the host wanted to offer had
// to become a method on that interface, and every method added to it broke
// every out-of-process plugin at once. v2 inverts it: a plugin EMBEDS Base, the
// host installs one Binding into it before Start, and everything a plugin can
// do it does through the one bus.Bus it inherits. Start therefore takes no host
// argument — by the time it runs, the base is already live.
//
// Base carries accessors and nothing else, which is what makes embedding safe.
// Go promotes the methods of an embedded type onto its outer type, so a base
// that carried a lifecycle method would silently make every plugin implement an
// interface it never wrote. TestBaseMethodSetIsAccessorsOnly holds that line by
// reflection rather than by review.
package plugin

import (
	"context"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Plugin is what every plugin implements.
//
// It embeds Bound, so a type can only satisfy Plugin by embedding Base. That is
// deliberate: a plugin the host cannot bind is a plugin the host cannot give a
// bus to, and there is no second way to reach the gateway.
type Plugin interface {
	Bound

	// ID is the plugin id. It must equal Manifest().ID and match the
	// directory/package name the host discovers it under.
	ID() string

	// Manifest declares nav, panels, launchers, subjects, assets, composition
	// and commercial terms. Host-authoritative fields are filled by the host
	// and should be left unset here rather than restated.
	Manifest() Manifest

	// Start acquires resources and publishes the plugin's services. There is no
	// host argument: Bind has already installed this generation's bus, logger,
	// profiler, state dir and identity, and Bus() is live before Start is
	// called.
	//
	// Start must not block. Register periodic work with Bus().Schedule() rather
	// than owning a goroutine.
	Start(ctx context.Context) error

	// Stop releases what Start acquired and must drain any work already
	// dispatched, honouring the caller's deadline. A plugin that returns from
	// Stop while a callback is still running will race the next generation.
	Stop(ctx context.Context) error
}

// Logger is the host's logger, narrowed to what a plugin may use. It is
// unchanged from v1: the narrowing is the contract, because a plugin that could
// reach the host's own logger could reconfigure the host's output.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Profiler is this plugin's host-owned profiling switch. Off is the default.
// Set takes effect on the next bus call, subscription or tick without a
// restart. Enabling this plugin does not enable another. Spawned plugins
// profile their own process; in-process plugins are attributed with pprof
// labels on the shared gateway runtime.
type Profiler interface {
	Enabled() bool
	Set(enabled bool)
}

// NopProfiler is always off. Test hosts, and the unbound Base, return it.
func NopProfiler() Profiler { return nopProfiler{} }

type nopProfiler struct{}

func (nopProfiler) Enabled() bool { return false }
func (nopProfiler) Set(bool)      {}

// nopLogger is what an unbound Base returns. It discards rather than refusing,
// because a plugin that logs during construction — before the host has bound
// anything — is doing something reasonable, and a refusal it cannot see is
// worth nothing anyway. Every OTHER accessor on an unbound Base refuses loudly;
// logging is the one place where silence is the right answer.
type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// unbound builds the refusal every accessor of an unbound Base produces. The
// code is FaultWithdrawn rather than FaultUnavailable because that is what it
// is from the caller's side: this generation's binding is not there — either
// not yet installed, or already replaced by a later one.
func unbound(op string) bus.Fault {
	return bus.Fault{
		Code:    bus.FaultWithdrawn,
		Op:      op,
		Message: "plugin base is not bound: the host has installed no binding for this generation",
	}
}
