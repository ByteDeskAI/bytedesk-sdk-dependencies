package v1compat

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	v1bus "github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// ---------------------------------------------------------------- the fake bus

// fakeBus records what the adapter asked of the substrate and lets a test make
// any of it refuse. Only the three core calls, Schedule, Trace and Capabilities
// are exercised: the adapter maps a v1 Host, and v1 had no durable messaging,
// so Streams, KV, Objects and Services are unreachable from it and return nil
// rather than a double nothing will use.
type fakeBus struct {
	mu sync.Mutex

	caps bus.Capabilities

	publishErr   error
	subscribeErr error
	requestErr   error
	reply        *bus.Msg

	published  []published
	patterns   []bus.Pattern
	requested  []published
	subs       []*fakeSub
	correlated []string

	sched *fakeScheduler
}

type published struct {
	subject bus.Subject
	data    []byte
	headers bus.Headers
}

func newFakeBus() *fakeBus {
	return &fakeBus{sched: &fakeScheduler{}}
}

func (f *fakeBus) Publish(_ context.Context, s bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, published{s, data, bus.ResolvePublish(opts).Headers})
	return f.publishErr
}

func (f *fakeBus) Subscribe(_ context.Context, p bus.Pattern, h bus.Handler, _ ...bus.SubOpt) (bus.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patterns = append(f.patterns, p)
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	s := &fakeSub{pattern: p, handler: h, done: make(chan struct{})}
	f.subs = append(f.subs, s)
	return s, nil
}

func (f *fakeBus) Request(_ context.Context, s bus.Subject, data []byte, opts ...bus.ReqOpt) (*bus.Msg, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested = append(f.requested, published{s, data, bus.ResolveReq(opts).Headers})
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	return f.reply, nil
}

func (f *fakeBus) Streams() bus.Streams           { return nil }
func (f *fakeBus) KV() bus.KV                     { return nil }
func (f *fakeBus) Objects() bus.Objects           { return nil }
func (f *fakeBus) Services() bus.Services         { return nil }
func (f *fakeBus) Schedule() bus.Scheduler        { return f.sched }
func (f *fakeBus) Trace() bus.Trace               { return fakeTrace{f} }
func (f *fakeBus) Capabilities() bus.Capabilities { return f.caps }
func (f *fakeBus) Close() error                   { return nil }

var _ bus.Bus = (*fakeBus)(nil)
var _ io.Closer = (*fakeBus)(nil)

// deliver feeds a message to every live subscription whose pattern matches.
func (f *fakeBus) deliver(m *bus.Msg) {
	f.mu.Lock()
	subs := make([]*fakeSub, len(f.subs))
	copy(subs, f.subs)
	f.mu.Unlock()
	for _, s := range subs {
		if !s.cancelled() && s.pattern.Matches(m.Subject) {
			s.handler(context.Background(), m)
		}
	}
}

func (f *fakeBus) snapshot() ([]published, []bus.Pattern, []published) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.published, f.patterns, f.requested
}

func (f *fakeBus) liveSubs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.subs {
		if !s.cancelled() {
			n++
		}
	}
	return n
}

type fakeSub struct {
	pattern bus.Pattern
	handler bus.Handler

	mu     sync.Mutex
	cancel int
	done   chan struct{}
}

func (s *fakeSub) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel++
	if s.cancel == 1 {
		close(s.done)
	}
}

func (s *fakeSub) cancels() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel
}

func (s *fakeSub) cancelled() bool             { return s.cancels() > 0 }
func (s *fakeSub) Drain(context.Context) error { return nil }
func (s *fakeSub) Done() <-chan struct{}       { return s.done }
func (s *fakeSub) Err() error                  { return nil }

// fakeTrace is the minimum the adapter uses: it remembers the correlation ids
// handed to it so a test can see that env.CorrelationID took the trace route
// rather than being dropped by the bd-* strip.
type fakeTrace struct{ f *fakeBus }

func (t fakeTrace) Correlation(context.Context) string { return "" }

