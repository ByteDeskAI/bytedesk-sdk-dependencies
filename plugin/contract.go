package plugin

import (
	"context"
	"net/http"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// The plugin contract (ADR 0024).
//
// Before this file the SDK shipped the nouns — Manifest, NavItem, PanelSpec,
// the lifecycle states, bus.Envelope — but not the verbs. Plugin and Host lived
// only inside the gateway, so nothing a plugin author imported described what a
// plugin actually is. First-party plugins implemented the gateway's private
// interface; third-party plugins got a bare http.Handler and an iframe. They
// were two programming models wearing one name.
//
// These interfaces are that contract, owned here and aliased by the host. What
// they deliberately do NOT encode is where the plugin runs: linked into the
// gateway binary or spawned as its own process is a deployment decision the
// host reads from the manifest's spawn field. The host supplies a Host
// implementation for each mode — direct in-process, or RPC over a unix socket —
// and a plugin cannot tell which it was handed.
//
// Every Host method is therefore constrained to be marshallable. That is not an
// accident of the current implementation; it is the property that keeps the two
// modes interchangeable, and a method that cannot cross a socket does not
// belong on this interface.

// Plugin is what every plugin implements, in either deployment mode.
type Plugin interface {
	// ID is the plugin id. It must equal Manifest().ID and match the
	// directory/package name the host discovers it under.
	ID() string

	// Manifest declares nav, panels, launchers, scopes, routes, composition and
	// commercial terms. Host-authoritative fields (role, targets, pricing,
	// publisher, minCoreVersion) are filled by the host and should be left
	// unset here rather than restated.
	Manifest() Manifest

	// Start acquires resources and publishes the plugin's service. The host
	// hands it a Host for the deployment mode in use. Start must not block:
	// register periodic work with Host.Every rather than owning a goroutine.
	Start(ctx context.Context, host Host) error

	// Stop releases what Start acquired and must drain any work already
	// dispatched, honouring the caller's deadline. A plugin that returns from
	// Stop while a callback is still running will race the next generation.
	Stop(ctx context.Context) error
}

// Logger is the host's logger, narrowed to what a plugin may use.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Host is the capability facade a plugin is given at Start. It is the only
// channel to the host: a plugin never touches host secrets, never reaches
// another plugin directly, and never loads, execs or proxies a peer.
//
// Both implementations satisfy this identically:
//
//	direct — in-process, calling the kernel straight through
//	rpc    — out-of-process, marshalling over a unix socket
type Host interface {
	// Publish emits an event. Fire-and-forget; delivery to subscribers is the
	// host's problem, not the caller's.
	Publish(env bus.Envelope) error

	// Subscribe registers a handler for an event type and returns the
	// unsubscribe. Call the unsubscribe from Stop — a subscription outliving
	// its plugin generation delivers into a withdrawn service.
	Subscribe(eventType string, h func(bus.Envelope)) (unsubscribe func())

	// Request is the request/response form of Publish, used for commands that
	// have an answer. The context bounds the wait.
	Request(ctx context.Context, env bus.Envelope) (bus.Envelope, error)

	// Logger returns the host logger, already tagged with this plugin's id.
	Logger() Logger

	// StateDir is where this plugin may persist state. The host owns the path;
	// a plugin must not assume it is under any particular root.
	StateDir(pluginID string) string

	// Every registers periodic work on the host's single timer wheel and
	// returns the cancel. Call it from Start and the cancel from Stop. Do not
	// own a ticker and a goroutine per plugin — that is what this exists to
	// prevent. Ticks do not overlap: a slow callback delays the next tick
	// rather than running concurrently with itself.
	Every(interval time.Duration, fn func()) (cancel func())

	// BumpContributions signals that nav, panels or launchers changed and the
	// shell should re-read /api/bootstrap.
	BumpContributions()
}

// Optional capabilities, interface-segregated. A plugin implements only what it
// needs and the host type-asserts for each. Adding a method to Plugin or Host
// is a breaking change for every out-of-process plugin; adding a new optional
// interface here is not.

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

// CommandHandler handles bus commands addressed to this plugin.
type CommandHandler interface {
	Handles() []string
	HandleCommand(ctx context.Context, env bus.Envelope) (bus.Envelope, error)
}

// Validator checks preconditions before Start. A validation failure moves the
// plugin to failed; for a plugin marked critical it halts the host, so return
// an error here only for something genuinely unrecoverable.
type Validator interface {
	Validate(ctx context.Context, host Host) error
}

// Readier reports post-Start readiness. A non-nil error marks the plugin
// degraded with that error as the reason, which is the right signal for a
// dependency that is missing but might return.
type Readier interface {
	Ready(ctx context.Context) error
}
