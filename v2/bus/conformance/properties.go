package conformance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// properties is the list. Order is the run order, and the names are the
// subtest names a runner filters on, so neither may be changed casually.
var properties = []struct {
	Property
	run func(*testing.T, *suite)
}{
	{Property{Name: "PublishSubscribeExact"}, publishSubscribeExact},
	{Property{Name: "WildcardOneToken"}, wildcardOneToken},
	{Property{Name: "WildcardTail"}, wildcardTail},
	{Property{Name: "PerSubscriberFIFO"}, perSubscriberFIFO},
	{Property{Name: "QueueGroupExclusivity", RequiredForDefault: true}, queueGroupExclusivity},
	{Property{Name: "RequestReplyCorrelated"}, requestReplyCorrelated},
	{Property{Name: "NoRespondersFastFail", RequiredForDefault: true}, noRespondersFastFail},
	{Property{Name: "RequestHonoursContext"}, requestHonoursContext},
	{Property{Name: "LoudAttributedRefusal", RequiredForDefault: true}, loudAttributedRefusal},
	{Property{Name: "PermanentDenyWins", RequiredForDefault: true}, permanentDenyWins},
	{Property{Name: "SourceUnforgeable", RequiredForDefault: true}, sourceUnforgeable},
	{Property{Name: "MaxPayloadRefusal"}, maxPayloadRefusal},
	{Property{Name: "BoundedOverflowCountedPublisherNeverBlocks"}, boundedOverflowCounted},
	{Property{Name: "RevokeDisconnectsWithinBound", RequiredForDefault: true}, revokeDisconnectsWithinBound},
	{Property{Name: "GenerationRevokeDisposal"}, generationRevokeDisposal},
	{Property{Name: "CredentialIsPerPrincipal"}, credentialIsPerPrincipal},
	{Property{Name: "StreamPublishConsumeAck", Requires: []string{"durable"}}, streamPublishConsumeAck},
	{Property{Name: "ReplayFromCursorAfterRestart", Requires: []string{"durable"}, RequiredForDefault: true}, replayFromCursorAfterRestart},
	{Property{Name: "MsgIDDedupe", Requires: []string{"durable"}}, msgIDDedupe},
	{Property{Name: "PutGetWatch", Requires: []string{"kv"}}, putGetWatch},
	{Property{Name: "KVSurvivesRestart", Requires: []string{"kv", "durable"}}, kvSurvivesRestart},
	{Property{Name: "ScheduledPublishFires", Requires: []string{"schedule"}}, scheduledPublishFires},
	{Property{Name: "ScheduledPublishSurvivesRestart", Requires: []string{"schedule", "durable"}}, scheduledPublishSurvivesRestart},
	{Property{Name: "RegisterDiscoverCall", Requires: []string{"services"}}, registerDiscoverCall},
	{Property{Name: "ObjectPutGetList", Requires: []string{"objects"}}, objectPutGetList},
	{Property{Name: "CounterAccumulatesAtomically", Requires: []string{"counters", "durable"}}, counterAccumulatesAtomically},
	{Property{Name: "BatchPublishIsAllOrNothing", Requires: []string{"batch", "durable"}}, batchPublishIsAllOrNothing},
}

// fastFailBound and revokeBound are the two wall-clock ceilings the contract
// states in seconds rather than in budgets. They do not scale: "fast" that
// scales with a slow machine is not a promise a caller can build on.
const (
	fastFailBound = time.Second
	revokeBound   = time.Second
)