func (t fakeTrace) WithCorrelation(ctx context.Context, id string) context.Context {
	t.f.mu.Lock()
	t.f.correlated = append(t.f.correlated, id)
	t.f.mu.Unlock()
	return ctx
}

func (t fakeTrace) Inject(_ context.Context, h bus.Headers) bus.Headers        { return h }
func (t fakeTrace) Extract(ctx context.Context, _ bus.Headers) context.Context { return ctx }

type fakeScheduler struct {
	mu      sync.Mutex
	err     error
	every   []string
	subject []bus.Subject
	cancels []string
}

func (s *fakeScheduler) At(context.Context, string, time.Time, bus.Subject, []byte, ...bus.PublishOpt) error {
	return bus.Unsupported("schedule.At", "schedule")
}

func (s *fakeScheduler) Every(_ context.Context, name string, _ time.Duration, subject bus.Subject, _ []byte, _ ...bus.PublishOpt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.every = append(s.every, name)
	s.subject = append(s.subject, subject)
	return nil
}

func (s *fakeScheduler) Cron(context.Context, string, string, bus.Subject, []byte, ...bus.PublishOpt) error {
	return bus.Unsupported("schedule.Cron", "schedule")
}

func (s *fakeScheduler) Cancel(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels = append(s.cancels, name)
	return nil
}

func (s *fakeScheduler) List(context.Context) ([]bus.Schedule, error) { return nil, nil }

func (s *fakeScheduler) state() ([]string, []bus.Subject, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.every, s.subject, s.cancels
}

// ---------------------------------------------------------------- the fake host

type recordLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *recordLogger) Info(msg string, args ...any)  { l.add("info", msg) }
func (l *recordLogger) Warn(msg string, args ...any)  { l.add("warn", msg) }
func (l *recordLogger) Error(msg string, args ...any) { l.add("error", msg) }

func (l *recordLogger) add(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, level+": "+msg)
}

func (l *recordLogger) has(level, substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.logs {
		if strings.HasPrefix(line, level+": ") && strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func (l *recordLogger) dump() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.logs...)
}

type fixture struct{ plugin.Base }

