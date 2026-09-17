// Package v1compat lets a v1 plugin run unchanged on the v2 bus.
//
// The 31 in-tree plugins were written against v1's plugin.Host: a capability
// facade with Publish, Subscribe, Request, Logger, Profiling, StateDir, Every
// and BumpContributions. v2 replaced that facade with an embedded Base and one
// bus.Bus. Converting all 31 in a single change would be a flag day, and a flag
// day across a fleet of plugins is how a migration acquires a rollback it can
// never execute. This package is the seam that removes the flag day: the
// gateway hands a v1 plugin a Host built from a v2 Base, that plugin keeps its
// source, and the conversion happens one plugin at a time.
//
// It is a migration aid with a planned end. Nothing new should be written
// against it, and it is expected to be deleted when the last v1 plugin is
// converted.
//
// Three things are deliberately NOT faithful to v1, because each one was a
// defect that v2 exists to close:
//
//   - A caller-supplied Envelope.Source is DISCARDED, not forwarded. v1's host
//     rewrote Source to the calling plugin's id on the way out; v2's substrate
//     stamps bd-caller from the bound connection credential, where a plugin
//     cannot reach it. Forwarding what the plugin wrote would re-open exactly
//     the forgery v2 closed.
//   - A refusal is never swallowed. v1's direct host had silent refusal paths;
//     every error the v2 bus returns is returned here, and the two v1
//     signatures with nowhere to put one (Subscribe, BumpContributions) log at
//     Error instead of returning nil.
//   - Every is at-least-once where the substrate is durable. See Every.
package v1compat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"time"

	v1bus "github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
	v1plugin "github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// The two headers this package owns.
//
// A v1 Envelope carries an id and a timestamp; a v2 Msg has no field for
// either, because the substrate's own metadata covers what the host needs and
// the rest is the sender's business. They therefore travel as ordinary headers
// so a v1 publisher and a v1 subscriber still agree on them across the bus.
//
// Neither key carries the bd- prefix, and that is not cosmetic: bus.WithHeaders
// strips every bd-* key a principal supplies, so a bd--prefixed key here would
// be silently dropped on egress and the fields would arrive empty. A v2-native
// publisher sets neither, which is why the ingress side falls back to a fresh
// id and the receive time rather than treating their absence as an error.
const (
	// HeaderEnvelopeID carries v1 Envelope.ID.
	HeaderEnvelopeID = "v1-envelope-id"
	// HeaderEnvelopeTimestamp carries v1 Envelope.Timestamp in RFC3339Nano.
	HeaderEnvelopeTimestamp = "v1-envelope-ts"
)

// Closer is implemented by the Host this package returns.
//
// A v1 plugin's Stop cannot release what it never knew about: the adapter owns
// every v2 subscription and every durable schedule it opened on that plugin's
// behalf, and a v1 plugin that forgot its own unsubscribe would otherwise leak
// them into the next generation, which then delivers into a withdrawn service.
// The gateway therefore type-asserts the Host it built and closes it as the
// last step of revoking a generation, AFTER the plugin's own Stop has returned:
//
//	h := v1compat.Host(base)
//	// ... plug.Start(ctx, h) ... plug.Stop(ctx) ...
//	if c, ok := h.(v1compat.Closer); ok {
//		_ = c.Close(ctx)
//	}
//
// Close is idempotent, so a host that closes twice — or closes after the
// plugin already cancelled everything — is not a special case.
type Closer interface {
	Close(ctx context.Context) error
}

// Host returns a v1 plugin.Host backed by base's v2 bus.
//
// The v1 plugin sees exactly the interface it was written against; every call
// underneath goes through the v2 bus under that plugin's own identity and
// effective grants. Nothing is re-authorised here — the adapter has no
// privilege of its own and cannot widen anything, because the only bus it can
// reach is the one the host already bound to that generation.
//
// An unbound (zero) Base is accepted: its accessors return refusing doubles
// rather than nil, so the adapter never nil-checks them and every call simply
// refuses with an attributed bus.Fault.
//
// The returned Host also implements Closer. See Closer for how the gateway is
// expected to call it.
func Host(base *plugin.Base) v1plugin.Host {
	ctx, cancel := context.WithCancel(context.Background())
	return &hostAdapter{base: base, ctx: ctx, cancel: cancel}
}