// 1. PublishSubscribeExact: a concrete subscription receives a concrete
// publish, with its payload intact.
func publishSubscribeExact(t *testing.T, s *suite) {
	b := s.bus(t, ident("p1", "g1", "p1.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(4)
	sub, err := b.Subscribe(ctx, "p1.exact", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	want := []byte("exact-payload")
	if err := b.Publish(ctx, "p1.exact", want); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	m := k.next(t, "p1.exact", s.budget())
	if m.Subject != "p1.exact" {
		t.Fatalf("delivered subject: want %q, got %q", "p1.exact", m.Subject)
	}
	if !bytes.Equal(m.Data, want) {
		t.Fatalf("delivered payload: want %q, got %q", want, m.Data)
	}
}

// 2. WildcardOneToken: "a.*.c" receives "a.b.c", not "a.b.x.c" and not "a.c".
// "*" spans exactly one token.
func wildcardOneToken(t *testing.T, s *suite) {
	b := s.bus(t, ident("p2", "g1", "p2.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(8)
	sub, err := b.Subscribe(ctx, "p2.*.c", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	// The two that must not match go first; the match is the barrier.
	for _, subj := range []bus.Subject{"p2.b.x.c", "p2.c", "p2.b.c"} {
		if err := b.Publish(ctx, subj, []byte(subj)); err != nil {
			t.Fatalf("Publish(%q): %v", subj, err)
		}
	}
	if got := k.until(t, "p2.*.c", "p2.b.c", s.budget()); len(got) != 0 {
		t.Fatalf(`"p2.*.c" delivered %s; "*" matched something other than exactly one token`, subjectsOf(got))
	}
	k.quiet(t, "p2.*.c after the match", s.settle())
	k.drained(t, "p2.*.c")
}

// 3. WildcardTail: "a.>" receives "a.b" and "a.b.c", not "a". ">" is one or
// more trailing tokens, never zero.
func wildcardTail(t *testing.T, s *suite) {
	b := s.bus(t, ident("p3", "g1", "p3", "p3.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(8)
	sub, err := b.Subscribe(ctx, "p3.>", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	for _, subj := range []bus.Subject{"p3", "p3.b", "p3.b.c"} {
		if err := b.Publish(ctx, subj, []byte(subj)); err != nil {
			t.Fatalf("Publish(%q): %v", subj, err)
		}
	}
	got := k.until(t, "p3.>", "p3.b.c", s.budget())
	if len(got) != 1 || got[0].Subject != "p3.b" {
		t.Fatalf(`"p3.>" delivered %s before "p3.b.c"; want exactly "p3.b" (and never the bare root "p3")`, subjectsOf(got))
	}
	k.quiet(t, "p3.> after the tail", s.settle())
	k.drained(t, "p3.>")
}

// 4. PerSubscriberFIFO: one subscriber sees its messages in publish order.
func perSubscriberFIFO(t *testing.T, s *suite) {
	const n = 64
	b := s.bus(t, ident("p4", "g1", "p4.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(n * 2)
	sub, err := b.Subscribe(ctx, "p4.seq", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	for i := 0; i < n; i++ {
		if err := b.Publish(ctx, "p4.seq", []byte(strconv.Itoa(i))); err != nil {
			t.Fatalf("Publish(%d): %v", i, err)
		}
	}
	for i := 0; i < n; i++ {
		m := k.next(t, fmt.Sprintf("p4.seq message %d", i), s.budget())
		if got := string(m.Data); got != strconv.Itoa(i) {
			t.Fatalf("out of order at position %d: want %q, got %q", i, strconv.Itoa(i), got)
		}
	}
	k.drained(t, "p4.seq")
}

// 5. QueueGroupExclusivity: N members of one group, M messages, each message
// delivered exactly once across the group.
func queueGroupExclusivity(t *testing.T, s *suite) {
	const members, msgs = 4, 40
	b := s.bus(t, ident("p5", "g1", "p5.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(msgs * 4)
	for i := 0; i < members; i++ {
		sub, err := b.Subscribe(ctx, "p5.work", k.handle, bus.QueueGroup("p5-group"))
		if err != nil {
			t.Fatalf("Subscribe(member %d): %v", i, err)
		}
		defer sub.Cancel()
	}

	for i := 0; i < msgs; i++ {
		if err := b.Publish(ctx, "p5.work", []byte(strconv.Itoa(i))); err != nil {
			t.Fatalf("Publish(%d): %v", i, err)
		}
	}
	seen := make(map[string]int, msgs)
	for i := 0; i < msgs; i++ {
		m := k.next(t, fmt.Sprintf("p5.work delivery %d of %d", i+1, msgs), s.budget())
		seen[string(m.Data)]++
	}
	for i := 0; i < msgs; i++ {
		switch c := seen[strconv.Itoa(i)]; {
		case c == 0:
			t.Fatalf("message %d was never delivered to any group member", i)
		case c > 1:
			t.Fatalf("message %d was delivered %d times across the group; a queue group delivers each message exactly once", i, c)
		}
	}
	// A duplicate would be a 41st delivery.
	k.quiet(t, "p5.work after all 40", s.settle())
	k.drained(t, "p5.work")
}

// 6. RequestReplyCorrelated: concurrent requests each get their own reply.
func requestReplyCorrelated(t *testing.T, s *suite) {
	const n = 8
	b := s.bus(t, ident("p6", "g1", "p6.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	sub, err := b.Subscribe(ctx, "p6.echo", func(_ context.Context, m *bus.Msg) {
		_ = m.Respond(nil, append([]byte("re:"), m.Data...))
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	type result struct {
		sent string
		got  string
		err  error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sent := "req-" + strconv.Itoa(i)
			reply, err := b.Request(ctx, "p6.echo", []byte(sent))
			r := result{sent: sent, err: err}
			if err == nil {
				if reply == nil {
					r.err = fmt.Errorf("nil reply with nil error")
				} else {
					r.got = string(reply.Data)
				}
			}
			results <- r
		}(i)
	}
	wg.Wait()
	close(results)

	seen := make(map[string]bool, n)
	for r := range results {
		if r.err != nil {
			t.Fatalf("Request(%s): %v", r.sent, r.err)
		}
		if want := "re:" + r.sent; r.got != want {
			t.Fatalf("reply crossed wires: request %q got reply %q, want %q", r.sent, r.got, want)
		}
		if seen[r.got] {
			t.Fatalf("reply %q was handed to two different requests", r.got)
		}
		seen[r.got] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct replies, want %d", len(seen), n)
	}
}

// 7. NoRespondersFastFail: with nothing listening, Request fails with
// FaultNoResponders in under a second. The request timeout is set far longer
// so a timeout cannot pass itself off as a fast fail.
func noRespondersFastFail(t *testing.T, s *suite) {
	b := s.bus(t, ident("p7", "g1", "p7.>"))

	long := 20 * s.budget()
	if long < 30*time.Second {
		long = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), long)
	defer cancel()

	start := time.Now()
	_, err := b.Request(ctx, "p7.void", []byte("anyone there"), bus.WithTimeout(long))
	elapsed := time.Since(start)

	mustCode(t, "Request with no responder", err, bus.FaultNoResponders)
	if elapsed >= fastFailBound {
		t.Fatalf("Request took %v to report no responders, with a %v timeout in force; the contract is a fast fail under %v, not a timeout", elapsed, long, fastFailBound)
	}
}

// 8. RequestHonoursContext: a cancelled context returns promptly, before the
// timeout. The responder is proven to have received the request first, so a
// substrate that never delivered anything cannot pass by erroring instantly.
func requestHonoursContext(t *testing.T, s *suite) {
	b := s.bus(t, ident("p8", "g1", "p8.>"))
	subCtx, cancelSub := s.deadline()
	defer cancelSub()

	long := 20 * s.budget()
	if long < 30*time.Second {
		long = 30 * time.Second
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	sub, err := b.Subscribe(subCtx, "p8.silent", func(_ context.Context, _ *bus.Msg) {
		once.Do(func() { close(entered) })
		<-release // never answers
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()
	defer close(release)

	reqCtx, cancelReq := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := b.Request(reqCtx, "p8.silent", []byte("no answer coming"), bus.WithTimeout(long))
		done <- err
	}()

	select {
	case <-entered:
	case <-time.After(s.budget()):
		t.Fatalf("the responder never received the request within %v; the request was not delivered at all, so cancellation proves nothing", s.budget())
	}

	start := time.Now()
	cancelReq()
	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("Request returned a reply after its context was cancelled, though the responder never answered")
		}
		if elapsed >= long/2 {
			t.Fatalf("Request took %v to notice a cancelled context, against a %v timeout; cancellation must return promptly", elapsed, long)
		}
	case <-time.After(s.budget()):
		t.Fatalf("Request did not return within %v of its context being cancelled", s.budget())
	}
}

// 9. LoudAttributedRefusal: a refused operation returns a bus.Fault whose text
// names BOTH the subject and the principal. Here, and only here, the TEXT is
// the contract: an operator reading a log cannot act on "denied".
func loudAttributedRefusal(t *testing.T, s *suite) {
	const principal = "p9"
	// Granted a neighbouring family, so the refusal is about this subject and
	// not about holding no grants at all.
	b := s.bus(t, ident(principal, "g1", "p9.allowed.>"))

	const subj bus.Subject = "p9.forbidden.subject"
	f := s.mustRefuse(t, b, subj)
	if f.Code != bus.FaultDenied {
		t.Fatalf("refusal of %q carried code %q, want %q", subj, f.Code, bus.FaultDenied)
	}
	text := f.Error()
	if !contains(text, string(subj)) {
		t.Fatalf("refusal text %q does not name the subject %q; an operator cannot tell which call was refused", text, subj)
	}
	if !contains(text, principal) {
		t.Fatalf("refusal text %q does not name the principal %q; an operator cannot tell who was refused", text, principal)
	}
}

// 10. PermanentDenyWins: a subject that is granted AND inside the substrate's
// permanently-ineligible set is still refused. The deny set is not a default a
// grant can override.
func permanentDenyWins(t *testing.T, s *suite) {
	denied := concrete(s.h.Deny[0])
	// Granted explicitly, on every kind.
	b := s.bus(t, ident("p10", "g1", bus.Pattern(denied)))

	f := s.mustRefuse(t, b, denied)
	if f.Code != bus.FaultDenied {
		t.Fatalf("refusal of permanently-denied %q carried code %q, want %q", denied, f.Code, bus.FaultDenied)
	}
}

// 11. SourceUnforgeable: a publisher that sets bd-caller to another id does not
// change what the handler sees. CallerOf reports the real publisher.
func sourceUnforgeable(t *testing.T, s *suite) {
	const realID, forgedID = "p11a", "p11b"
	pub := s.bus(t, ident(realID, "g1", "p11.>"))
	subscriber := s.bus(t, ident(forgedID, "g1", "p11.>"))

	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(4)
	sub, err := subscriber.Subscribe(ctx, "p11.probe", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	forged := bus.Headers{
		bus.HeaderCaller:     forgedID,
		bus.HeaderGeneration: "forged-generation",
		bus.HeaderSubject:    "forged-lease",
	}
	if err := pub.Publish(ctx, "p11.probe", []byte("who sent this"), bus.WithHeaders(forged)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	m := k.next(t, "p11.probe", s.budget())
	caller := bus.CallerOf(m)
	if caller.PluginID != realID {
		t.Fatalf("CallerOf reported %q; the real publisher is %q and the forged header said %q", caller.PluginID, realID, forgedID)
	}
	if caller.Generation == "forged-generation" {
		t.Fatalf("CallerOf reported the forged generation %q", caller.Generation)
	}
	if caller.SubjectLease == "forged-lease" {
		t.Fatalf("CallerOf reported the forged subject lease %q; a plugin must not be able to mint a human principal", caller.SubjectLease)
	}
}

// 12. MaxPayloadRefusal: Caps.MaxPayload bytes are accepted and delivered;
// one more byte is refused.
func maxPayloadRefusal(t *testing.T, s *suite) {
	max := s.h.Caps.MaxPayload
	if max <= 0 {
		t.Skip("substrate declares no Capabilities.MaxPayload")
	}
	b := s.bus(t, ident("p12", "g1", "p12.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	k := newSink(4)
	sub, err := b.Subscribe(ctx, "p12.payload", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	atLimit := bytes.Repeat([]byte("a"), max)
	if err := b.Publish(ctx, "p12.payload", atLimit); err != nil {
		t.Fatalf("Publish of exactly MaxPayload (%d bytes) was refused: %v", max, err)
	}
	m := k.next(t, "p12.payload at the limit", s.budget())
	if len(m.Data) != max {
		t.Fatalf("delivered %d bytes, published %d", len(m.Data), max)
	}

	over := bytes.Repeat([]byte("a"), max+1)
	err = b.Publish(ctx, "p12.payload", over)
	if err == nil {
		t.Fatalf("Publish of MaxPayload+1 (%d bytes) was accepted; the ceiling must be refused, not truncated or deferred", max+1)
	}
	mustFault(t, "Publish of MaxPayload+1", err)
	k.quiet(t, "p12.payload over the limit", s.settle())
}

// 13. BoundedOverflowCountedPublisherNeverBlocks: a blocked handler does not
// block the publisher, and the overflow is counted loudly as
// FaultSlowConsumer on the subscription rather than dropped in silence.
func boundedOverflowCounted(t *testing.T, s *suite) {
	const flood = 400
	b := s.bus(t, ident("p13", "g1", "p13.>"))
	ctx, cancel := context.WithTimeout(context.Background(), 4*s.budget())
	defer cancel()

	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	sub, err := b.Subscribe(ctx, "p13.flood", func(_ context.Context, _ *bus.Msg) {
		first := false
		once.Do(func() { first = true; close(entered) })
		if !first {
			return
		}
		select {
		case <-release:
		case <-time.After(4 * s.budget()):
		}
		close(finished)
	}, bus.PendingLimit(4))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := b.Publish(ctx, "p13.flood", []byte("wake")); err != nil {
		t.Fatalf("Publish(wake): %v", err)
	}
	select {
	case <-entered:
	case <-time.After(s.budget()):
		t.Fatalf("the handler never received the first message within %v; nothing is blocked, so the property proves nothing", s.budget())
	}

	payload := bytes.Repeat([]byte("x"), 256)
	start := time.Now()
	for i := 0; i < flood; i++ {
		// A publish into an overflowing subscription may be refused; it may
		// not block. Only the elapsed time is the assertion here.
		_ = b.Publish(ctx, "p13.flood", payload)
	}
	elapsed := time.Since(start)

	select {
	case <-finished:
		t.Fatal("the handler returned during the publish loop; it was not blocked, so the loop proves nothing about the publisher")
	default:
	}
	if limit := s.budget() / 4; elapsed > limit {
		t.Fatalf("%d publishes took %v while the handler was blocked (limit %v); the publisher is blocking behind the subscriber", flood, elapsed, limit)
	}
	close(release)

	select {
	case <-sub.Done():
	case <-time.After(2 * s.budget()):
		t.Fatalf("the subscription never ended after overflowing a %d-deep queue with %d messages; the drop was silent", 4, flood)
	}
	mustCode(t, "Subscription.Err after overflow", sub.Err(), bus.FaultSlowConsumer)
}

// 14. RevokeDisconnectsWithinBound: after Revoke, the subscription's Done
// closes within a second and Err says why.
func revokeDisconnectsWithinBound(t *testing.T, s *suite) {
	s.needRevoke(t)
	const principal = "p14"
	b := s.bus(t, ident(principal, "g1", "p14.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	sub, err := b.Subscribe(ctx, "p14.live", func(context.Context, *bus.Msg) {})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	start := time.Now()
	s.revoke(t, principal)
	select {
	case <-sub.Done():
		if elapsed := time.Since(start); elapsed >= revokeBound {
			t.Fatalf("the subscription survived %v after the credential was revoked; the bound is %v", elapsed, revokeBound)
		}
	case <-time.After(revokeBound):
		t.Fatalf("the subscription was still live %v after the credential was revoked", revokeBound)
	}
	if sub.Err() == nil {
		t.Fatal("Subscription.Err is nil after a revoke; a withdrawn credential is not a clean cancel and must be reported")
	}
}

// 15. GenerationRevokeDisposal: a revoked generation's subscription delivers
// nothing to the OLD handler once a new generation of the same plugin id has
// connected and subscribed.
func generationRevokeDisposal(t *testing.T, s *suite) {
	s.needRevoke(t)
	const principal = "p15"
	old := s.bus(t, ident(principal, "g1", "p15.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	oldSink := newSink(8)
	oldSub, err := old.Subscribe(ctx, "p15.probe", oldSink.handle)
	if err != nil {
		t.Fatalf("Subscribe(generation g1): %v", err)
	}
	defer oldSub.Cancel()

	s.revoke(t, principal)

	fresh := s.bus(t, ident(principal, "g2", "p15.>"))
	freshCtx, cancelFresh := s.deadline()
	defer cancelFresh()

	newSinkG2 := newSink(8)
	newSub, err := fresh.Subscribe(freshCtx, "p15.probe", newSinkG2.handle)
	if err != nil {
		t.Fatalf("Subscribe(generation g2): %v", err)
	}
	defer newSub.Cancel()

	if err := fresh.Publish(freshCtx, "p15.probe", []byte("after the new generation")); err != nil {
		t.Fatalf("Publish(generation g2): %v", err)
	}
	newSinkG2.next(t, "p15.probe on generation g2", s.budget())
	oldSink.quiet(t, "p15.probe on the revoked generation g1", s.settle())
}

// 16. CredentialIsPerPrincipal: principal A cannot reach a subject only B is
// granted. A grant belongs to a credential, not to the substrate.
func credentialIsPerPrincipal(t *testing.T, s *suite) {
	a := s.bus(t, ident("p16a", "g1", "p16a.>"))
	b := s.bus(t, ident("p16b", "g1", "p16b.>"))

	ctx, cancel := s.deadline()
	defer cancel()
	// B really does hold the grant, so A's refusal is about A's credential and
	// not about the subject being unroutable.
	sub, err := b.Subscribe(ctx, "p16b.data", func(context.Context, *bus.Msg) {})
	if err != nil {
		t.Fatalf("Subscribe as the entitled principal p16b: %v", err)
	}
	defer sub.Cancel()

	f := s.mustRefuse(t, a, "p16b.data")
	if f.Code != bus.FaultDenied {
		t.Fatalf("p16a reaching p16b's subject was refused with code %q, want %q", f.Code, bus.FaultDenied)
	}
}

// 17. StreamPublishConsumeAck: a declared stream stores, replays and
// acknowledges.
func streamPublishConsumeAck(t *testing.T, s *suite) {
	const stream = "p17"
	b := s.bus(t, withStream(ident("p17", "g1", "p17.>"), stream))
	ctx, cancel := s.deadline()
	defer cancel()

	declare(t, b, stream, "p17.>")

	for i := 0; i < 3; i++ {
		seq, err := b.Streams().Publish(ctx, "p17.ev", []byte(strconv.Itoa(i)))
		if err != nil {
			t.Fatalf("Streams().Publish(%d): %v", i, err)
		}
		if seq == 0 {
			t.Fatalf("Streams().Publish(%d) returned sequence 0; a stored message has a position", i)
		}
	}

	got := make(chan string, 8)
	consumer, err := b.Streams().Consume(ctx, stream, bus.ConsumerSpec{
		Ack:   bus.AckExplicit,
		Start: bus.StartAll,
	}, func(_ context.Context, m *bus.StreamMsg) {
		if err := m.Ack(); err != nil {
			t.Errorf("Ack(seq %s): %v", m.Seq, err)
		}
		select {
		case got <- string(m.Data):
		default:
		}
	})
	if err != nil {
		t.Fatalf("Streams().Consume: %v", err)
	}
	defer consumer.Cancel()

	for i := 0; i < 3; i++ {
		select {
		case data := <-got:
			if data != strconv.Itoa(i) {
				t.Fatalf("stream replay out of order at %d: want %q, got %q", i, strconv.Itoa(i), data)
			}
		case <-time.After(s.budget()):
			t.Fatalf("stream message %d was never delivered within %v", i, s.budget())
		}
	}
}

// 18. ReplayFromCursorAfterRestart: consume part of a stream, keep the cursor,
// restart the substrate, resume from the cursor and receive exactly the rest —
// no gap, no duplicate.
//
// Cursor() is the consumer's ACK FLOOR, not its delivery position: it covers
// everything the consumer has acknowledged, which is what makes a resume
// at-least-once rather than lossy. A message that was delivered but not acked
// is BEFORE the cursor and is replayed. The first pass therefore acknowledges
// exactly `first` messages and deliberately leaves the rest unacked, so the
// floor really is at `first` when the cursor is read. A handler that acked
// everything it was handed would move the floor to the end of the stream, and
// the resume would correctly yield nothing — the test would be asserting on
// its own channel buffer rather than on the substrate's position.
func replayFromCursorAfterRestart(t *testing.T, s *suite) {
	s.needRestart(t)
	const stream, total, first = "p18", 6, 3
	id := withStream(ident("p18", "g1", "p18.>"), stream)
	b := s.bus(t, id)
	ctx, cancel := s.deadline()
	defer cancel()

	declare(t, b, stream, "p18.>")
	for i := 0; i < total; i++ {
		if _, err := b.Streams().Publish(ctx, "p18.ev", []byte(strconv.Itoa(i))); err != nil {
			t.Fatalf("Streams().Publish(%d): %v", i, err)
		}
	}

	got := make(chan string, total*2)
	var acked atomic.Int64
	consumer, err := b.Streams().Consume(ctx, stream, bus.ConsumerSpec{
		Ack:   bus.AckExplicit,
		Start: bus.StartAll,
	}, func(_ context.Context, m *bus.StreamMsg) {
		// Deliveries are sequential per consumer, so a counter is enough.
		// Anything past `first` is left unacked on purpose: it must stay
		// behind the ack floor so the cursor means what this test claims.
		if acked.Add(1) > first {
			return
		}
		if err := m.Ack(); err != nil {
			t.Errorf("first pass Ack(seq %s): %v", m.Seq, err)
			return
		}
		select {
		case got <- string(m.Data):
		default:
		}
	})
	if err != nil {
		t.Fatalf("Streams().Consume: %v", err)
	}
	for i := 0; i < first; i++ {
		select {
		case data := <-got:
			if data != strconv.Itoa(i) {
				t.Fatalf("first pass out of order at %d: want %q, got %q", i, strconv.Itoa(i), data)
			}
		case <-time.After(s.budget()):
			t.Fatalf("first pass: message %d never arrived within %v", i, s.budget())
		}
	}
	cursor := consumer.Cursor()
	if cursor.IsZero() {
		t.Fatalf("Consumer.Cursor is zero after %d acknowledged messages; there is nothing to resume from", first)
	}
	consumer.Cancel()

	s.restart(t)

	resumed := s.bus(t, id)
	ctx2, cancel2 := s.deadline()
	defer cancel2()

	after := make(chan string, total*2)
	consumer2, err := resumed.Streams().Consume(ctx2, stream, bus.ConsumerSpec{
		Ack:    bus.AckExplicit,
		Start:  bus.StartCursor,
		Cursor: cursor,
	}, func(_ context.Context, m *bus.StreamMsg) {
		_ = m.Ack()
		select {
		case after <- string(m.Data):
		default:
		}
	})
	if err != nil {
		t.Fatalf("Streams().Consume(StartCursor) after restart: %v", err)
	}
	defer consumer2.Cancel()

	for i := first; i < total; i++ {
		select {
		case data := <-after:
			if data != strconv.Itoa(i) {
				t.Fatalf("resume from cursor: want %q at position %d, got %q; the cursor skipped or repeated", strconv.Itoa(i), i, data)
			}
		case <-time.After(s.budget()):
			t.Fatalf("resume from cursor: message %d never arrived within %v", i, s.budget())
		}
	}
	select {
	case extra := <-after:
		t.Fatalf("resume from cursor delivered %q after the stream was exhausted; the cursor replayed a message that was already consumed", extra)
	case <-time.After(s.settle()):
	}
}

// 19. MsgIDDedupe: the same WithMsgID published twice inside the dedupe window
// is stored once.
func msgIDDedupe(t *testing.T, s *suite) {
	const stream = "p19"
	b := s.bus(t, withStream(ident("p19", "g1", "p19.>"), stream))
	ctx, cancel := s.deadline()
	defer cancel()

	if err := b.Streams().Declare(ctx, bus.StreamSpec{
		Name:      stream,
		Subjects:  []bus.Pattern{"p19.>"},
		Retention: bus.RetentionLimits,
		MaxAge:    time.Hour,
		MaxBytes:  1 << 20,
		Dedupe:    time.Minute,
	}); err != nil {
		t.Fatalf("Streams().Declare: %v", err)
	}

	first, err := b.Streams().Publish(ctx, "p19.ev", []byte("once"), bus.WithMsgID("p19-idempotency-key"))
	if err != nil {
		t.Fatalf("first Streams().Publish: %v", err)
	}
	second, err := b.Streams().Publish(ctx, "p19.ev", []byte("once"), bus.WithMsgID("p19-idempotency-key"))
	if err != nil {
		t.Fatalf("repeat Streams().Publish: %v", err)
	}
	if second != first {
		t.Fatalf("a repeated msg-id returned sequence %s, the original was %s; a deduplicated publish reports the stored message", second, first)
	}

	info, err := b.Streams().Info(ctx, stream)
	if err != nil {
		t.Fatalf("Streams().Info: %v", err)
	}
	if info.Msgs != 1 {
		t.Fatalf("the stream holds %d messages after publishing one msg-id twice inside a %v dedupe window; want 1", info.Msgs, time.Minute)
	}
}

// 20. PutGetWatch: a KV bucket stores, versions and notifies.
func putGetWatch(t *testing.T, s *suite) {
	const bucket = "p20"
	b := s.bus(t, withKV(ident("p20", "g1", "p20.>"), bucket))
	ctx, cancel := s.deadline()
	defer cancel()

	handle := openBucket(t, b, bucket)

	watcher, err := handle.Watch(ctx, ">")
	if err != nil {
		t.Fatalf("Bucket.Watch: %v", err)
	}
	defer watcher.Stop()

	rev, err := handle.Put(ctx, "key", []byte("value-1"))
	if err != nil {
		t.Fatalf("Bucket.Put: %v", err)
	}
	if rev == 0 {
		t.Fatal("Bucket.Put returned revision 0; a stored value is versioned")
	}

	entry, err := handle.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Bucket.Get: %v", err)
	}
	if string(entry.Value) != "value-1" {
		t.Fatalf("Bucket.Get returned %q, want %q", entry.Value, "value-1")
	}
	if entry.Revision != rev {
		t.Fatalf("Bucket.Get reported revision %s, Put returned %s", entry.Revision, rev)
	}

	deadline := time.After(s.budget())
	for {
		select {
		case e, ok := <-watcher.Updates():
			if !ok {
				t.Fatalf("the watcher closed before delivering the write; Err: %v", watcher.Err())
			}
			if e.Key != "key" {
				continue
			}
			if string(e.Value) != "value-1" {
				t.Fatalf("the watcher delivered %q for key, want %q", e.Value, "value-1")
			}
			return
		case <-deadline:
			t.Fatalf("the watcher never reported the write within %v", s.budget())
		}
	}
}

// 21. KVSurvivesRestart: a KV value written before a restart is readable
// afterwards, at its revision.
func kvSurvivesRestart(t *testing.T, s *suite) {
	s.needRestart(t)
	const bucket = "p21"
	id := withKV(ident("p21", "g1", "p21.>"), bucket)
	b := s.bus(t, id)
	ctx, cancel := s.deadline()
	defer cancel()

	handle := openBucket(t, b, bucket)
	rev, err := handle.Put(ctx, "durable-key", []byte("survives"))
	if err != nil {
		t.Fatalf("Bucket.Put: %v", err)
	}

	s.restart(t)

	resumed := s.bus(t, id)
	ctx2, cancel2 := s.deadline()
	defer cancel2()

	handle2, err := resumed.KV().Open(ctx2, bucket)
	if err != nil {
		t.Fatalf("KV().Open after restart: %v", err)
	}
	entry, err := handle2.Get(ctx2, "durable-key")
	if err != nil {
		t.Fatalf("Bucket.Get after restart: %v", err)
	}
	if string(entry.Value) != "survives" {
		t.Fatalf("after restart the key reads %q, want %q", entry.Value, "survives")
	}
	if entry.Revision != rev {
		t.Fatalf("after restart the key is at revision %s, want %s; the restart rewrote history", entry.Revision, rev)
	}
}

// 22. ScheduledPublishFires: a scheduled publish reaches an ordinary
// subscriber.
func scheduledPublishFires(t *testing.T, s *suite) {
	b := s.bus(t, ident("p22", "g1", "p22.>"))
	ctx, cancel := context.WithTimeout(context.Background(), 4*s.budget())
	defer cancel()

	k := newSink(4)
	sub, err := b.Subscribe(ctx, "p22.tick", k.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	delay := s.budget() / 10
	if delay < 50*time.Millisecond {
		delay = 50 * time.Millisecond
	}
	if err := b.Schedule().At(ctx, "p22-once", time.Now().Add(delay), "p22.tick", []byte("fired")); err != nil {
		t.Fatalf("Schedule().At: %v", err)
	}
	defer func() { _ = b.Schedule().Cancel(context.Background(), "p22-once") }()

	m := k.next(t, "p22.tick scheduled publish", 3*s.budget())
	if string(m.Data) != "fired" {
		t.Fatalf("the scheduled publish delivered %q, want %q", m.Data, "fired")
	}
}

// 23. ScheduledPublishSurvivesRestart: a repeating schedule declared before a
// restart is still listed and still firing afterwards.
//
// The schedule repeats deliberately. A one-shot would race the restart: if it
// fired while nothing was subscribed the message is simply gone, and the test
// could not tell that apart from a schedule the restart dropped.
func scheduledPublishSurvivesRestart(t *testing.T, s *suite) {
	s.needRestart(t)
	id := ident("p23", "g1", "p23.>")
	b := s.bus(t, id)
	ctx, cancel := s.deadline()
	defer cancel()

	every := s.budget() / 10
	if every < 50*time.Millisecond {
		every = 50 * time.Millisecond
	}
	if err := b.Schedule().Every(ctx, "p23-repeat", every, "p23.tick", []byte("still firing")); err != nil {
		t.Fatalf("Schedule().Every: %v", err)
	}

	s.restart(t)

	resumed := s.bus(t, id)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 4*s.budget())
	defer cancel2()
	defer func() { _ = resumed.Schedule().Cancel(context.Background(), "p23-repeat") }()

	schedules, err := resumed.Schedule().List(ctx2)
	if err != nil {
		t.Fatalf("Schedule().List after restart: %v", err)
	}
	found := false
	for _, sc := range schedules {
		if sc.Name == "p23-repeat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Schedule().List does not contain %q after a restart; the schedule did not survive", "p23-repeat")
	}

	k := newSink(8)
	sub, err := resumed.Subscribe(ctx2, "p23.tick", k.handle)
	if err != nil {
		t.Fatalf("Subscribe after restart: %v", err)
	}
	defer sub.Cancel()

	m := k.next(t, "p23.tick after restart", 3*s.budget())
	if string(m.Data) != "still firing" {
		t.Fatalf("the surviving schedule delivered %q, want %q", m.Data, "still firing")
	}
}

// 24. RegisterDiscoverCall: a mounted service is discoverable by name and
// callable at its endpoint.
func registerDiscoverCall(t *testing.T, s *suite) {
	b := s.bus(t, ident("p24", "g1", "svc.p24.>"))
	ctx, cancel := s.deadline()
	defer cancel()

	const endpoint bus.Subject = "svc.p24.probe.v1.ping"
	svc, err := b.Services().Serve(ctx, bus.ServiceSpec{
		Name:    "p24-probe",
		Version: "1.0.0",
		Endpoints: []bus.EndpointSpec{{
			Name:    "ping",
			Subject: endpoint,
			Handler: func(_ context.Context, m *bus.Msg) {
				_ = m.Respond(nil, []byte("pong"))
			},
		}},
	})
	if err != nil {
		t.Fatalf("Services().Serve: %v", err)
	}
	defer func() { _ = svc.Stop(context.Background()) }()

	found, err := b.Services().Discover(ctx, "p24-probe")
	if err != nil {
		t.Fatalf("Services().Discover: %v", err)
	}
	if len(found) == 0 {
		t.Fatal(`Services().Discover("p24-probe") returned nothing for a service that is mounted`)
	}
	hasEndpoint := false
	for _, info := range found {
		for _, e := range info.Endpoints {
			if e.Subject == endpoint {
				hasEndpoint = true
			}
		}
	}
	if !hasEndpoint {
		t.Fatalf("the discovered service does not advertise %q; discovery that omits the endpoint is not discovery", endpoint)
	}

	reply, err := b.Services().Call(ctx, endpoint, []byte("ping"))
	if err != nil {
		t.Fatalf("Services().Call(%q): %v", endpoint, err)
	}
	if reply == nil || string(reply.Data) != "pong" {
		t.Fatalf("Services().Call returned %q, want %q", replyData(reply), "pong")
	}
}

// 25. ObjectPutGetList: an object bucket stores bytes by name and reports
// them faithfully — the same bytes back from Get, the same size and digest
// from Put, Get and Info alike, and the name in List.
//
// The overwrite at the end is the part that is easy to leave out: without it
// the property passes against a store that digests the NAME rather than the
// content, which is exactly the shape a stub grows into.
func objectPutGetList(t *testing.T, s *suite) {
	const bucket = "p25"
	b := s.bus(t, withObjects(ident("p25", "g1", "p25.>"), bucket))
	ctx, cancel := s.deadline()
	defer cancel()

	if err := b.Objects().Declare(ctx, bus.BucketSpec{Name: bucket, MaxBytes: 1 << 20}); err != nil {
		t.Fatalf("Objects().Declare(%q): %v", bucket, err)
	}

	// Larger than the core payload ceiling would comfortably carry, because
	// "the bus carries the name, not the bytes" is the point of the surface.
	payload := bytes.Repeat([]byte("object-bytes."), 1024)
	put, err := b.Objects().Put(ctx, bus.ObjectMeta{Bucket: bucket, Name: "artifact", Description: "p25"}, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Objects().Put: %v", err)
	}
	if put.Size != int64(len(payload)) {
		t.Fatalf("Put reported size %d for a %d-byte object", put.Size, len(payload))
	}
	if put.Digest == "" {
		t.Fatal("Put reported no digest; a blob store that does not identify its content gives a reader nothing to verify against")
	}

	rc, meta, err := b.Objects().Get(ctx, bucket, "artifact")
	if err != nil {
		t.Fatalf("Objects().Get: %v", err)
	}
	got, readErr := io.ReadAll(rc)
	if err := rc.Close(); err != nil {
		t.Fatalf("closing the object reader: %v", err)
	}
	if readErr != nil {
		t.Fatalf("reading the object: %v", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Get returned %d bytes, want the %d that were put", len(got), len(payload))
	}
	if meta.Size != put.Size || meta.Digest != put.Digest {
		t.Fatalf("Get reports size %d digest %q; Put reported size %d digest %q", meta.Size, meta.Digest, put.Size, put.Digest)
	}

	info, err := b.Objects().Info(ctx, bucket, "artifact")
	if err != nil {
		t.Fatalf("Objects().Info: %v", err)
	}
	if info.Digest != put.Digest || info.Size != put.Size {
		t.Fatalf("Info reports size %d digest %q, Put reported size %d digest %q; Info is metadata without the bytes, not different metadata", info.Size, info.Digest, put.Size, put.Digest)
	}

	listed, err := b.Objects().List(ctx, bucket)
	if err != nil {
		t.Fatalf("Objects().List: %v", err)
	}
	found := false
	for _, m := range listed {
		if m.Name == "artifact" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Objects().List(%q) returned %d entries and none of them is the object just put", bucket, len(listed))
	}

	replacement := []byte("something else entirely")
	put2, err := b.Objects().Put(ctx, bus.ObjectMeta{Bucket: bucket, Name: "artifact"}, bytes.NewReader(replacement))
	if err != nil {
		t.Fatalf("second Objects().Put: %v", err)
	}
	if put2.Digest == put.Digest {
		t.Fatalf("overwriting %d bytes with %d left the digest at %q; the digest does not identify the content", len(payload), len(replacement), put2.Digest)
	}
	if put2.Size != int64(len(replacement)) {
		t.Fatalf("after the overwrite Put reports size %d, want %d", put2.Size, len(replacement))
	}
}

// 26. CounterAccumulatesAtomically: Counter adds to a per-subject total on a
// stream and returns the new value, and concurrent adds all land.
//
// The concurrent half is the word "atomically" in bus.Capabilities. A
// read-modify-write that is not atomic loses increments under contention and
// nothing else in the suite would notice.
func counterAccumulatesAtomically(t *testing.T, s *suite) {
	const stream = "p26"
	const subject bus.Subject = "p26.hits"
	b := s.bus(t, withStream(ident("p26", "g1", "p26.>"), stream))
	ctx, cancel := s.deadline()
	defer cancel()
	declare(t, b, stream, "p26.>")

	first, err := b.Streams().Counter(ctx, subject, 3)
	if err != nil {
		t.Fatalf("Streams().Counter(+3): %v", err)
	}
	if first != 3 {
		t.Fatalf("the first Counter(+3) returned %d, want 3; a counter that has never been touched is zero", first)
	}
	next, err := b.Streams().Counter(ctx, subject, 4)
	if err != nil {
		t.Fatalf("Streams().Counter(+4): %v", err)
	}
	if next != 7 {
		t.Fatalf("Counter(+3) then Counter(+4) returned %d, want 7; the counter does not accumulate", next)
	}

	// A second subject on the same stream is a second counter: the capability
	// is "the atomic per-subject counter on a stream", not one per stream.
	other, err := b.Streams().Counter(ctx, "p26.other", 1)
	if err != nil {
		t.Fatalf("Streams().Counter on a second subject: %v", err)
	}
	if other != 1 {
		t.Fatalf("a second subject's counter reads %d, want 1; the two subjects share one counter", other)
	}

	const workers, each = 8, 25
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if _, err := b.Streams().Counter(ctx, subject, 1); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		t.Fatalf("concurrent Streams().Counter: %v", err)
	}

	// Adding zero reads the total without changing it.
	total, err := b.Streams().Counter(ctx, subject, 0)
	if err != nil {
		t.Fatalf("final Streams().Counter(+0): %v", err)
	}
	if want := int64(7 + workers*each); total != want {
		t.Fatalf("after %d concurrent increments the counter reads %d, want %d; %d increments were lost", workers*each, total, want, want-total)
	}
}

// 27. BatchPublishIsAllOrNothing: PublishBatch "appends every message or
// none", so a batch that cannot be stored whole leaves the stream untouched.
//
// The message count before and after is the only observable that distinguishes
// atomicity from a loop that appends until it hits the fault: both return an
// error, and only one of them leaves the earlier messages behind.
func batchPublishIsAllOrNothing(t *testing.T, s *suite) {
	const stream = "p27"
	b := s.bus(t, withStream(ident("p27", "g1", "p27.>"), stream))
	ctx, cancel := s.deadline()
	defer cancel()
	declare(t, b, stream, "p27.>")

	seqs, err := b.Streams().PublishBatch(ctx, []bus.BatchMsg{
		{Subject: "p27.a", Data: []byte("1")},
		{Subject: "p27.b", Data: []byte("2")},
		{Subject: "p27.c", Data: []byte("3")},
	})
	if err != nil {
		t.Fatalf("Streams().PublishBatch: %v", err)
	}
	if len(seqs) != 3 {
		t.Fatalf("PublishBatch returned %d sequences for a batch of 3", len(seqs))
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("PublishBatch returned sequences %v; a batch is appended in the order it was given", seqs)
		}
	}
	before, err := b.Streams().Info(ctx, stream)
	if err != nil {
		t.Fatalf("Streams().Info: %v", err)
	}
	if before.Msgs != 3 {
		t.Fatalf("the stream holds %d messages after a batch of 3", before.Msgs)
	}

	// The second message's precondition is false on purpose, so the batch
	// cannot be stored whole. The first message must not survive it.
	_, err = b.Streams().PublishBatch(ctx, []bus.BatchMsg{
		{Subject: "p27.a", Data: []byte("4")},
		{Subject: "p27.b", Data: []byte("5"), Opts: []bus.PublishOpt{bus.ExpectLastSeq(99)}},
	})
	if err == nil {
		t.Fatal("a batch whose second message expects sequence 99 was accepted; the precondition was never checked")
	}
	mustCode(t, "PublishBatch with a false precondition", err, bus.FaultConflict)

	after, err := b.Streams().Info(ctx, stream)
	if err != nil {
		t.Fatalf("Streams().Info after the refused batch: %v", err)
	}
	if after.Msgs != before.Msgs {
		t.Fatalf("the stream holds %d messages after a batch that was refused; it held %d before. PublishBatch stored part of a batch it did not accept", after.Msgs, before.Msgs)
	}
}

// ------------------------------------------------------------------ helpers

// declare creates the standard bounded stream a property needs. MaxBytes and
// MaxAge are mandatory in StreamSpec, so there is no unbounded shortcut.
func declare(t *testing.T, b bus.Bus, name string, subjects ...bus.Pattern) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.Streams().Declare(ctx, bus.StreamSpec{
		Name:      name,
		Subjects:  subjects,
		Retention: bus.RetentionLimits,
		MaxAge:    time.Hour,
		MaxBytes:  1 << 20,
	}); err != nil {
		t.Fatalf("Streams().Declare(%q): %v", name, err)
	}
}

// openBucket declares and opens the standard KV bucket a property needs.
func openBucket(t *testing.T, b bus.Bus, name string) bus.Bucket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.KV().Declare(ctx, bus.BucketSpec{Name: name, History: 4, MaxBytes: 1 << 20}); err != nil {
		t.Fatalf("KV().Declare(%q): %v", name, err)
	}
	handle, err := b.KV().Open(ctx, name)
	if err != nil {
		t.Fatalf("KV().Open(%q): %v", name, err)
	}
	if handle == nil {
		t.Fatalf("KV().Open(%q) returned a nil Bucket and no error", name)
	}
	return handle
}

func replyData(m *bus.Msg) string {
	if m == nil {
		return "<nil reply>"
	}
	return string(m.Data)
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && bytes.Contains([]byte(haystack), []byte(needle))
}