// bind builds a bound Base over b and returns the v1 Host and the logger.
func bind(t *testing.T, b *fakeBus) (*hostAdapter, *recordLogger) {
	t.Helper()
	log := &recordLogger{}
	f := &fixture{}
	err := plugin.Bind(f, plugin.Binding{
		Bus:      b,
		Logger:   log,
		StateDir: "/var/lib/bytedesk/files",
		Identity: bus.Identity{PluginID: "files", Generation: "g7", Role: bus.RolePlugin},
		// Bind wraps the bus so Capabilities() reports the NEGOTIATED set, not
		// the substrate's, so a test steers Every from here.
		Caps: b.caps,
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	h := Host(&f.Base).(*hostAdapter)
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h, log
}

// -------------------------------------------------------------------- the tests

func TestPublishCarriesTypeVerbatimAndDropsSource(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)

	err := h.Publish(v1bus.Envelope{
		ID:            "env-1",
		Type:          "event.files.changed",
		Source:        "impersonated-peer",
		CorrelationID: "corr-9",
		Timestamp:     time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
		Headers:       map[string]string{"tenant": "acme", "bd-caller": "forged"},
		Payload:       []byte(`{"path":"/a"}`),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	pubs, _, _ := b.snapshot()
	if len(pubs) != 1 {
		t.Fatalf("want 1 publish, got %d", len(pubs))
	}
	if pubs[0].subject != "event.files.changed" {
		t.Errorf("subject = %q, want the envelope Type verbatim", pubs[0].subject)
	}
	if string(pubs[0].data) != `{"path":"/a"}` {
		t.Errorf("data = %q, want the envelope payload", pubs[0].data)
	}
	// Source is not carried at all: no header repeats it, under any spelling.
	for k, v := range pubs[0].headers {
		if strings.Contains(v, "impersonated-peer") {
			t.Errorf("header %q carries the caller-supplied Source %q", k, v)
		}
		if strings.HasPrefix(k, bus.HeaderPrefix) {
			t.Errorf("header %q survived the bd-* strip", k)
		}
	}
	if got := pubs[0].headers.Get("tenant"); got != "acme" {
		t.Errorf("tenant header = %q, want acme", got)
	}
	if got := pubs[0].headers.Get(HeaderEnvelopeID); got != "env-1" {
		t.Errorf("%s = %q, want env-1", HeaderEnvelopeID, got)
	}
	if got := pubs[0].headers.Get(HeaderEnvelopeTimestamp); got != "2026-09-17T08:00:00Z" {
		t.Errorf("%s = %q", HeaderEnvelopeTimestamp, got)
	}
	b.mu.Lock()
	corr := append([]string(nil), b.correlated...)
	b.mu.Unlock()
	if len(corr) != 1 || corr[0] != "corr-9" {
		t.Errorf("correlation ids offered to Trace = %v, want [corr-9]", corr)
	}
}

func TestPublishRefusalIsReturned(t *testing.T) {
	b := newFakeBus()
	b.publishErr = bus.Denied("event.files.changed", "files@g7", "not in effective grants")
	h, _ := bind(t, b)

	err := h.Publish(v1bus.Envelope{Type: "event.files.changed"})
	var fault bus.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("Publish error = %v, want a bus.Fault", err)
	}
	if fault.Code != bus.FaultDenied {
		t.Errorf("fault code = %q, want %q", fault.Code, bus.FaultDenied)
	}
}

func TestPublishRefusesAnUnparseableType(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)

	for _, bad := range []string{"", "event..changed", "event.files.*"} {
		err := h.Publish(v1bus.Envelope{Type: bad})
		var fault bus.Fault
		if !errors.As(err, &fault) || fault.Code != bus.FaultSchema {
			t.Errorf("Publish(%q) error = %v, want a FaultSchema", bad, err)
		}
		if !strings.Contains(err.Error(), bad) && bad != "" {
			t.Errorf("Publish(%q) error %q does not name the value", bad, err)
		}
	}
	if pubs, _, _ := b.snapshot(); len(pubs) != 0 {
		t.Errorf("an unparseable type reached the bus: %v", pubs)
	}
}

func TestSubscribeRebuildsSourceFromTheStampedCaller(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)

	got := make(chan v1bus.Envelope, 1)
	unsub := h.Subscribe("event.peer.changed", func(env v1bus.Envelope) { got <- env })
	defer unsub()

	b.deliver(bus.NewMsg("event.peer.changed", "", bus.Headers{
		bus.HeaderCaller:        "peer",
		bus.HeaderGeneration:    "g3",
		bus.HeaderCorrelation:   "corr-1",
		HeaderEnvelopeID:        "env-7",
		HeaderEnvelopeTimestamp: "2026-09-17T08:00:00Z",
		"source":                "a-lie",
		"tenant":                "acme",
	}, []byte(`{"n":1}`), nil))

	env := <-got
	if env.Source != "peer" {
		t.Errorf("Source = %q, want the bd-caller value", env.Source)
	}
	if env.Type != "event.peer.changed" {
		t.Errorf("Type = %q", env.Type)
	}
	if env.ID != "env-7" || env.CorrelationID != "corr-1" {
		t.Errorf("ID = %q, CorrelationID = %q", env.ID, env.CorrelationID)
	}
	if !env.Timestamp.Equal(time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("Timestamp = %v", env.Timestamp)
	}
	if string(env.Payload) != `{"n":1}` {
		t.Errorf("Payload = %q", env.Payload)
	}
	// The reserved headers and this package's own metadata do not leak into the
	// v1 Headers map; an ordinary header does.
	if env.Headers["tenant"] != "acme" {
		t.Errorf("Headers = %v, want tenant=acme", env.Headers)
	}
	for _, k := range []string{bus.HeaderCaller, bus.HeaderCorrelation, HeaderEnvelopeID, HeaderEnvelopeTimestamp} {
		if _, ok := env.Headers[k]; ok {
			t.Errorf("Headers still carries %q", k)
		}
	}
}