type hostAdapter struct {
	base   *plugin.Base
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	closed   bool
	ticks    int            // per-host counter behind the tick.<id>.<n> subjects
	cleanups map[int]func() // everything Close must release
	nextID   int
}

var (
	_ v1plugin.Host = (*hostAdapter)(nil)
	_ Closer        = (*hostAdapter)(nil)
)

// Publish emits an event on the v2 bus.
//
// The subject is env.Type VERBATIM. There is deliberately no renaming table:
// the event. and cmd. namespaces are unchanged in v2 because the operation name
// is an input to the schema hash, so a rename would churn every descriptor in
// the fleet for no behavioural gain. An empty or unparseable Type is refused
// with FaultSchema naming the value rather than published to a subject the
// substrate would reject later and less legibly.
//
// env.Source is discarded — see the package comment. env.ID, env.Timestamp and
// env.CorrelationID have no v2 field, so they travel as metadata: the first two
// in this package's own headers, the correlation id through the trace context,
// which is the only path to bd-corr that exists (a principal cannot set a bd-*
// header, and bus.WithHeaders strips one it is handed).
//
// The bus's error is RETURNED, never logged and dropped.
func (h *hostAdapter) Publish(env v1bus.Envelope) error {
	subject, err := subjectOf(env.Type, "v1compat.Publish")
	if err != nil {
		return err
	}
	ctx := h.correlated(env.CorrelationID)
	return h.base.Bus().Publish(ctx, subject, env.Payload, bus.WithHeaders(egressHeaders(env)))
}

// Subscribe registers a handler for an event type and returns the unsubscribe.
//
// The v1 signature has nowhere to return an error, and that missing return is
// the seam that hid v1's refusals: a plugin whose subscription was denied or
// dropped past a registration limit waited forever for events nobody was
// sending it. The adapter cannot add a return value without changing the
// interface every v1 plugin compiled against, so it does the next loudest
// thing — it LOGS the fault at Error, naming the subject and the principal, and
// returns a working no-op unsubscribe. It never panics and never returns nil.
//
// A bare "*" is translated to ">" and logged at Warn. v1 code sometimes
// subscribes to the literal "*" meaning "everything"; in v2's grammar "*" is
// exactly one token and ">" is the tail wildcard, so the v1 spelling would
// silently match almost nothing. This is a known, intentional conversion, not a
// fallback.
//
// The returned unsubscribe is safe to call any number of times.
func (h *hostAdapter) Subscribe(eventType string, fn func(v1bus.Envelope)) func() {
	log := h.base.Logger()
	principal := h.base.Identity().String()

	raw := eventType
	if raw == "*" {
		raw = ">"
		log.Warn("v1compat: translated v1 subscribe pattern \"*\" to v2 \">\"",
			"plugin", principal, "pattern", eventType)
	}
	pattern, err := bus.ParsePattern(raw)
	if err != nil {
		log.Error("v1compat: subscribe refused: unparseable subject pattern",
			"plugin", principal, "pattern", eventType, "error", err)
		return func() {}
	}

	deliver := func(_ context.Context, m *bus.Msg) {
		if fn != nil {
			fn(envelopeOf(m))
		}
	}
	sub, err := h.base.Bus().Subscribe(h.ctx, pattern, deliver)
	if err != nil {
		log.Error("v1compat: subscribe refused",
			"plugin", principal, "subject", string(pattern), "error", err)
		return func() {}
	}

	var once sync.Once
	release := func() { once.Do(sub.Cancel) }
	return h.track(release)
}

