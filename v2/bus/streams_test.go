package bus

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestSeqMarshalsAsADecimalString is not a style choice. A uint64 past 2^53
// loses precision in every JSON parser that backs a browser, and a sequence
// that silently rounds is a replay that silently skips. The browser is a
// first-class consumer of these numbers from plan 3 onward.
func TestSeqMarshalsAsADecimalString(t *testing.T) {
	const big = Seq(9007199254740993) // 2^53 + 1: the first value a float64 cannot hold
	b, err := json.Marshal(big)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"9007199254740993"` {
		t.Fatalf("Seq marshalled as %s, want a quoted decimal string", b)
	}

	var back Seq
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back != big {
		t.Fatalf("round trip lost precision: %d != %d", back, big)
	}

	// The proof that the string form is load-bearing: through a float64, the
	// same value comes back wrong.
	var asFloat float64
	if err := json.Unmarshal([]byte("9007199254740993"), &asFloat); err != nil {
		t.Fatal(err)
	}
	if uint64(asFloat) == uint64(big) {
		t.Skip("this platform's float64 holds 2^53+1; the hazard this guards is absent here")
	}
}

func TestRevisionMarshalsAsADecimalString(t *testing.T) {
	b, err := json.Marshal(Revision(42))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"42"` {
		t.Fatalf("Revision marshalled as %s", b)
	}
	var r Revision
	if err := json.Unmarshal([]byte(`"42"`), &r); err != nil || r != 42 {
		t.Fatalf("Unmarshal = %d, %v", r, err)
	}
	// A hand-written fixture may use a number; it is accepted, not required.
	if err := json.Unmarshal([]byte(`43`), &r); err != nil || r != 43 {
		t.Fatalf("a JSON number must still decode: %d, %v", r, err)
	}
	if err := json.Unmarshal([]byte(`"not-a-number"`), &r); err == nil {
		t.Error("a non-numeric string must be refused, not silently zeroed")
	}
	if err := json.Unmarshal([]byte(`true`), &r); err == nil {
		t.Error("a boolean must be refused")
	}
}

func TestSeqAndRevisionString(t *testing.T) {
	if Seq(7).String() != "7" || Revision(7).String() != "7" {
		t.Error("String is the plain decimal form")
	}
}

// TestCursorIsOpaque pins the contract: a cursor round-trips through JSON and
// tells the holder nothing. Nothing here parses one, because nothing anywhere
// may.
func TestCursorIsOpaque(t *testing.T) {
	c := NewCursor("v1:aGVsbG8:9f86d0")
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Cursor
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Token() != c.Token() {
		t.Fatalf("cursor did not round trip: %q != %q", back.Token(), c.Token())
	}
	if !(Cursor{}).IsZero() {
		t.Error("the zero cursor is zero")
	}
	if c.IsZero() {
		t.Error("a minted cursor is not zero")
	}
}

// TestStreamMsgWithoutAnAcknowledgementRefuses: a handler that acks a message
// it did not receive from a stream must find out, not have the ack vanish.
func TestStreamMsgWithoutAnAcknowledgementRefuses(t *testing.T) {
	m := &StreamMsg{}
	for name, call := range map[string]func() error{
		"Ack":        m.Ack,
		"InProgress": m.InProgress,
		"Nak":        func() error { return m.Nak(0) },
		"Term":       func() error { return m.Term("why") },
	} {
		var f Fault
		if err := call(); !errors.As(err, &f) || f.Code != FaultUnhandled {
			t.Errorf("%s returned %v, want a FaultUnhandled", name, err)
		}
	}
}

