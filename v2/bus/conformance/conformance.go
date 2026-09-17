// Package conformance is the property list every bus substrate must satisfy.
//
// It is the contract for the whole programme, not a convenience: the gateway
// runs this same list against its in-memory substrate and against embedded
// NATS, and a plugin author runs it against the SDK's test double. One list,
// several runners. A property that is vague here becomes a substrate
// difference nobody catches later, so every property asserts on an observable
// the contract names — a delivered message, a bus.Fault code, a bound in
// wall-clock time — and never on the absence of an error alone.
//
// The package deliberately lives in non-test files: it is imported by other
// packages' tests, so it must compile into the library.
package conformance

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Harness is what a substrate provides to be proven.
type Harness struct {
	// New returns a Bus bound to id. Called many times per property.
	New func(t *testing.T, id bus.Identity) bus.Bus

	// Restart restarts the substrate keeping durable state. Every Bus from a
	// previous New is dead afterwards. Nil means the substrate cannot restart
	// and every [Harness.Caps] Durable-restart property skips.
	Restart func(t *testing.T)

	// Revoke ends pluginID's credential. Nil means revoke cannot be tested.
	Revoke func(t *testing.T, pluginID string)

	// Caps is what the substrate implements. Properties gated on a capability
	// it lacks are skipped.
	Caps bus.Capabilities

	// Deny is the permanently-ineligible family set the substrate compiled
	// in. PermanentDenyWins publishes into it. Must be non-empty.
	Deny []bus.Pattern

	// Default marks this substrate as the one the gateway runs by default. A
	// property in the requiredForDefault set may then NOT be skipped: a
	// missing capability is a hard failure, not a skip.
	Default bool

	// Budget scales every wait. Zero means 5s. CI runs the gateway suite with
	// GOMAXPROCS=1 -p 1, so budgets are generous on purpose.
	Budget time.Duration
}

// Property names one entry of the list.
type Property struct {
	Name               string
	Requires           []string
	RequiredForDefault bool
}

// Properties returns the list, in order, for a caller that wants to report or
// filter it. Requires holds capability names from bus.CapabilityNames().
func Properties() []Property {
	out := make([]Property, 0, len(properties))
	for _, p := range properties {
		out = append(out, Property{
			Name:               p.Name,
			Requires:           append([]string(nil), p.Requires...),
			RequiredForDefault: p.RequiredForDefault,
		})
	}
	return out
}

// Deferred names the capabilities in bus.CapabilityNames() that no property
// proves, each with the reason it is not yet worth a cross-implementation
// guarantee.
//
// It exists so that a gap is VISIBLE rather than absent. A capability name a
// manifest may declare in "needs", the host checks with Capabilities.Has, and
// no property compares two implementations on, is a promise nobody keeps; the
// only thing worse than deferring one is deferring it silently.
// TestCapabilityVocabularyIsExactlyWhatTheSuiteProves holds this map and the
// property list to the vocabulary between them, so a name can be covered or
// deferred and nothing else.
//
// An entry here is a debt, not a decision. Delete it by writing the property.
func Deferred() map[string]string {
	return map[string]string{
		"trace": "broker-side message tracing is not built in any substrate: " +
			"bus.Capabilities documents Trace as false until it is, and the bus " +
			"exposes no surface a property could exercise — Bus.Trace() is bd-corr " +
			"and traceparent propagation, which works regardless of this flag and is " +
			"deliberately not gated on it. A property written now could only assert " +
			"that nothing happens.",
	}
}

// Run executes every property as a subtest named exactly for the property.
func Run(t *testing.T, h Harness) {
	t.Helper()
	if h.New == nil {
		t.Fatal("conformance: Harness.New is nil; a substrate that cannot hand out a Bus cannot be proven")
	}
	if len(h.Deny) == 0 {
		t.Fatal("conformance: Harness.Deny is empty; PermanentDenyWins has nothing to publish into, and a substrate with no permanent deny set is not the substrate this contract describes")
	}
	for _, p := range properties {
		s := &suite{h: h, p: p.Property}
		t.Run(p.Name, func(t *testing.T) {
			if missing := h.Caps.Missing(p.Requires); len(missing) > 0 {
				if h.Default && p.RequiredForDefault {
					t.Fatalf("default substrate lacks %q, which %s requires; the default substrate may not skip a required property", missing[0], p.Name)
				}
				t.Skipf("substrate lacks %q", missing[0])
			}
			p.run(t, s)
		})
	}
}