// Request is the request/response form of Publish. Subject and payload map
// exactly as they do there, and the reply maps back through the same
// reconstruction Subscribe uses.
//
// A reply carrying bd-fault becomes an error — a bus.Fault with that code —
// rather than a successful Envelope the caller has to remember to inspect. That
// is the one place where "faithful to v1" and "loud" disagree and loud wins: a
// v1 caller that ignores the error now gets a zero Envelope instead of a
// plausible-looking one carrying a refusal in its payload.
func (h *hostAdapter) Request(ctx context.Context, env v1bus.Envelope) (v1bus.Envelope, error) {
	subject, err := subjectOf(env.Type, "v1compat.Request")
	if err != nil {
		return v1bus.Envelope{}, err
	}
	if env.CorrelationID != "" {
		ctx = h.base.Bus().Trace().WithCorrelation(ctx, env.CorrelationID)
	}
	reply, err := h.base.Bus().Request(ctx, subject, env.Payload, bus.WithHeaders(egressHeaders(env)))
	if err != nil {
		return v1bus.Envelope{}, err
	}
	if code := reply.Headers.Get(bus.HeaderFault); code != "" {
		return v1bus.Envelope{}, bus.Fault{
			Code:    code,
			Op:      string(subject),
			Message: string(reply.Data),
		}
	}
	return envelopeOf(reply), nil
}

// Logger returns the host logger, already tagged with this plugin's id.
//
// The v1 and v2 Logger method sets are identical — Info, Warn and Error over
// (string, ...any) — but identical method sets in two packages are still two
// types, and Go will not let one be returned where the other is declared. The
// wrapper is therefore a formality rather than a translation; it is here
// because the alternative does not compile.
func (h *hostAdapter) Logger() v1plugin.Logger { return loggerAdapter{h.base.Logger()} }

// Profiling returns this plugin's profiler switch, off by default. As with
// Logger, the v1 and v2 method sets are identical and the wrapper exists only
// because the types are declared in different packages.
func (h *hostAdapter) Profiling() v1plugin.Profiler { return profilerAdapter{h.base.Profiling()} }

// StateDir is where this plugin may persist state.
//
// v1 took the plugin id as an argument, which is how a plugin could ask for a
// peer's state directory and be given it. v2's Base already knows whose
// generation it is, so the argument is now only a claim to check: an empty id
// means "mine" and is honoured, an id equal to this plugin's is honoured, and
// anything else is a plugin reaching for someone else's state. That returns ""
// and logs at Error. It is not honoured, and it is not silently redirected to
// the caller's own directory either — a plugin that asked for another's
// directory and received its own would write the wrong data and never learn.
func (h *hostAdapter) StateDir(pluginID string) string {
	own := h.base.Identity().PluginID
	if pluginID != "" && pluginID != own {
		h.base.Logger().Error("v1compat: refused state dir for another plugin",
			"plugin", h.base.Identity().String(), "requested", pluginID)
		return ""
	}
	return h.base.StateDir()
}

// Every registers periodic work and returns the cancel.
//
// Where the substrate supports scheduling it is backed by bus.Scheduler on the
// subject tick.<pluginID>.<n>, with n a per-Host counter, plus a subscription
// that runs fn. That makes the schedule durable — and durable delivery is
// AT-LEAST-ONCE: a fire that was written but not yet delivered is re-delivered
// on recovery, so fn can run twice for one interval across a restart. This is a
// deliberate behaviour change from v1's in-process timer wheel, which lost the
// tick instead. A callback that was idempotent by accident under v1 has to
// become idempotent on purpose.
//
// Where the substrate does NOT support scheduling — Capabilities().Schedule
// false, which includes an unbound Base — it falls back to an in-process
// time.Ticker with v1's exact semantics: ticks DO NOT OVERLAP, because fn is
// called on the loop's own goroutine, so a callback slower than the interval
// delays the next tick rather than running concurrently with itself. The same
// fallback covers a scheduler that refuses, which is logged at Error: periodic
// work that silently stopped happening is the failure this whole package is
// trying not to reproduce.
//
// cancel is safe to call any number of times and releases both halves — the
// schedule and the subscription.
func (h *hostAdapter) Every(interval time.Duration, fn func()) func() {
	if fn == nil || interval <= 0 {
		h.base.Logger().Error("v1compat: refused Every: nil callback or non-positive interval",
			"plugin", h.base.Identity().String(), "interval", interval.String())
		return func() {}
	}
	if cancel, ok := h.scheduled(interval, fn); ok {
		return cancel
	}
	return h.ticker(interval, fn)
}