func TestSubscribeWithoutMetadataHeadersStillYieldsAnID(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)

	got := make(chan v1bus.Envelope, 1)
	defer h.Subscribe("event.native.thing", func(env v1bus.Envelope) { got <- env })()

	before := time.Now().Add(-time.Second)
	b.deliver(bus.NewMsg("event.native.thing", "", nil, nil, nil))

	env := <-got
	if env.ID == "" {
		t.Error("a v2-native message yielded an empty envelope ID")
	}
	if env.Timestamp.Before(before) {
		t.Errorf("Timestamp = %v, want the receive time", env.Timestamp)
	}
}

func TestFailedSubscribeLogsAndReturnsANoopUnsubscribe(t *testing.T) {
	b := newFakeBus()
	b.subscribeErr = bus.Denied("event.other.thing", "files@g7", "pattern not covered")
	h, log := bind(t, b)

	unsub := h.Subscribe("event.other.thing", func(v1bus.Envelope) {
		t.Error("handler ran on a refused subscription")
	})
	if unsub == nil {
		t.Fatal("Subscribe returned a nil unsubscribe")
	}
	unsub()
	unsub() // safe twice

	if !log.has("error", "subscribe refused") {
		t.Errorf("no Error log for the refusal; logs = %v", log.dump())
	}
}

func TestSubscribeTranslatesStarToTailWildcard(t *testing.T) {
	b := newFakeBus()
	h, log := bind(t, b)

	defer h.Subscribe("*", func(v1bus.Envelope) {})()

	_, patterns, _ := b.snapshot()
	if len(patterns) != 1 || patterns[0] != ">" {
		t.Fatalf("patterns = %v, want [>]", patterns)
	}
	if !log.has("warn", "translated") {
		t.Errorf("the translation was not logged at Warn; logs = %v", log.dump())
	}
}

func TestSubscribeUnsubscribeIsIdempotent(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)

	unsub := h.Subscribe("event.files.changed", func(v1bus.Envelope) {})
	unsub()
	unsub()
	unsub()

	b.mu.Lock()
	sub := b.subs[0]
	b.mu.Unlock()
	if n := sub.cancels(); n != 1 {
		t.Errorf("underlying Cancel called %d times, want 1", n)
	}
}

func TestRequestTurnsAFaultReplyIntoAnError(t *testing.T) {
	b := newFakeBus()
	b.reply = bus.NewMsg("cmd.files.v1.delete", "", bus.Headers{
		bus.HeaderFault: bus.FaultBudget,
	}, []byte("monthly budget exhausted"), nil)
	h, _ := bind(t, b)

	env, err := h.Request(context.Background(), v1bus.Envelope{Type: "cmd.files.v1.delete"})
	var fault bus.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("Request error = %v, want a bus.Fault", err)
	}
	if fault.Code != bus.FaultBudget {
		t.Errorf("fault code = %q, want %q", fault.Code, bus.FaultBudget)
	}
	if fault.Op != "cmd.files.v1.delete" {
		t.Errorf("fault Op = %q, want the subject", fault.Op)
	}
	if env.Type != "" {
		t.Errorf("a fault reply also produced an Envelope: %+v", env)
	}
}

