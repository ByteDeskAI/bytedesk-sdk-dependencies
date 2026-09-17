package plugin

import (
	"context"
	"net/http"
)

// Optional interfaces that sit BESIDE Plugin.
//
// Plugin's method set is small and closed on purpose: every deployed
// out-of-process plugin links its own copy of it, so adding a method there
// breaks all of them at once and forces a rebuild and a re-consent. A new
// capability arrives as a new interface here instead. A plugin implements only
// what it needs, the host type-asserts for each, and a plugin that implements
// none of them is a perfectly ordinary plugin.
//
// RETIRED IN v2 — do not look for these, and do not reintroduce them:
//
//   - Host          the plugin no longer receives a capability facade; it
//                   embeds Base and calls Bus().
//   - Kit           same: Kit existed to hold a Host and a capability cache.
//                   Base holds the binding, and Bus().Capabilities() and
//                   Identity().Grants() answer what Kit.Can answered.
//   - CommandHandler commands are bus SERVICES now. Mount an endpoint on
//                   svc.<id>.<point>.v1.<op> (see typed.Serve and Registrar)
//                   instead of answering a dispatch callback.
//   - StatusSubscriber  Bus().Subscribe returns a bus.Subscription with Done
//                   and Err already, so there is nothing left to opt into.
//   - ObservableRegistrar  same reason: every bus registration returns its own
//                   refusal, so there is no silent-no-op form to correct.
//   - Negotiator    negotiation happens before Bind and its result arrives as
//                   Binding.Caps.
//   - ExtensionRegistrar  extension points are services. A provider mounts the
//                   point's subject and the host DISCOVERS it (see points.go);
//                   it no longer registers itself through a host callback.

// HTTPPlugin serves HTTP. The returned handler is mounted at each entry in the
// manifest's Routes. In-process the host mounts it on the shared mux; out of
// process the host proxies those same paths to the plugin socket WITHOUT
// rewriting them, so the handler observes identical request paths either way.
// Match on the full path — do not assume a stripped prefix.
type HTTPPlugin interface {
	Handler() http.Handler
}

// HealthContributor contributes rows to the operator health view. Each map is
// one section: {id, title, status, detail}.
type HealthContributor interface {
	HealthSections(ctx context.Context) []map[string]any
}

// Validator checks preconditions before Start. It takes no host argument in v2
// because the base is already bound when it runs: Bus(), StateDir() and
// Identity() are all live, which is what a precondition check needs.
//
// A validation failure moves the plugin to failed; for a plugin marked critical
// it halts the host, so return an error here only for something genuinely
// unrecoverable.
type Validator interface {
	Validate(ctx context.Context) error
}

// Readier reports post-Start readiness. A non-nil error marks the plugin
// degraded with that error as the reason, which is the right signal for a
// dependency that is missing but might return.
type Readier interface {
	Ready(ctx context.Context) error
}

// ActivationChecker runs after Start and before publication. Failure rolls the
// candidate generation back. Unlike Readier this is an admission condition, not
// a degraded-health report for a plugin that is already available.
type ActivationChecker interface {
	CheckActivation(ctx context.Context) error
}

// Draining is called before the host revokes the generation, while the bus,
// schedules and state dir still work. A hook next to Stop would be dead on
// arrival, because every subscription and schedule is disposed by then.
//
// It may not veto. An error is reported and teardown continues, because a
// plugin that can refuse to stop cannot be turned off. The context carries the
// host's per-plugin teardown budget: return when it is done, and expect the
// host to proceed regardless.
type Draining interface {
	OnDrain(ctx context.Context) error
}

// DataVersion is the version of what a plugin left in its state dir. It is an
// opaque string chosen by the plugin, compared only for equality.
type DataVersion string

// DataVersioned is the version record for a plugin's own state dir: which
// version last wrote it. It is scoped to the calling plugin, so there is no id
// to pass and no other plugin's record to reach.
//
// It exists because an upgrade hook has no "from" to pass until something
// records this. Read it in Start, migrate if it differs from what you write
// today, then set it.
type DataVersioned interface {
	DataVersion() (DataVersion, bool)
	SetDataVersion(DataVersion) error
}