// scheduled installs the durable half of Every. It reports false when the
// substrate cannot schedule or refuses, which is the caller's signal to fall
// back to a ticker.
func (h *hostAdapter) scheduled(interval time.Duration, fn func()) (func(), bool) {
	b := h.base.Bus()
	if !b.Capabilities().Schedule {
		return nil, false
	}
	log := h.base.Logger()
	principal := h.base.Identity().String()

	h.mu.Lock()
	n := h.ticks
	h.ticks++
	h.mu.Unlock()

	name := "tick." + h.base.Identity().PluginID + "." + strconv.Itoa(n)
	subject, err := bus.ParseSubject(name)
	if err != nil {
		// An unbound or oddly-named plugin cannot spell a tick subject. That is
		// not a reason to stop ticking.
		log.Error("v1compat: Every falling back to an in-process ticker: unspellable tick subject",
			"plugin", principal, "subject", name, "error", err)
		return nil, false
	}

	sub, err := b.Subscribe(h.ctx, bus.Pattern(subject), func(context.Context, *bus.Msg) { fn() })
	if err != nil {
		log.Error("v1compat: Every falling back to an in-process ticker: tick subscribe refused",
			"plugin", principal, "subject", string(subject), "error", err)
		return nil, false
	}
	if err := b.Schedule().Every(h.ctx, name, interval, subject, nil); err != nil {
		sub.Cancel()
		log.Error("v1compat: Every falling back to an in-process ticker: schedule refused",
			"plugin", principal, "subject", string(subject), "error", err)
		return nil, false
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			sub.Cancel()
			if err := b.Schedule().Cancel(context.WithoutCancel(h.ctx), name); err != nil {
				log.Error("v1compat: cancelling a schedule failed",
					"plugin", principal, "schedule", name, "error", err)
			}
		})
	}
	return h.track(release), true
}

// ticker is the in-process fallback. fn runs on the loop goroutine, which is
// what makes ticks non-overlapping.
func (h *hostAdapter) ticker(interval time.Duration, fn func()) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-h.ctx.Done():
				return
			case <-t.C:
				fn()
			}
		}
	}()
	var once sync.Once
	return h.track(func() { once.Do(func() { close(stop) }) })
}

// BumpContributions signals that nav, panels or launchers changed and the shell
// should re-read /api/bootstrap.
//
// The v1 signature returns nothing, so a refusal cannot be propagated to the
// caller and is logged at Error instead. That is the whole of the compromise:
// the UI may not refresh, and the reason is in the log rather than in the
// caller's hands.
func (h *hostAdapter) BumpContributions() {
	const subject bus.Subject = "cmd.ui.v1.contributions.bump"
	if err := h.base.Bus().Publish(h.ctx, subject, nil); err != nil {
		h.base.Logger().Error("v1compat: contributions bump refused",
			"plugin", h.base.Identity().String(), "subject", string(subject), "error", err)
	}
}

// Close releases everything the adapter holds: every subscription it opened and
// every schedule it registered, in the reverse of the order they were created.
// See Closer for when the gateway calls it. It is idempotent, and a release
// registered after Close runs immediately rather than being remembered by a
// closed adapter.
func (h *hostAdapter) Close(context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	ids := make([]int, 0, len(h.cleanups))
	for id := range h.cleanups {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	// Reverse creation order, so a schedule's subscription outlives the
	// schedule that feeds it rather than the other way round.
	releases := make([]func(), 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		releases = append(releases, h.cleanups[ids[i]])
	}
	h.cleanups = nil
	h.mu.Unlock()

	// Outside the lock: a release calls back into the bus, and the schedule
	// cancel below still needs a live context, so the adapter context is
	// cancelled only once every release has run.
	for _, release := range releases {
		release()
	}
	h.cancel()
	return nil
}

// track registers a release with Close and returns a cancel that both runs it
// and forgets it, so a plugin that cancels properly does not leave the adapter
// holding a reference until the generation ends.
func (h *hostAdapter) track(release func()) func() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		release()
		return func() {}
	}
	if h.cleanups == nil {
		h.cleanups = map[int]func(){}
	}
	id := h.nextID
	h.nextID++
	h.cleanups[id] = release
	h.mu.Unlock()

	return func() {
		h.mu.Lock()
		delete(h.cleanups, id)
		h.mu.Unlock()
		release()
	}
}