func TestStreamMsgAcknowledgementsCarryTheirArguments(t *testing.T) {
	type call struct {
		kind   string
		delay  time.Duration
		reason string
	}
	var got call
	m := NewStreamMsg(Msg{Subject: "event.files.changed"}, 7, time.Now(), 1,
		func(kind string, delay time.Duration, reason string) error {
			got = call{kind, delay, reason}
			return nil
		})
	if m.Seq != 7 || m.Delivered != 1 {
		t.Fatalf("NewStreamMsg lost its position: %+v", m)
	}
	if err := m.Nak(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got != (call{"nak", 2 * time.Second, ""}) {
		t.Fatalf("Nak delivered %+v", got)
	}
	if err := m.Term("poisoned"); err != nil {
		t.Fatal(err)
	}
	if got != (call{"term", 0, "poisoned"}) {
		t.Fatalf("Term delivered %+v", got)
	}
}

func TestResolvePublish(t *testing.T) {
	o := ResolvePublish([]PublishOpt{
		WithHeaders(Headers{"X-A": "1"}),
		WithMsgID("id-1"),
		ExpectLastSeq(9),
		WithTTL(time.Minute),
		nil, // a nil option is ignored, not a panic
	})
	if o.Headers.Get("x-a") != "1" || o.MsgID != "id-1" || o.ExpectLastSeq != 9 || o.TTL != time.Minute {
		t.Fatalf("ResolvePublish = %+v", o)
	}
}

// TestWithHeadersStripsReserved: an option is the other place a plugin could
// try to write bd-caller. It is stripped at construction, before any substrate
// sees it.
func TestWithHeadersStripsReserved(t *testing.T) {
	o := ResolvePublish([]PublishOpt{WithHeaders(Headers{HeaderCaller: "someone-else", "x-mine": "kept"})})
	if _, ok := o.Headers[HeaderCaller]; ok {
		t.Fatal("WithHeaders let a reserved header through")
	}
	if o.Headers.Get("x-mine") != "kept" {
		t.Fatal("WithHeaders dropped an ordinary header")
	}
	r := ResolveReq([]ReqOpt{WithHeaders(Headers{HeaderGeneration: "999"})})
	if _, ok := r.Headers[HeaderGeneration]; ok {
		t.Fatal("WithHeaders let a reserved header through on a request")
	}
}

func TestResolveSubDefaultsThePendingLimit(t *testing.T) {
	if got := ResolveSub(nil).PendingLimit; got != DefaultPendingLimit {
		t.Fatalf("default PendingLimit = %d, want %d", got, DefaultPendingLimit)
	}
	if got := ResolveSub([]SubOpt{PendingLimit(0)}).PendingLimit; got != DefaultPendingLimit {
		t.Fatalf("PendingLimit(0) = %d, want the default; an unbounded queue is the hazard this bounds", got)
	}
	if got := ResolveSub([]SubOpt{PendingLimit(-1)}).PendingLimit; got != DefaultPendingLimit {
		t.Fatalf("a negative PendingLimit = %d, want the default", got)
	}
	o := ResolveSub([]SubOpt{QueueGroup("ui"), PendingLimit(8)})
	if o.QueueGroup != "ui" || o.PendingLimit != 8 {
		t.Fatalf("ResolveSub = %+v", o)
	}
}

func TestResolveReqDefaultsTheTimeout(t *testing.T) {
	if got := ResolveReq(nil).Timeout; got != DefaultTimeout {
		t.Fatalf("default timeout = %v, want %v", got, DefaultTimeout)
	}
	if got := ResolveReq([]ReqOpt{WithTimeout(0)}).Timeout; got != DefaultTimeout {
		t.Fatalf("WithTimeout(0) = %v, want the default", got)
	}
	if got := ResolveReq([]ReqOpt{WithTimeout(time.Second)}).Timeout; got != time.Second {
		t.Fatalf("WithTimeout = %v", got)
	}
}

func TestCapabilitiesHasCoversTheWholeVocabulary(t *testing.T) {
	all := Capabilities{Durable: true, KV: true, Objects: true, Services: true,
		Schedule: true, Counters: true, Batch: true, Trace: true}
	for _, n := range CapabilityNames() {
		if !all.Has(n) {
			t.Errorf("Has(%q) = false on a fully-capable substrate: the vocabulary and the struct have drifted", n)
		}
		if (Capabilities{}).Has(n) {
			t.Errorf("Has(%q) = true on an empty Capabilities", n)
		}
	}
	if all.Has("telepathy") {
		t.Error("an unknown capability name must fail closed")
	}
	if len(CapabilityNames()) != 8 {
		t.Errorf("the needs vocabulary is closed at 8 names, got %d", len(CapabilityNames()))
	}
}

func TestCapabilitiesMissingPreservesOrder(t *testing.T) {
	c := Capabilities{Durable: true, KV: true}
	got := c.Missing([]string{"durable", "services", "kv", "schedule"})
	if len(got) != 2 || got[0] != "services" || got[1] != "schedule" {
		t.Fatalf("Missing = %v, want [services schedule] in the order asked", got)
	}
	if got := c.Missing([]string{"durable", "kv"}); got != nil {
		t.Fatalf("Missing = %v, want nil when everything is present", got)
	}
}

// TestWithSchemaAndWithCorrelationSurviveTheStrip is the regression test for a
// real defect: bd-schema is host-reserved, WithHeaders strips the whole
// reserved namespace, so the typed layer's schema stamp was being removed on
// the way out and the receive-side check could never fire. A check that cannot
// fire is worse than no check, because it reads as one.
//
// The two headers a caller may legitimately set now have their own options,
// and the identity triple still has none.
func TestWithSchemaAndWithCorrelationSurviveTheStrip(t *testing.T) {
	o := ResolvePublish([]PublishOpt{
		WithHeaders(Headers{"x-mine": "1", HeaderCaller: "someone-else"}),
		WithSchema("sha256:abc"),
		WithCorrelation("corr-1"),
	})
	if got := o.Headers.Get(HeaderSchema); got != "sha256:abc" {
		t.Fatalf("bd-schema = %q, want the stamped hash", got)
	}
	if got := o.Headers.Get(HeaderCorrelation); got != "corr-1" {
		t.Fatalf("bd-corr = %q, want the stamped id", got)
	}
	if _, ok := o.Headers[HeaderCaller]; ok {
		t.Fatal("bd-caller is still forgeable; the identity triple must have no option and survive no strip")
	}
	if o.Headers.Get("x-mine") != "1" {
		t.Fatal("an ordinary header was lost")
	}

	r := ResolveReq([]ReqOpt{WithSchema("sha256:def"), WithCorrelation("corr-2"), WithHeaders(Headers{HeaderGeneration: "999"})})
	if r.Headers.Get(HeaderSchema) != "sha256:def" || r.Headers.Get(HeaderCorrelation) != "corr-2" {
		t.Fatalf("request headers = %v", r.Headers)
	}
	if _, ok := r.Headers[HeaderGeneration]; ok {
		t.Fatal("bd-generation is still forgeable on a request")
	}

	// An empty value writes nothing rather than an empty header, so an
	// unstamped descriptor does not produce a header that looks set.
	if o := ResolvePublish([]PublishOpt{WithSchema("")}); len(o.Headers) != 0 {
		t.Fatalf("WithSchema(\"\") wrote %v", o.Headers)
	}
}

// TestIdentityHeadersHaveNoOption pins the asymmetry by construction: there is
// no exported option that can set an identity header. If someone adds one, the
// unforgeability property in the conformance suite becomes a lie, so the rule
// is stated here as well as enforced by the substrate.
func TestIdentityHeadersHaveNoOption(t *testing.T) {
	for _, h := range []string{HeaderCaller, HeaderGeneration, HeaderSubject} {
		if !IsReservedHeader(h) {
			t.Errorf("%q must be in the reserved namespace", h)
		}
		if got := StripReserved(Headers{h: "forged"}); len(got) != 0 {
			t.Errorf("StripReserved kept %q", h)
		}
	}
}
