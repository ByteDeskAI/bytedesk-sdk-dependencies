package memory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

func testIdentity(id string) bus.Identity {
	return bus.Identity{
		PluginID:   id,
		Generation: "gen-1",
		Role:       bus.RolePlugin,
		Lease:      bus.NewLease("lease-" + id),
		Grants: bus.Grants{
			Publish:   []bus.Pattern{"app.>"},
			Subscribe: []bus.Pattern{"app.>"},
			Request:   []bus.Pattern{"app.>", "svc.app.>"},
			Serves:    []bus.Pattern{"svc.app.>"},
			Streams:   []string{"events"},
			KV:        []string{"cfg"},
			Objects:   []string{"blobs"},
		},
	}
}

func newTestStore(t *testing.T, opts ...Option) *Store {
	t.Helper()
	s := NewStore(opts...)
	t.Cleanup(s.Close)
	return s
}

func faultOf(t *testing.T, err error) bus.Fault {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var f bus.Fault
	if !errors.As(err, &f) {
		t.Fatalf("expected a bus.Fault, got %T: %v", err, err)
	}
	return f
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", d, what)
}

// TestDeniedRefusalNamesSubjectAndPrincipal is conformance property
// LoudAttributedRefusal: a refusal an operator reads in a log must say which
// subject was refused AND who was refused it.
func TestDeniedRefusalNamesSubjectAndPrincipal(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	cases := []struct {
		name    string
		subject string
		run     func() error
	}{
		{"publish", "other.thing", func() error { return b.Publish(ctx, "other.thing", nil) }},
		{"request", "other.ask", func() error { _, err := b.Request(ctx, "other.ask", nil); return err }},
		{"subscribe", "other.>", func() error {
			_, err := b.Subscribe(ctx, "other.>", func(context.Context, *bus.Msg) {})
			return err
		}},
		{"serve", "svc.other.op", func() error {
			_, err := b.Services().Serve(ctx, bus.ServiceSpec{
				Name:      "other",
				Endpoints: []bus.EndpointSpec{{Name: "op", Subject: "svc.other.op", Handler: func(context.Context, *bus.Msg) {}}},
			})
			return err
		}},
		{"stream", "ledger", func() error {
			return b.Streams().Declare(ctx, bus.StreamSpec{Name: "ledger", MaxAge: time.Hour, MaxBytes: 1 << 20})
		}},
		{"kv", "secrets", func() error { return b.KV().Declare(ctx, bus.BucketSpec{Name: "secrets"}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := faultOf(t, tc.run())
			if f.Code != bus.FaultDenied {
				t.Fatalf("code = %q, want %q", f.Code, bus.FaultDenied)
			}
			text := f.Error()
			if !strings.Contains(text, tc.subject) {
				t.Errorf("refusal %q does not name the subject %q", text, tc.subject)
			}
			if !strings.Contains(text, "plug-a@gen-1") {
				t.Errorf("refusal %q does not name the principal", text)
			}
		})
	}
}

// TestPermanentDenyBeatsGrant: deny always wins, even over a grant that covers
// the subject.
func TestPermanentDenyBeatsGrant(t *testing.T) {
	s := newTestStore(t, WithDeny("app.secret.>"))
	id := testIdentity("plug-a")
	b := s.Connect(id)
	ctx := context.Background()

	if !id.Grants.Can(bus.GrantPublish, "app.secret.key") {
		t.Fatal("precondition: the grant must cover the denied subject")
	}
	f := faultOf(t, b.Publish(ctx, "app.secret.key", nil))
	if f.Code != bus.FaultDenied {
		t.Fatalf("code = %q, want denied", f.Code)
	}
	if _, err := b.Subscribe(ctx, "app.secret.*", func(context.Context, *bus.Msg) {}); err == nil {
		t.Fatal("subscribe to a denied family was allowed")
	}
	// A wide subscription must not be a way around the deny list.
	if _, err := b.Subscribe(ctx, "app.>", func(context.Context, *bus.Msg) {}); err == nil {
		t.Fatal("a subscription overlapping a denied family was allowed")
	}
	if err := b.Publish(ctx, "app.public.key", nil); err != nil {
		t.Fatalf("an undenied subject was refused: %v", err)
	}
}

// TestSourceIsUnforgeable: a sender that writes bd-caller is seen as itself.
func TestSourceIsUnforgeable(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	got := make(chan bus.Caller, 1)
	gotHeaders := make(chan bus.Headers, 1)
	sub, err := b.Subscribe(ctx, "app.hello", func(_ context.Context, m *bus.Msg) {
		got <- bus.CallerOf(m)
		gotHeaders <- m.Headers
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()

	err = b.Publish(ctx, "app.hello", []byte("hi"), bus.WithHeaders(bus.Headers{
		"bd-caller":     "someone-else",
		"bd-generation": "forged",
		"bd-subject":    "forged-lease",
		"x-mine":        "kept",
	}))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case c := <-got:
		if c.PluginID != "plug-a" {
			t.Errorf("caller = %q, want plug-a", c.PluginID)
		}
		if c.Generation != "gen-1" {
			t.Errorf("generation = %q, want gen-1", c.Generation)
		}
		if c.SubjectLease != "lease-plug-a" {
			t.Errorf("lease = %q, want lease-plug-a", c.SubjectLease)
		}
		h := <-gotHeaders
		if h.Get("x-mine") != "kept" {
			t.Errorf("a non-reserved header was dropped: %v", h)
		}
		if h.Get(bus.HeaderCorrelation) == "" {
			t.Error("bd-corr was not stamped on egress")
		}
	case <-time.After(time.Second):
		t.Fatal("message never arrived")
	}
}

// TestSlowConsumerOverflowIsLoud: the drop is counted, Err becomes
// FaultSlowConsumer, Done closes, and the publisher never blocks.
func TestSlowConsumerOverflowIsLoud(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	release := make(chan struct{})
	sub, err := b.Subscribe(ctx, "app.slow", func(context.Context, *bus.Msg) { <-release }, bus.PendingLimit(1))
	if err != nil {
		t.Fatal(err)
	}
	defer close(release)
	defer sub.Cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			if err := b.Publish(ctx, "app.slow", []byte("x")); err != nil {
				t.Errorf("publish %d: %v", i, err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the publisher blocked on a slow consumer")
	}

	select {
	case <-sub.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() did not close on overflow")
	}
	f := faultOf(t, sub.Err())
	if f.Code != bus.FaultSlowConsumer {
		t.Fatalf("code = %q, want %q", f.Code, bus.FaultSlowConsumer)
	}
	if d, ok := sub.(interface{ Dropped() uint64 }); !ok || d.Dropped() == 0 {
		t.Error("the drop was not counted")
	}
}

// TestQueueGroupIsExclusive: each message reaches exactly one member.
func TestQueueGroupIsExclusive(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	var a, c atomic.Int64
	for _, counter := range []*atomic.Int64{&a, &c} {
		n := counter
		sub, err := b.Subscribe(ctx, "app.work", func(context.Context, *bus.Msg) { n.Add(1) }, bus.QueueGroup("workers"))
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Cancel()
	}
	// An ungrouped listener gets its own copy of every message.
	var solo atomic.Int64
	solo3, err := b.Subscribe(ctx, "app.work", func(context.Context, *bus.Msg) { solo.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer solo3.Cancel()

	const n = 20
	for i := 0; i < n; i++ {
		if err := b.Publish(ctx, "app.work", []byte("job")); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 2*time.Second, "every job to land", func() bool { return a.Load()+c.Load() == n && solo.Load() == n })
	if total := a.Load() + c.Load(); total != n {
		t.Fatalf("group received %d, want exactly %d", total, n)
	}
	if a.Load() == 0 || c.Load() == 0 {
		t.Errorf("one member starved: a=%d c=%d", a.Load(), c.Load())
	}
}

func TestRequestReply(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	sub, err := b.Subscribe(ctx, "app.echo", func(_ context.Context, m *bus.Msg) {
		_ = m.Respond(nil, append([]byte("re:"), m.Data...))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()

	reply, err := b.Request(ctx, "app.echo", []byte("ping"))
	if err != nil {
		t.Fatal(err)
	}
	if string(reply.Data) != "re:ping" {
		t.Fatalf("reply = %q", reply.Data)
	}
}

// TestNoRespondersIsFast: nothing listening must not burn the timeout.
func TestNoRespondersIsFast(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))

	start := time.Now()
	_, err := b.Request(context.Background(), "app.nobody", nil)
	elapsed := time.Since(start)
	f := faultOf(t, err)
	if f.Code != bus.FaultNoResponders {
		t.Fatalf("code = %q, want %q", f.Code, bus.FaultNoResponders)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("took %s; it must fail fast, not wait out the timeout", elapsed)
	}
}

func TestRequestHonoursTimeoutAndCancel(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	sub, err := b.Subscribe(ctx, "app.mute", func(context.Context, *bus.Msg) {})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()

	if f := faultOf(t, mustErr(b.Request(ctx, "app.mute", nil, bus.WithTimeout(30*time.Millisecond)))); f.Code != bus.FaultTimeout {
		t.Fatalf("code = %q, want timeout", f.Code)
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if f := faultOf(t, mustErr(b.Request(cctx, "app.mute", nil, bus.WithTimeout(5*time.Second)))); f.Code != bus.FaultTimeout {
		t.Fatalf("ctx cancellation: code = %q, want timeout", f.Code)
	}
}

func mustErr(_ *bus.Msg, err error) error { return err }

func TestMaxPayload(t *testing.T) {
	s := newTestStore(t, WithMaxPayload(16))
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	if b.Capabilities().MaxPayload != 16 {
		t.Fatalf("MaxPayload = %d, want 16", b.Capabilities().MaxPayload)
	}
	if err := b.Publish(ctx, "app.size", bytes.Repeat([]byte("x"), 16)); err != nil {
		t.Fatalf("exactly MaxPayload must be allowed: %v", err)
	}
	f := faultOf(t, b.Publish(ctx, "app.size", bytes.Repeat([]byte("x"), 17)))
	if f.Code != bus.FaultBudget {
		t.Fatalf("code = %q, want %q", f.Code, bus.FaultBudget)
	}
}

// ------------------------------------------------------------------ streams

func declareEvents(t *testing.T, b bus.Bus) {
	t.Helper()
	err := b.Streams().Declare(context.Background(), bus.StreamSpec{
		Name:     "events",
		Subjects: []bus.Pattern{"app.ev.>"},
		MaxAge:   time.Hour,
		MaxBytes: 1 << 20,
		Dedupe:   time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamDeclareRefusesUnbounded(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	for _, spec := range []bus.StreamSpec{
		{Name: "events", Subjects: []bus.Pattern{"app.ev.>"}, MaxBytes: 1 << 20},
		{Name: "events", Subjects: []bus.Pattern{"app.ev.>"}, MaxAge: time.Hour},
	} {
		if err := b.Streams().Declare(ctx, spec); err == nil {
			t.Fatalf("an unbounded stream was accepted: %+v", spec)
		}
	}
	declareEvents(t, b)
	// Idempotent.
	declareEvents(t, b)
}

func TestStreamPublishSequenceDedupeAndConflict(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	declareEvents(t, b)
	st := b.Streams()

	for i := 1; i <= 3; i++ {
		seq, err := st.Publish(ctx, "app.ev.one", []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if seq != bus.Seq(i) {
			t.Fatalf("seq = %d, want %d", seq, i)
		}
	}
	first, err := st.Publish(ctx, "app.ev.one", []byte("dup"), bus.WithMsgID("k1"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.Publish(ctx, "app.ev.one", []byte("dup"), bus.WithMsgID("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("dedupe: %d != %d", again, first)
	}
	info, err := st.Info(ctx, "events")
	if err != nil {
		t.Fatal(err)
	}
	if info.Msgs != 4 {
		t.Fatalf("msgs = %d, want 4 (the repeat must not be stored twice)", info.Msgs)
	}
	f := faultOf(t, mustSeqErr(st.Publish(ctx, "app.ev.one", nil, bus.ExpectLastSeq(99))))
	if f.Code != bus.FaultConflict {
		t.Fatalf("code = %q, want conflict", f.Code)
	}
}

func mustSeqErr(_ bus.Seq, err error) error { return err }

func TestStreamPublishBatchIsAllOrNothing(t *testing.T) {
	s := newTestStore(t, WithMaxPayload(8))
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	declareEvents(t, b)
	st := b.Streams()

	_, err := st.PublishBatch(ctx, []bus.BatchMsg{
		{Subject: "app.ev.a", Data: []byte("ok")},
		{Subject: "app.ev.b", Data: bytes.Repeat([]byte("x"), 9)},
	})
	if faultOf(t, err).Code != bus.FaultBudget {
		t.Fatalf("err = %v, want a budget fault", err)
	}
	info, _ := st.Info(ctx, "events")
	if info.Msgs != 0 {
		t.Fatalf("a refused batch stored %d messages", info.Msgs)
	}
	seqs, err := st.PublishBatch(ctx, []bus.BatchMsg{
		{Subject: "app.ev.a", Data: []byte("1")},
		{Subject: "app.ev.b", Data: []byte("2")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Fatalf("seqs = %v", seqs)
	}
}

func TestStreamCounterIsAtomic(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	declareEvents(t, b)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.Streams().Counter(ctx, "app.ev.hits", 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := b.Streams().Counter(ctx, "app.ev.hits", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != 50 {
		t.Fatalf("counter = %d, want 50", got)
	}
}

func TestStreamConsumeFilterFetchAndPurge(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	declareEvents(t, b)
	st := b.Streams()
	for _, subj := range []bus.Subject{"app.ev.keep", "app.ev.skip", "app.ev.keep"} {
		if _, err := st.Publish(ctx, subj, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var seen []bus.Seq
	cons, err := st.Consume(ctx, "events", bus.ConsumerSpec{Filter: "app.ev.keep", Ack: bus.AckExplicit}, func(_ context.Context, m *bus.StreamMsg) {
		mu.Lock()
		seen = append(seen, m.Seq)
		mu.Unlock()
		_ = m.Ack()
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "the filtered messages", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2
	})
	if seen[0] != 1 || seen[1] != 3 {
		t.Fatalf("seen = %v, want [1 3]", seen)
	}
	cons.Cancel()

	got, err := st.Fetch(ctx, "events", bus.ConsumerSpec{Name: "puller", Ack: bus.AckExplicit}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 1 {
		t.Fatalf("fetch = %d messages starting at %v", len(got), got[0].Seq)
	}
	for _, m := range got {
		if err := m.Ack(); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Purge(ctx, "events", "app.ev.skip"); err != nil {
		t.Fatal(err)
	}
	info, _ := st.Info(ctx, "events")
	if info.Msgs != 2 {
		t.Fatalf("after purge msgs = %d, want 2", info.Msgs)
	}
	if err := st.Delete(ctx, "events"); err != nil {
		t.Fatal(err)
	}
	if faultOf(t, mustInfoErr(st.Info(ctx, "events"))).Code != bus.FaultNotFound {
		t.Fatal("a deleted stream is still reported")
	}
}

func mustInfoErr(_ bus.StreamInfo, err error) error { return err }

func TestStreamRedeliversAndHonoursMaxDeliver(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	declareEvents(t, b)
	if _, err := b.Streams().Publish(ctx, "app.ev.retry", []byte("x")); err != nil {
		t.Fatal(err)
	}

	var deliveries atomic.Int64
	cons, err := b.Streams().Consume(ctx, "events", bus.ConsumerSpec{
		Name: "retrier", Ack: bus.AckExplicit, AckWait: 5 * time.Millisecond, MaxDeliver: 3,
	}, func(_ context.Context, m *bus.StreamMsg) { deliveries.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer cons.Cancel()
	waitFor(t, 2*time.Second, "three delivery attempts", func() bool { return deliveries.Load() >= 3 })
	time.Sleep(60 * time.Millisecond)
	if n := deliveries.Load(); n > 3 {
		t.Fatalf("delivered %d times, MaxDeliver was 3", n)
	}
}

// TestCursorReplayAcrossRestart is the whole point of a signed cursor: a
// position handed out before a restart resumes exactly after the last message
// it covered.
func TestCursorReplayAcrossRestart(t *testing.T) {
	s := newTestStore(t)
	id := testIdentity("plug-a")
	b := s.Connect(id)
	ctx := context.Background()
	declareEvents(t, b)
	for i := 1; i <= 5; i++ {
		if _, err := b.Streams().Publish(ctx, "app.ev.n", []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var first []bus.Seq
	cons, err := b.Streams().Consume(ctx, "events", bus.ConsumerSpec{Ack: bus.AckExplicit}, func(_ context.Context, m *bus.StreamMsg) {
		if m.Seq > 3 {
			return // leave 4 and 5 unacknowledged
		}
		_ = m.Ack()
		mu.Lock()
		first = append(first, m.Seq)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "the first three messages", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(first) == 3
	})
	cur := cons.Cursor()
	cons.Cancel()
	if cur.IsZero() {
		t.Fatal("cursor is empty")
	}

	s.Restart()
	if err := b.Publish(ctx, "app.ev.n", nil); err == nil {
		t.Fatal("a bus from before the restart is still usable")
	}

	b2 := s.Connect(id)
	var mu2 sync.Mutex
	var second []bus.Seq
	cons2, err := b2.Streams().Consume(ctx, "events", bus.ConsumerSpec{
		Start: bus.StartCursor, Cursor: cur, Ack: bus.AckExplicit,
	}, func(_ context.Context, m *bus.StreamMsg) {
		_ = m.Ack()
		mu2.Lock()
		second = append(second, m.Seq)
		mu2.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cons2.Cancel()
	waitFor(t, 2*time.Second, "the resumed messages", func() bool {
		mu2.Lock()
		defer mu2.Unlock()
		return len(second) == 2
	})
	if second[0] != 4 || second[1] != 5 {
		t.Fatalf("resumed at %v, want [4 5]", second)
	}
}

// TestFabricatedCursorIsRefused: a cursor is never interpreted, only verified.
func TestFabricatedCursorIsRefused(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	declareEvents(t, b)
	if _, err := b.Streams().Publish(ctx, "app.ev.n", []byte("1")); err != nil {
		t.Fatal(err)
	}

	real := s.mintCursor("events", "c", 1)
	for _, bad := range []bus.Cursor{
		bus.NewCursor("events|c|1"),
		bus.NewCursor(real.Token() + "x"),
		bus.NewCursor(""),
		s.mintCursor("other-stream", "c", 1),
	} {
		_, err := b.Streams().Consume(ctx, "events", bus.ConsumerSpec{Start: bus.StartCursor, Cursor: bad},
			func(context.Context, *bus.StreamMsg) {})
		if err == nil {
			t.Fatalf("cursor %q was accepted", bad.Token())
		}
	}
	// The real one still verifies after a restart: the signing key survives.
	s.Restart()
	b2 := s.Connect(testIdentity("plug-a"))
	cons, err := b2.Streams().Consume(ctx, "events", bus.ConsumerSpec{Start: bus.StartCursor, Cursor: real},
		func(context.Context, *bus.StreamMsg) {})
	if err != nil {
		t.Fatalf("a cursor minted before the restart was refused after it: %v", err)
	}
	cons.Cancel()
}

// ----------------------------------------------------------------------- KV

func TestKVCompareAndSwap(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	if err := b.KV().Declare(ctx, bus.BucketSpec{Name: "cfg", History: 5}); err != nil {
		t.Fatal(err)
	}
	bk, err := b.KV().Open(ctx, "cfg")
	if err != nil {
		t.Fatal(err)
	}

	if faultOf(t, mustEntryErr(bk.Get(ctx, "missing"))).Code != bus.FaultNotFound {
		t.Fatal("a missing key is not FaultNotFound")
	}
	rev, err := bk.Create(ctx, "mode", []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if f := faultOf(t, mustRevErr(bk.Create(ctx, "mode", []byte("b")))); f.Code != bus.FaultConflict {
		t.Fatalf("Create over an existing key: code = %q, want conflict", f.Code)
	}
	if f := faultOf(t, mustRevErr(bk.Update(ctx, "mode", []byte("b"), rev+7))); f.Code != bus.FaultConflict {
		t.Fatalf("stale Update: code = %q, want conflict", f.Code)
	}
	rev2, err := bk.Update(ctx, "mode", []byte("b"), rev)
	if err != nil {
		t.Fatal(err)
	}
	if rev2 <= rev {
		t.Fatalf("revision did not advance: %d -> %d", rev, rev2)
	}
	hist, err := bk.History(ctx, "mode")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || string(hist[0].Value) != "a" {
		t.Fatalf("history = %v", hist)
	}
	if err := bk.Delete(ctx, "mode"); err != nil {
		t.Fatal(err)
	}
	if faultOf(t, mustEntryErr(bk.Get(ctx, "mode"))).Code != bus.FaultNotFound {
		t.Fatal("a deleted key is still readable")
	}
	status, err := bk.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Values != 0 || status.History != 5 {
		t.Fatalf("status = %+v", status)
	}
}

func mustEntryErr(_ bus.Entry, err error) error  { return err }
func mustRevErr(_ bus.Revision, err error) error { return err }

func TestKVWatchDeliversCurrentThenChanges(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	if err := b.KV().Declare(ctx, bus.BucketSpec{Name: "cfg"}); err != nil {
		t.Fatal(err)
	}
	bk, _ := b.KV().Open(ctx, "cfg")
	if _, err := bk.Put(ctx, "a.one", []byte("1")); err != nil {
		t.Fatal(err)
	}

	w, err := bk.Watch(ctx, "a.>")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	select {
	case e := <-w.Updates():
		if e.Key != "a.one" || string(e.Value) != "1" {
			t.Fatalf("first update = %+v, want the current value", e)
		}
	case <-time.After(time.Second):
		t.Fatal("the current value was never delivered")
	}
	if _, err := bk.Put(ctx, "a.two", []byte("2")); err != nil {
		t.Fatal(err)
	}
	if _, err := bk.Put(ctx, "b.three", []byte("3")); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-w.Updates():
		if e.Key != "a.two" {
			t.Fatalf("update = %q, want a.two (b.three does not match the filter)", e.Key)
		}
	case <-time.After(time.Second):
		t.Fatal("the change was never delivered")
	}
}

func TestKVSurvivesRestart(t *testing.T) {
	s := newTestStore(t)
	id := testIdentity("plug-a")
	b := s.Connect(id)
	ctx := context.Background()
	if err := b.KV().Declare(ctx, bus.BucketSpec{Name: "cfg"}); err != nil {
		t.Fatal(err)
	}
	bk, _ := b.KV().Open(ctx, "cfg")
	if _, err := bk.Put(ctx, "kept", []byte("yes")); err != nil {
		t.Fatal(err)
	}

	s.Restart()
	b2 := s.Connect(id)
	bk2, err := b2.KV().Open(ctx, "cfg")
	if err != nil {
		t.Fatalf("the bucket did not survive the restart: %v", err)
	}
	e, err := bk2.Get(ctx, "kept")
	if err != nil || string(e.Value) != "yes" {
		t.Fatalf("value did not survive: %v %v", e, err)
	}
}

// ------------------------------------------------------------------ objects

func TestObjects(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	o := b.Objects()
	if err := o.Declare(ctx, bus.BucketSpec{Name: "blobs"}); err != nil {
		t.Fatal(err)
	}
	meta, err := o.Put(ctx, bus.ObjectMeta{Name: "report.txt", Bucket: "blobs"}, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != 5 || meta.Digest == "" {
		t.Fatalf("meta = %+v", meta)
	}
	rc, got, err := o.Get(ctx, "blobs", "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "hello" || got.Name != "report.txt" {
		t.Fatalf("body = %q meta = %+v", body, got)
	}
	list, err := o.List(ctx, "blobs")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v %v", list, err)
	}
	if err := o.Delete(ctx, "blobs", "report.txt"); err != nil {
		t.Fatal(err)
	}
	if faultOf(t, mustMetaErr(o.Info(ctx, "blobs", "report.txt"))).Code != bus.FaultNotFound {
		t.Fatal("a deleted object is still reported")
	}
	if faultOf(t, o.Declare(ctx, bus.BucketSpec{Name: "not-mine"})).Code != bus.FaultDenied {
		t.Fatal("an ungranted object bucket was declared")
	}
}

func mustMetaErr(_ bus.ObjectMeta, err error) error { return err }

// ----------------------------------------------------------------- services

func TestServicesServeCallDiscoverStats(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	svc, err := b.Services().Serve(ctx, bus.ServiceSpec{
		Name:    "greeter",
		Version: "1.0.0",
		Endpoints: []bus.EndpointSpec{
			{Name: "hello", Subject: "svc.app.hello", Handler: func(_ context.Context, m *bus.Msg) {
				_ = m.Respond(nil, []byte("hi "+string(m.Data)))
			}},
			{Name: "boom", Subject: "svc.app.boom", Handler: func(_ context.Context, m *bus.Msg) {
				_ = m.RespondFault(bus.Fault{Code: bus.FaultBudget, Op: "svc.app.boom", Message: "too much"})
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	reply, err := b.Services().Call(ctx, "svc.app.hello", []byte("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if string(reply.Data) != "hi bob" {
		t.Fatalf("reply = %q", reply.Data)
	}
	f := faultOf(t, mustMsgErr(b.Services().Call(ctx, "svc.app.boom", nil)))
	if f.Code != bus.FaultBudget {
		t.Fatalf("a bd-fault reply became code %q, want %q", f.Code, bus.FaultBudget)
	}

	found, err := b.Services().Discover(ctx, "greeter")
	if err != nil || len(found) != 1 || len(found[0].Endpoints) != 2 {
		t.Fatalf("discover = %+v %v", found, err)
	}
	if found[0].Endpoints[0].QueueGroup != "greeter" {
		t.Errorf("queue group = %q, want the service name", found[0].Endpoints[0].QueueGroup)
	}
	all, err := b.Services().Discover(ctx, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("discover all = %+v %v", all, err)
	}
	stats, err := b.Services().Stats(ctx, "greeter")
	if err != nil || len(stats) != 1 {
		t.Fatalf("stats = %+v %v", stats, err)
	}
	if stats[0].Requests != 2 || stats[0].Errors != 1 {
		t.Fatalf("stats = %+v, want 2 requests and 1 error", stats[0])
	}

	if err := svc.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish the service")
	}
	if faultOf(t, mustMsgErr(b.Services().Call(ctx, "svc.app.hello", nil))).Code != bus.FaultNoResponders {
		t.Fatal("a stopped service still answers")
	}
	if left, _ := b.Services().Discover(ctx, "greeter"); len(left) != 0 {
		t.Fatalf("a stopped service is still discoverable: %+v", left)
	}
}

func mustMsgErr(_ *bus.Msg, err error) error { return err }

// ---------------------------------------------------------------- scheduler

func TestScheduleEveryFiresAndCancelIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	var n atomic.Int64
	sub, err := b.Subscribe(ctx, "app.tick", func(_ context.Context, m *bus.Msg) {
		if bus.CallerOf(m).PluginID != "plug-a" {
			t.Errorf("a scheduled publish was attributed to %q", bus.CallerOf(m).PluginID)
		}
		n.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()

	if err := b.Schedule().Every(ctx, "ticker", 10*time.Millisecond, "app.tick", []byte("t")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "three ticks", func() bool { return n.Load() >= 3 })

	list, err := b.Schedule().List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "ticker" {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := b.Schedule().Cancel(ctx, "ticker"); err != nil {
		t.Fatal(err)
	}
	if err := b.Schedule().Cancel(ctx, "never-existed"); err != nil {
		t.Fatalf("cancelling an unknown name must not be an error: %v", err)
	}
	stopped := n.Load()
	time.Sleep(60 * time.Millisecond)
	if n.Load() != stopped {
		t.Fatal("a cancelled schedule kept firing")
	}
}

func TestScheduleSurvivesRestart(t *testing.T) {
	s := newTestStore(t)
	id := testIdentity("plug-a")
	b := s.Connect(id)
	ctx := context.Background()
	if err := b.Schedule().Every(ctx, "ticker", 10*time.Millisecond, "app.tick", []byte("t")); err != nil {
		t.Fatal(err)
	}

	s.Restart()
	b2 := s.Connect(id)
	var n atomic.Int64
	sub, err := b2.Subscribe(ctx, "app.tick", func(context.Context, *bus.Msg) { n.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()
	waitFor(t, 2*time.Second, "the schedule to resume after a restart", func() bool { return n.Load() >= 2 })

	list, err := b2.Schedule().List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("the schedule did not survive: %+v %v", list, err)
	}
}

func TestCronParser(t *testing.T) {
	base := time.Date(2026, 3, 10, 9, 7, 0, 0, time.UTC)
	cases := []struct {
		expr string
		want time.Time
	}{
		{"*/15 * * * *", time.Date(2026, 3, 10, 9, 15, 0, 0, time.UTC)},
		{"0 * * * *", time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)},
		{"30 9 * * *", time.Date(2026, 3, 10, 9, 30, 0, 0, time.UTC)},
		{"0 0 1 * *", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		{"5,20 9 * * *", time.Date(2026, 3, 10, 9, 20, 0, 0, time.UTC)},
		{"0 10-12 * * *", time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)},
		{"0 9 * * 0", time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		e, err := parseCron(tc.expr)
		if err != nil {
			t.Fatalf("%s: %v", tc.expr, err)
		}
		got, ok := e.next(base)
		if !ok || !got.Equal(tc.want) {
			t.Errorf("%s: next = %v (%v), want %v", tc.expr, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* * * * 9", "a * * * *", "*/0 * * * *", "5-1 * * * *"} {
		if _, err := parseCron(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestScheduleCronRegisters(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	if err := b.Schedule().Cron(ctx, "nightly", "0 3 * * *", "app.tick", nil); err != nil {
		t.Fatal(err)
	}
	list, err := b.Schedule().List(ctx)
	if err != nil || len(list) != 1 || list[0].Next.IsZero() {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := b.Schedule().Cron(ctx, "broken", "not a cron", "app.tick", nil); err == nil {
		t.Fatal("an invalid cron expression was accepted")
	}
}

// -------------------------------------------------------------------- trace

func TestTraceRoundTrip(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	tr := b.Trace()
	ctx := context.Background()

	if tr.Correlation(ctx) == "" {
		t.Fatal("Correlation minted nothing for a bare context")
	}
	if a, c := tr.Correlation(ctx), tr.Correlation(ctx); a == c {
		t.Fatal("two mints produced the same id")
	}
	ctx = tr.WithCorrelation(ctx, "corr-123")
	if got := tr.Correlation(ctx); got != "corr-123" {
		t.Fatalf("correlation = %q", got)
	}
	h := tr.Inject(ctx, bus.Headers{"traceparent": "00-aaaa-bbbb-01"})
	if h.Get(bus.HeaderCorrelation) != "corr-123" {
		t.Fatalf("inject dropped bd-corr: %v", h)
	}
	back := tr.Extract(context.Background(), h)
	if tr.Correlation(back) != "corr-123" {
		t.Fatal("extract lost bd-corr")
	}
	if out := tr.Inject(back, nil); out.Get(bus.HeaderTraceparent) != "00-aaaa-bbbb-01" {
		t.Fatalf("traceparent was not passed through untouched: %v", out)
	}
}

// ------------------------------------------------------------- capabilities

func TestCapabilitiesDefaultsAndRefusals(t *testing.T) {
	s := newTestStore(t)
	caps := s.Connect(testIdentity("plug-a")).Capabilities()
	for name, want := range map[string]bool{
		"durable": true, "kv": true, "objects": true, "services": true,
		"schedule": true, "counters": true, "batch": true, "trace": false,
	} {
		if caps.Has(name) != want {
			t.Errorf("capability %q = %v, want %v", name, caps.Has(name), want)
		}
	}
	if caps.MaxPayload != DefaultMaxPayload {
		t.Errorf("MaxPayload = %d, want %d", caps.MaxPayload, DefaultMaxPayload)
	}

	off := newTestStore(t, WithCapabilities(bus.Capabilities{}))
	b := off.Connect(testIdentity("plug-a"))
	ctx := context.Background()
	checks := map[string]error{
		"durable":  b.Streams().Declare(ctx, bus.StreamSpec{Name: "events", MaxAge: time.Hour, MaxBytes: 1}),
		"kv":       b.KV().Declare(ctx, bus.BucketSpec{Name: "cfg"}),
		"objects":  b.Objects().Declare(ctx, bus.BucketSpec{Name: "blobs"}),
		"schedule": b.Schedule().Cancel(ctx, "x"),
	}
	_, checks["services"] = b.Services().Discover(ctx, "")
	for name, err := range checks {
		f := faultOf(t, err)
		if f.Code != bus.FaultUnsupported {
			t.Errorf("%s: code = %q, want %q", name, f.Code, bus.FaultUnsupported)
		}
	}
	// A disabled capability must not silently drop the payload ceiling either.
	if b.Capabilities().MaxPayload != DefaultMaxPayload {
		t.Errorf("MaxPayload = %d, want the configured default", b.Capabilities().MaxPayload)
	}
}

// -------------------------------------------------------------------- lifecycle

// TestRevokeEndsEveryConnectionForThePlugin: within 1s, Err is FaultWithdrawn
// and Done is closed.
func TestRevokeEndsSubscriptions(t *testing.T) {
	s := newTestStore(t)
	victim := s.Connect(testIdentity("plug-a"))
	bystander := s.Connect(testIdentity("plug-b"))
	ctx := context.Background()

	sub, err := victim.Subscribe(ctx, "app.x", func(context.Context, *bus.Msg) {})
	if err != nil {
		t.Fatal(err)
	}
	other, err := bystander.Subscribe(ctx, "app.x", func(context.Context, *bus.Msg) {})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Cancel()

	start := time.Now()
	s.Revoke("plug-a")
	select {
	case <-sub.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() did not close within 1s of Revoke")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("revoke took %s", elapsed)
	}
	if f := faultOf(t, sub.Err()); f.Code != bus.FaultWithdrawn {
		t.Fatalf("code = %q, want %q", f.Code, bus.FaultWithdrawn)
	}
	if err := victim.Publish(ctx, "app.x", nil); err == nil {
		t.Fatal("a revoked bus still publishes")
	}
	if other.Err() != nil {
		t.Fatalf("a bystander's subscription was ended: %v", other.Err())
	}
	if err := bystander.Publish(ctx, "app.x", nil); err != nil {
		t.Fatalf("a bystander was disturbed: %v", err)
	}
}

func TestCancelAndDrainLeaveErrNil(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	var n atomic.Int64
	sub, err := b.Subscribe(ctx, "app.d", func(context.Context, *bus.Msg) { n.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := b.Publish(ctx, "app.d", nil); err != nil {
			t.Fatal(err)
		}
	}
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sub.Drain(dctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n.Load() != 5 {
		t.Fatalf("drain delivered %d of 5", n.Load())
	}
	if sub.Err() != nil {
		t.Fatalf("a clean drain left Err = %v", sub.Err())
	}

	sub2, err := b.Subscribe(ctx, "app.d", func(context.Context, *bus.Msg) {})
	if err != nil {
		t.Fatal(err)
	}
	sub2.Cancel()
	<-sub2.Done()
	if sub2.Err() != nil {
		t.Fatalf("a clean cancel left Err = %v", sub2.Err())
	}
}

// TestDeliveryIsOrderedAndSerial: one message at a time, in order, per
// subscription.
func TestDeliveryIsOrderedAndSerial(t *testing.T) {
	s := newTestStore(t)
	b := s.Connect(testIdentity("plug-a"))
	ctx := context.Background()

	var mu sync.Mutex
	var order []byte
	var concurrent, maxConcurrent int32
	sub, err := b.Subscribe(ctx, "app.seq", func(_ context.Context, m *bus.Msg) {
		c := atomic.AddInt32(&concurrent, 1)
		if c > atomic.LoadInt32(&maxConcurrent) {
			atomic.StoreInt32(&maxConcurrent, c)
		}
		time.Sleep(time.Millisecond)
		mu.Lock()
		order = append(order, m.Data[0])
		mu.Unlock()
		atomic.AddInt32(&concurrent, -1)
	}, bus.PendingLimit(100))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()

	for i := byte(0); i < 20; i++ {
		if err := b.Publish(ctx, "app.seq", []byte{i}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 3*time.Second, "all 20 messages", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 20
	})
	if maxConcurrent != 1 {
		t.Fatalf("handler ran %d at a time, want 1", maxConcurrent)
	}
	for i := byte(0); i < 20; i++ {
		if order[i] != i {
			t.Fatalf("out of order at %d: %v", i, order)
		}
	}
}

func TestConnectionsAreIndependent(t *testing.T) {
	s := newTestStore(t)
	id := testIdentity("plug-a")
	a := s.Connect(id)
	c := s.Connect(id)
	ctx := context.Background()

	var got atomic.Int64
	sub, err := c.Subscribe(ctx, "app.pair", func(context.Context, *bus.Msg) { got.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(ctx, "app.pair", nil); err != nil {
		t.Fatalf("closing one connection killed the other: %v", err)
	}
	waitFor(t, time.Second, "the message", func() bool { return got.Load() == 1 })
}
