package plugin

import (
	"context"
	"io"
	"time"
)

// The session backend contract (agent-fabric ADR 0002).
//
// The host's terminal runtime drove local tmux directly, so a session could
// only ever be a process on the host's own machine. This point is the seam:
// the runtime asks a SessionBackend for the session instead, and the tab record
// remembers which backend answered. The existing local tmux code becomes the
// "local" backend without moving; a remote backend (Agent Fabric) registers
// beside it, and the terminal list, attach, input and capture paths stay the
// same for both.
//
// Unlike the other points in this package, this one is NOT marshallable and so
// is in-process only: Attach hands back a live byte stream, which no command
// envelope carries. A spawned plugin cannot implement it until the host can
// grant byte streams (gateway ADR 0025). Everything here is therefore a plain
// Go interface with no wire schema, no bd classification tags and no generated
// TypeScript.

// SessionBackendPoint is the extension point a plugin implements to supply
// terminal sessions. The host owns it, so only the host and plugins compiled
// into it may declare it (see HostPointNamespace).
const SessionBackendPoint = HostPointNamespace + "session.backend"

// SessionBackendLocal is the id of the host's built-in local tmux backend. It
// is the only id the host may assume: a tab with no recorded backend predates
// this point and belongs to the local one. Every other backend chooses its own
// id and the host learns it from ID(), so no id but this one is hard-coded.
const SessionBackendLocal = "local"

// The states a backend reports for a session. They exist so the host can
// reconcile after a restart without knowing anything about the backend: a
// running session keeps its tab, and anything else means the tab is finished.
const (
	SessionStateRunning = "running"
	SessionStateExited  = "exited"
)

// SessionBackend creates and drives terminal sessions on behalf of the host.
//
// Implementations are called concurrently and must be safe for that. Every
// method must honour the caller's context deadline and must refuse a
// SessionRef it did not issue rather than acting on some other session.
type SessionBackend interface {
	// ID is this backend's stable id, lowercase and unchanging across
	// restarts and releases, because the host persists it in each tab record
	// and routes every later call for that tab back here by it. The built-in
	// local tmux backend answers SessionBackendLocal.
	ID() string

	// Create starts one session and returns the reference the host persists.
	// It is called exactly once per tab, when the tab is created, and never
	// again for the same tab: restart reconciliation uses List, so a tab whose
	// session is gone is closed, not re-created.
	//
	// Create is all-or-nothing. If it returns an error it must leave nothing
	// running, because the host has no reference with which to clean up.
	Create(ctx context.Context, spec SessionSpec) (SessionRef, error)

	// Close ends the session and releases what Create acquired. It is
	// idempotent: a ref that has already exited or already been closed returns
	// nil, because the host closes on paths that race the session's own exit.
	Close(ctx context.Context, ref SessionRef) error

	// List reports every session this backend currently holds. The host calls
	// it to reconcile its tabs after a restart, so it must observe live state
	// rather than a cache written by this process, and it must never create,
	// revive or mutate anything. A ref the host holds but List omits is gone
	// for good.
	List(ctx context.Context) ([]SessionStatus, error)

	// Attach opens a byte stream to the session's PTY, sized to cols by rows.
	//
	// The stream carries PTY bytes unchanged in both directions: no framing, no
	// encoding, no added echo, no translated control sequences. The host pumps
	// it straight between the session and the browser's terminal, so anything
	// added here reaches the user as corruption.
	//
	// Several attaches to one ref may be open at once, each with its own
	// stream. Closing a stream detaches only; the session keeps running until
	// Close.
	Attach(ctx context.Context, ref SessionRef, cols, rows int) (Stream, error)

	// SendText writes text to the session as though it had been typed, and
	// when enter is true follows it with a carriage return. It does not need an
	// attached stream, and it must not open one.
	SendText(ctx context.Context, ref SessionRef, text string, enter bool) error

	// Capture returns the last lines of the session's scrollback as plain
	// text, most recent last. Zero or fewer lines means the visible screen
	// only. It is read-only and must not disturb the session or any attach.
	Capture(ctx context.Context, ref SessionRef, lines int) (string, error)
}

// Stream is one attachment to a session's PTY. Read yields output bytes, Write
// delivers input bytes, and Close detaches without ending the session.
type Stream interface {
	io.ReadWriteCloser

	// Resize tells the session the terminal is now cols by rows. It applies to
	// the session, not to this stream, so concurrent attaches see it too.
	Resize(cols, rows int) error
}

// SessionSpec is what the host knows about a session at creation. A backend
// uses the fields its placement makes meaningful and ignores the rest: CWD is a
// path on the host's own machine and means nothing to a remote backend, while
// RepoURL and BaseRef are how a remote backend obtains the same code.
type SessionSpec struct {
	// TabID is the host tab this session belongs to. It is unique and stable,
	// and is the only id the host can correlate by before Create returns.
	TabID string
	// ProjectID is the project the tab was launched from, empty for a tab
	// launched outside one.
	ProjectID string
	// Kind is the agent kind the tab runs, as the host names it.
	Kind string
	// Command is the command line to run in the session.
	Command string
	// CWD is the working directory, on the host's machine.
	CWD string
	// RepoURL and BaseRef are the repository and starting ref a backend that
	// is not on the host's machine must obtain before it can run Command.
	RepoURL string
	BaseRef string
	// Placement is the host's request for where the session should run, in
	// whatever form the backend published. It is advisory; the backend decides.
	Placement string
	// Env is added to the session's environment. It must not carry secrets:
	// a backend obtains credentials through its own trusted path, never here.
	Env map[string]string
}

// SessionRef identifies one live session. The host persists it with the tab and
// passes it back unchanged, so a backend must be able to resolve one issued by
// an earlier run of itself.
type SessionRef struct {
	// Backend is the issuing backend's ID(). The host routes by it.
	Backend string
	// ID identifies the session within that backend and is opaque to the host.
	ID string
	// Location is where the session runs, for display only. The host shows it
	// as a badge and never parses or routes on it.
	Location string
}

// SessionStatus is one row of List.
type SessionStatus struct {
	// Ref is the session, as Create returned it.
	Ref SessionRef
	// State is SessionStateRunning or SessionStateExited.
	State string
	// AgentStatus and Objective are what the agent in the session reports
	// about itself, for display. Both may be empty, and a backend that cannot
	// see inside the session always leaves them empty.
	AgentStatus string
	Objective   string
	// LastActivity is when the session last produced output, zero if unknown.
	LastActivity time.Time
}