// correlated returns the publish context, carrying id as the correlation when
// the envelope named one. bd-corr cannot be set through bus.WithHeaders — the
// substrate strips every bd-* key a principal supplies — so the trace context
// is the only route to it, and it is the right one: the substrate stamps the
// header itself on egress.
func (h *hostAdapter) correlated(id string) context.Context {
	if id == "" {
		return h.ctx
	}
	return h.base.Bus().Trace().WithCorrelation(h.ctx, id)
}

// subjectOf parses a v1 Envelope.Type as a v2 subject. The name is carried
// verbatim; only its shape is checked.
func subjectOf(eventType, op string) (bus.Subject, error) {
	subject, err := bus.ParseSubject(eventType)
	if err != nil {
		return "", bus.Fault{
			Code:    bus.FaultSchema,
			Op:      op,
			Message: "envelope type " + strconv.Quote(eventType) + " is not a valid subject",
			Err:     err,
		}
	}
	return subject, nil
}

// egressHeaders is what a v1 Envelope contributes to a v2 message: the headers
// the plugin set, plus the id and timestamp v2 has no field for. Anything bd-*
// the plugin supplied is dropped by bus.WithHeaders, which is where that rule
// belongs; re-implementing the strip here would give it a second place to drift.
func egressHeaders(env v1bus.Envelope) bus.Headers {
	out := make(bus.Headers, len(env.Headers)+2)
	for k, v := range env.Headers {
		out[k] = v
	}
	if env.ID != "" {
		out[HeaderEnvelopeID] = env.ID
	}
	if !env.Timestamp.IsZero() {
		out[HeaderEnvelopeTimestamp] = env.Timestamp.Format(time.RFC3339Nano)
	}
	return out
}

// envelopeOf rebuilds the v1 Envelope a v1 handler expects from a v2 message.
//
// Source comes from bd-caller, via bus.CallerOf — the value the SUBSTRATE
// stamped from the connection's own credential, not anything the sender wrote.
// A v2-native publisher carries neither of this package's metadata headers, so
// ID falls back to a fresh id and Timestamp to the receive time; that keeps a
// v1 handler's ID non-empty, which some of them log or de-duplicate on.
func envelopeOf(m *bus.Msg) v1bus.Envelope {
	if m == nil {
		return v1bus.Envelope{ID: newID(), Timestamp: time.Now().UTC()}
	}
	env := v1bus.Envelope{
		ID:            m.Headers.Get(HeaderEnvelopeID),
		CorrelationID: m.Headers.Get(bus.HeaderCorrelation),
		Type:          string(m.Subject),
		Source:        bus.CallerOf(m).PluginID,
		Headers:       inboundHeaders(m.Headers),
	}
	if len(m.Data) > 0 {
		env.Payload = json.RawMessage(m.Data)
	}
	if env.ID == "" {
		env.ID = newID()
	}
	if ts := m.Headers.Get(HeaderEnvelopeTimestamp); ts != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			env.Timestamp = parsed
		}
	}
	if env.Timestamp.IsZero() {
		env.Timestamp = time.Now().UTC()
	}
	return env
}

// inboundHeaders is what a v1 handler sees in Envelope.Headers: the
// non-reserved headers, minus this package's own two, which were promoted to
// Envelope.ID and Envelope.Timestamp. Leaving them in as well would make a
// round-trip through the adapter disagree with itself about where the id lives.
func inboundHeaders(h bus.Headers) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		switch {
		case bus.IsReservedHeader(k):
		case k == HeaderEnvelopeID, k == HeaderEnvelopeTimestamp:
		default:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// newID mints an envelope id for a message that arrived without one. It is
// random rather than a counter because two adapters in one process would
// otherwise hand out the same ids.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any supported platform; if it ever does,
		// a timestamp-derived id is still better than an empty one.
		return "v1c-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// loggerAdapter presents a v2 Logger as a v1 one. See hostAdapter.Logger.
type loggerAdapter struct{ l plugin.Logger }

func (a loggerAdapter) Info(msg string, args ...any)  { a.l.Info(msg, args...) }
func (a loggerAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, args...) }
func (a loggerAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }

// profilerAdapter presents a v2 Profiler as a v1 one. See hostAdapter.Profiling.
type profilerAdapter struct{ p plugin.Profiler }

func (a profilerAdapter) Enabled() bool    { return a.p.Enabled() }
func (a profilerAdapter) Set(enabled bool) { a.p.Set(enabled) }