func TestRequestMapsASuccessfulReply(t *testing.T) {
	b := newFakeBus()
	b.reply = bus.NewMsg("cmd.files.v1.delete", "", bus.Headers{
		bus.HeaderCaller: "host",
	}, []byte(`{"deleted":1}`), nil)
	h, _ := bind(t, b)

	env, err := h.Request(context.Background(), v1bus.Envelope{
		Type:    "cmd.files.v1.delete",
		Payload: []byte(`{"path":"/a"}`),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if env.Source != "host" || string(env.Payload) != `{"deleted":1}` {
		t.Errorf("reply envelope = %+v", env)
	}
	if _, _, reqs := b.snapshot(); len(reqs) != 1 || reqs[0].subject != "cmd.files.v1.delete" {
		t.Errorf("requests = %v", reqs)
	}
}

func TestStateDirRefusesAnotherPlugin(t *testing.T) {
	b := newFakeBus()
	h, log := bind(t, b)

	if got := h.StateDir(""); got != "/var/lib/bytedesk/files" {
		t.Errorf(`StateDir("") = %q, want the bound dir`, got)
	}
	if got := h.StateDir("files"); got != "/var/lib/bytedesk/files" {
		t.Errorf(`StateDir("files") = %q, want the bound dir`, got)
	}
	if got := h.StateDir("someone-else"); got != "" {
		t.Errorf(`StateDir("someone-else") = %q, want ""`, got)
	}
	if !log.has("error", "refused state dir") {
		t.Errorf("the refusal was not logged at Error; logs = %v", log.dump())
	}
}

func TestEveryFallsBackToANonOverlappingTicker(t *testing.T) {
	b := newFakeBus() // caps.Schedule is false
	h, _ := bind(t, b)

	var mu sync.Mutex
	var concurrent, max, runs int
	release := make(chan struct{})
	done := make(chan struct{}, 1)

	cancel := h.Every(time.Millisecond, func() {
		mu.Lock()
		concurrent++
		runs++
		if concurrent > max {
			max = concurrent
		}
		n := runs
		mu.Unlock()

		if n == 1 {
			// Hold the first tick well past several intervals. A ticker that
			// ran callbacks concurrently would start the next one here.
			<-release
		}
		mu.Lock()
		concurrent--
		if runs >= 2 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
		mu.Unlock()
	})
	defer cancel()

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	overlapped := max
	mu.Unlock()
	if overlapped != 1 {
		t.Fatalf("max concurrent callbacks = %d, want 1 (ticks must not overlap)", overlapped)
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the ticker stopped after the slow callback")
	}

	if evs, _, _ := b.sched.state(); len(evs) != 0 {
		t.Errorf("the scheduler was used although Capabilities().Schedule is false: %v", evs)
	}
}

func TestEveryUsesTheSchedulerWhenAvailable(t *testing.T) {
	b := newFakeBus()
	b.caps = bus.Capabilities{Schedule: true}
	h, _ := bind(t, b)

	ran := make(chan struct{}, 1)
	cancel := h.Every(time.Hour, func() { ran <- struct{}{} })

	names, subjects, _ := b.sched.state()
	if len(names) != 1 || names[0] != "tick.files.0" {
		t.Fatalf("schedule names = %v, want [tick.files.0]", names)
	}
	if subjects[0] != "tick.files.0" {
		t.Errorf("schedule subject = %q", subjects[0])
	}
	// The subscription is what actually runs fn when the schedule fires.
	b.deliver(bus.NewMsg("tick.files.0", "", nil, nil, nil))
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a scheduled tick did not reach the callback")
	}

	// A second Every takes the next counter value.
	h.Every(time.Hour, func() {})
	if names, _, _ = b.sched.state(); len(names) != 2 || names[1] != "tick.files.1" {
		t.Fatalf("schedule names = %v, want the counter to advance", names)
	}

	cancel()
	cancel()
	if _, _, cancels := b.sched.state(); len(cancels) != 1 || cancels[0] != "tick.files.0" {
		t.Errorf("schedule cancels = %v, want exactly one for tick.files.0", cancels)
	}
}

func TestEveryFallsBackWhenTheSchedulerRefuses(t *testing.T) {
	b := newFakeBus()
	b.caps = bus.Capabilities{Schedule: true}
	b.sched.err = bus.Denied("tick.files.0", "files@g7", "schedule not granted")
	h, log := bind(t, b)

	ran := make(chan struct{}, 1)
	defer h.Every(time.Millisecond, func() {
		select {
		case ran <- struct{}{}:
		default:
		}
	})()

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a refused schedule left the plugin with no periodic work at all")
	}
	if !log.has("error", "schedule refused") {
		t.Errorf("the fallback was not logged at Error; logs = %v", log.dump())
	}
	if b.liveSubs() != 0 {
		t.Error("the tick subscription outlived the refused schedule")
	}
}

