package plugin

import (
	"context"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// Optional interfaces that sit BESIDE Host and Plugin.
//
// Host's method set is pinned (TestHostMethodSetIsPinned): every deployed plugin
// links its own copy of the interface, so adding a method to it would break all
// of them at once and force a rebuild and re-consent. These arrive alongside
// instead. A plugin type-asserts for what it wants; an older host simply does
// not implement it, and the plugin keeps running.

// ObservableRegistrar is Subscribe and Every with the refusal visible
// (gateway ADR 0026 E1: every refusal is observable).
//
// Host.Subscribe and Host.Every return only a cancel, so a registration refused
// past the per-generation cap, or refused because the grant is missing, is a
// silent no-op: the plugin believes it is subscribed and simply never hears
// anything. These forms say so. The cancel is still safe to call on refusal.
type ObservableRegistrar interface {
	SubscribeErr(eventType string, h func(bus.Envelope)) (cancel func(), err error)
	EveryErr(interval time.Duration, fn func()) (cancel func(), err error)
}

// Draining is an optional PLUGIN interface: the host calls OnDrain before it
// revokes the generation, while the bus, timers and state dir still work
// (ADR 0026 B3). A hook next to Stop would be dead on arrival, because every
// subscription and timer is disposed by then.
//
// It may not veto. An error is reported and teardown continues, because a
// plugin that can refuse to stop cannot be turned off (ADR 0026 B2). The context
// carries the host's per-plugin teardown budget: return when it is done, and
// expect the host to proceed regardless.
type Draining interface {
	OnDrain(ctx context.Context) error
}

// DataVersion is the version of what a plugin left in its state dir. It is an
// opaque string chosen by the plugin, compared only for equality.
type DataVersion string

// DataVersioned is the host side of the version record (ADR 0026 B4): which
// version last wrote this plugin's state dir. It is scoped to the calling
// plugin, so there is no id to pass and no other plugin's record to reach.
//
// It exists because an upgrade hook has no "from" to pass until the host records
// this. Read it in Start, migrate if it differs from what you write today, then
// set it.
type DataVersioned interface {
	DataVersion() (DataVersion, bool)
	SetDataVersion(DataVersion) error
}