// suite carries the harness plus the property currently running, because
// whether a missing hook skips or fails hard depends on the property.
type suite struct {
	h Harness
	p Property
}

func (s *suite) budget() time.Duration {
	if s.h.Budget > 0 {
		return s.h.Budget
	}
	return 5 * time.Second
}

// settle is the quiet window a negative assertion waits out. It is never the
// synchronisation primitive for a success path.
func (s *suite) settle() time.Duration {
	d := s.budget() / 20
	if d < 25*time.Millisecond {
		return 25 * time.Millisecond
	}
	return d
}

// deadline returns a context bounded by the budget.
func (s *suite) deadline() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.budget())
}

// bus builds a Bus for id and closes it when the test ends.
func (s *suite) bus(t *testing.T, id bus.Identity) bus.Bus {
	t.Helper()
	b := s.h.New(t, id)
	if b == nil {
		t.Fatalf("Harness.New returned a nil Bus for %s", id)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// needRestart demands the restart hook. A default substrate may not skip a
// required property for want of one.
func (s *suite) needRestart(t *testing.T) {
	t.Helper()
	if s.h.Restart != nil {
		return
	}
	if s.h.Default && s.p.RequiredForDefault {
		t.Fatalf("default substrate must provide Harness.Restart: %s is required for the default substrate and is meaningless without it", s.p.Name)
	}
	t.Skip("substrate cannot restart")
}

// needRevoke demands the revoke hook, on the same terms as needRestart.
func (s *suite) needRevoke(t *testing.T) {
	t.Helper()
	if s.h.Revoke != nil {
		return
	}
	if s.h.Default && s.p.RequiredForDefault {
		t.Fatalf("default substrate must provide Harness.Revoke: %s is required for the default substrate and is meaningless without it", s.p.Name)
	}
	t.Skip("substrate cannot revoke a credential")
}

func (s *suite) restart(t *testing.T) {
	t.Helper()
	s.h.Restart(t)
}

func (s *suite) revoke(t *testing.T, pluginID string) {
	t.Helper()
	s.h.Revoke(t, pluginID)
}

// ---------------------------------------------------------------- identities

// ident builds the principal a property needs, granted exactly the patterns it
// names and nothing else, so CredentialIsPerPrincipal and
// LoudAttributedRefusal are testing something.
func ident(pluginID, generation string, pats ...bus.Pattern) bus.Identity {
	return bus.Identity{
		PluginID:   pluginID,
		Generation: generation,
		Role:       bus.RolePlugin,
		Grants: bus.Grants{
			Publish:   pats,
			Subscribe: pats,
			Request:   pats,
			Serves:    pats,
		},
	}
}

// withStream adds a provisioned stream to an identity's grants.
func withStream(id bus.Identity, name string) bus.Identity {
	id.Grants.Streams = append(id.Grants.Streams, name)
	return id
}

// withKV adds a provisioned KV bucket to an identity's grants.
func withKV(id bus.Identity, name string) bus.Identity {
	id.Grants.KV = append(id.Grants.KV, name)
	return id
}

// withObjects adds a provisioned object bucket to an identity's grants.
func withObjects(id bus.Identity, name string) bus.Identity {
	id.Grants.Objects = append(id.Grants.Objects, name)
	return id
}

// ------------------------------------------------------------------ faults

// asFault reports the bus.Fault in err, if any.
func asFault(err error) (bus.Fault, bool) {
	var f bus.Fault
	if errors.As(err, &f) {
		return f, true
	}
	return bus.Fault{}, false
}

// mustFault fails unless err is a bus.Fault.
func mustFault(t *testing.T, what string, err error) bus.Fault {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected a bus.Fault, got nil", what)
	}
	f, ok := asFault(err)
	if !ok {
		t.Fatalf("%s: expected a bus.Fault, got %T: %v", what, err, err)
	}
	return f
}

// mustCode fails unless err is a bus.Fault with the given code.
func mustCode(t *testing.T, what string, err error, code string) bus.Fault {
	t.Helper()
	f := mustFault(t, what, err)
	if f.Code != code {
		t.Fatalf("%s: expected fault code %q, got %q (%v)", what, code, f.Code, f)
	}
	return f
}

// mustRefuse returns the Fault the substrate raises for an operation the
// principal is not entitled to perform on subj.
//
// It tries subscribe, then request, then publish, because bus.Bus.Publish
// documents that a core publish refusal may surface late while Subscribe and
// Request answer synchronously. A substrate that refuses none of the three has
// not refused at all, and that is the failure.
func (s *suite) mustRefuse(t *testing.T, b bus.Bus, subj bus.Subject) bus.Fault {
	t.Helper()
	ctx, cancel := s.deadline()
	defer cancel()

	sub, err := b.Subscribe(ctx, bus.Pattern(subj), func(context.Context, *bus.Msg) {})
	if err != nil {
		return mustFault(t, "Subscribe("+string(subj)+")", err)
	}
	// A substrate may accept the call and end the subscription instead.
	select {
	case <-sub.Done():
		if f, ok := asFault(sub.Err()); ok {
			sub.Cancel()
			return f
		}
	case <-time.After(s.settle()):
	}
	sub.Cancel()

	// FaultNoResponders is not a refusal: nothing is listening either way.
	if _, err := b.Request(ctx, subj, []byte("refuse")); err != nil {
		if f, ok := asFault(err); ok && f.Code != bus.FaultNoResponders {
			return f
		}
	}
	if err := b.Publish(ctx, subj, []byte("refuse")); err != nil {
		if f, ok := asFault(err); ok {
			return f
		}
	}
	t.Fatalf("no operation on %q was refused, though the principal holds no grant covering it: subscribe, request and publish all succeeded", subj)
	return bus.Fault{}
}

// ------------------------------------------------------------------- sinks

// sink collects delivered messages. The channel is generous and overflow is
// counted, because a silently dropped duplicate is exactly what
// QueueGroupExclusivity exists to catch.
type sink struct {
	ch      chan *bus.Msg
	dropped atomic.Int64
}

func newSink(n int) *sink { return &sink{ch: make(chan *bus.Msg, n)} }

func (k *sink) handle(_ context.Context, m *bus.Msg) {
	cp := *m
	select {
	case k.ch <- &cp:
	default:
		k.dropped.Add(1)
	}
}

// next returns the next delivered message, or fails.
func (k *sink) next(t *testing.T, what string, d time.Duration) *bus.Msg {
	t.Helper()
	select {
	case m := <-k.ch:
		return m
	case <-time.After(d):
		t.Fatalf("%s: no message delivered within %v", what, d)
		return nil
	}
}

// quiet fails if anything is delivered within d.
func (k *sink) quiet(t *testing.T, what string, d time.Duration) {
	t.Helper()
	select {
	case m := <-k.ch:
		t.Fatalf("%s: expected no delivery, got %q %q", what, m.Subject, m.Data)
	case <-time.After(d):
	}
}

// until collects everything delivered before the barrier subject, which must
// itself be delivered. The barrier replaces a sleep: the messages that should
// not have been delivered were published BEFORE it, so by the time the barrier
// lands they would already be here (bus.Handler is documented to deliver one
// subscription's messages in publish order).
func (k *sink) until(t *testing.T, what string, barrier bus.Subject, d time.Duration) []*bus.Msg {
	t.Helper()
	deadline := time.After(d)
	var got []*bus.Msg
	for {
		select {
		case m := <-k.ch:
			if m.Subject == barrier {
				return got
			}
			got = append(got, m)
		case <-deadline:
			t.Fatalf("%s: barrier %q never arrived within %v (got %s)", what, barrier, d, subjectsOf(got))
			return nil
		}
	}
}

// drained fails if the sink dropped anything.
func (k *sink) drained(t *testing.T, what string) {
	t.Helper()
	if n := k.dropped.Load(); n != 0 {
		t.Fatalf("%s: the test's own sink overflowed by %d messages; the substrate delivered more than the property expects", what, n)
	}
}

func subjectsOf(ms []*bus.Msg) string {
	if len(ms) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, string(m.Subject))
	}
	return strings.Join(parts, ", ")
}

// concrete turns a deny pattern into a concrete subject inside it. Wildcards
// become a literal token, so the result is a subject the deny set matches.
func concrete(p bus.Pattern) bus.Subject {
	toks := p.Tokens()
	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		if tok == "*" || tok == ">" {
			out = append(out, "probe")
			continue
		}
		out = append(out, tok)
	}
	return bus.Subject(strings.Join(out, "."))
}