func TestBumpContributionsPublishesAndLogsARefusal(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)
	h.BumpContributions()

	pubs, _, _ := b.snapshot()
	if len(pubs) != 1 || pubs[0].subject != "cmd.ui.v1.contributions.bump" {
		t.Fatalf("publishes = %v", pubs)
	}
	if len(pubs[0].data) != 0 {
		t.Errorf("payload = %q, want empty", pubs[0].data)
	}

	b2 := newFakeBus()
	b2.publishErr = bus.Denied("cmd.ui.v1.contributions.bump", "files@g7", "no grant")
	h2, log := bind(t, b2)
	h2.BumpContributions()
	if !log.has("error", "contributions bump refused") {
		t.Errorf("the refusal was not logged; logs = %v", log.dump())
	}
}

func TestCloseReleasesEverythingAndIsIdempotent(t *testing.T) {
	b := newFakeBus()
	b.caps = bus.Capabilities{Schedule: true}
	h, _ := bind(t, b)

	h.Subscribe("event.files.changed", func(v1bus.Envelope) {})
	h.Subscribe("event.files.deleted", func(v1bus.Envelope) {})
	h.Every(time.Hour, func() {})

	if b.liveSubs() != 3 {
		t.Fatalf("live subscriptions before Close = %d, want 3", b.liveSubs())
	}

	ctx := context.Background()
	if err := h.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if b.liveSubs() != 0 {
		t.Errorf("live subscriptions after Close = %d, want 0", b.liveSubs())
	}
	if _, _, cancels := b.sched.state(); len(cancels) != 1 {
		t.Errorf("schedule cancels = %v, want exactly one", cancels)
	}
	b.mu.Lock()
	for _, s := range b.subs {
		if n := s.cancels(); n != 1 {
			t.Errorf("subscription %q cancelled %d times, want 1", s.pattern, n)
		}
	}
	b.mu.Unlock()
}

func TestCancelAfterCloseIsSafe(t *testing.T) {
	b := newFakeBus()
	h, _ := bind(t, b)
	unsub := h.Subscribe("event.files.changed", func(v1bus.Envelope) {})

	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	unsub()
	unsub()

	b.mu.Lock()
	defer b.mu.Unlock()
	if n := b.subs[0].cancels(); n != 1 {
		t.Errorf("Cancel called %d times across Close and unsubscribe, want 1", n)
	}
}

func TestUnboundBaseRefusesEverythingWithoutPanicking(t *testing.T) {
	var base plugin.Base
	h := Host(&base)
	defer func() { _ = h.(Closer).Close(context.Background()) }()

	if err := h.Publish(v1bus.Envelope{Type: "event.files.changed"}); err == nil {
		t.Error("Publish on an unbound base returned nil")
	}
	if _, err := h.Request(context.Background(), v1bus.Envelope{Type: "cmd.files.v1.delete"}); err == nil {
		t.Error("Request on an unbound base returned nil")
	}
	unsub := h.Subscribe("event.files.changed", func(v1bus.Envelope) {})
	if unsub == nil {
		t.Fatal("Subscribe on an unbound base returned a nil unsubscribe")
	}
	unsub()
	if got := h.StateDir(""); got != "" {
		t.Errorf("StateDir = %q, want empty", got)
	}
	if h.Logger() == nil || h.Profiling() == nil {
		t.Error("Logger or Profiling returned nil on an unbound base")
	}
	if h.Profiling().Enabled() {
		t.Error("an unbound profiler reports enabled")
	}
	h.BumpContributions()
	// Every must still tick: Capabilities().Schedule is false, so this is the
	// in-process fallback over a bus that refuses everything else.
	ran := make(chan struct{}, 1)
	cancel := h.Every(time.Millisecond, func() {
		select {
		case ran <- struct{}{}:
		default:
		}
	})
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("Every did not tick on an unbound base")
	}
	cancel()
	cancel()
}

func TestHostImplementsCloser(t *testing.T) {
	if _, ok := Host(nil).(Closer); !ok {
		t.Fatal("the returned Host does not implement Closer")
	}
}
