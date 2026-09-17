package bus

import (
	"errors"
	"testing"
)

func TestHeadersGetIsCaseInsensitive(t *testing.T) {
	// NATS canonicalises header keys, so a contract that depended on case
	// would break on the first real broker. Get is where that is absorbed.
	h := Headers{"bd-corr": "abc", "Content-Type": "application/json"}
	for _, k := range []string{"bd-corr", "BD-CORR", "Bd-Corr"} {
		if got := h.Get(k); got != "abc" {
			t.Errorf("Get(%q) = %q, want %q", k, got, "abc")
		}
	}
	if got := h.Get("content-type"); got != "application/json" {
		t.Errorf("Get(%q) = %q", "content-type", got)
	}
	if got := h.Get("absent"); got != "" {
		t.Errorf("a missing header reads as empty, got %q", got)
	}
	if got := Headers(nil).Get("anything"); got != "" {
		t.Errorf("nil Headers reads as empty, got %q", got)
	}
}

func TestHeadersCloneLowercases(t *testing.T) {
	c := Headers{"BD-Corr": "abc", "X-Thing": "1"}.Clone()
	if _, ok := c["bd-corr"]; !ok {
		t.Errorf("Clone did not lowercase: %v", c)
	}
	if _, ok := c["BD-Corr"]; ok {
		t.Errorf("Clone kept the original spelling: %v", c)
	}
	if Headers(nil).Clone() != nil {
		t.Error("cloning nil yields nil")
	}
}

// TestStripReservedIsHowSourceStaysUnforgeable: a sender may write anything it
// likes into bd-caller. StripReserved is what the substrate calls before
// stamping the real value, so the handler never sees the sender's version.
func TestStripReservedIsHowSourceStaysUnforgeable(t *testing.T) {
	sent := Headers{
		HeaderCaller:     "someone-else",
		HeaderGeneration: "999",
		HeaderSubject:    "forged-lease",
		"BD-Anything":    "x",
		"x-mine":         "kept",
	}
	got := StripReserved(sent)
	for _, k := range IdentityHeaders() {
		if _, ok := got[k]; ok {
			t.Errorf("StripReserved kept %q, which carries authority", k)
		}
	}
	if got.Get("x-mine") != "kept" {
		t.Error("StripReserved dropped an ordinary header")
	}
	// Scope matters as much as the strip: a bd- header that is NOT identity
	// must survive, or the typed layer's bd-schema stamp never reaches the
	// receiver and its check silently becomes a no-op.
	if got.Get("bd-anything") != "x" {
		t.Error("StripReserved removed a non-identity bd- header; bd-schema would not survive either")
	}
	if StripReserved(nil) != nil {
		t.Error("stripping nil yields nil")
	}
}

func TestIsReservedHeader(t *testing.T) {
	for _, k := range []string{"bd-caller", "BD-CALLER", "bd-anything"} {
		if !IsReservedHeader(k) {
			t.Errorf("%q is reserved", k)
		}
	}
	for _, k := range []string{"traceparent", "content-type", "bdx-not-reserved"} {
		if IsReservedHeader(k) {
			t.Errorf("%q is not reserved", k)
		}
	}
}

func TestCallerOfReadsWhatTheSubstrateStamped(t *testing.T) {
	m := &Msg{Headers: Headers{
		HeaderCaller:     "files",
		HeaderGeneration: "7",
		HeaderSubject:    "lease-abc",
	}}
	c := CallerOf(m)
	if c.PluginID != "files" || c.Generation != "7" || c.SubjectLease != "lease-abc" {
		t.Fatalf("CallerOf = %+v", c)
	}
	if c.Autonomous() {
		t.Error("a caller with a lease is not autonomous")
	}
	if !CallerOf(&Msg{Headers: Headers{HeaderCaller: "files"}}).Autonomous() {
		t.Error("no lease means autonomous")
	}
	if (CallerOf(nil) != Caller{}) {
		t.Error("CallerOf(nil) is the zero caller, not a panic")
	}
}

// TestRespondRefusesWhenTheMessageIsNotARequest keeps a handler from believing
// it answered. A Msg a caller built itself carries no responder, and silently
// dropping the answer is how a request times out with no explanation.
func TestRespondRefusesWhenTheMessageIsNotARequest(t *testing.T) {
	m := &Msg{Subject: "event.files.changed"}
	err := m.Respond(nil, []byte("hi"))
	var f Fault
	if !errors.As(err, &f) {
		t.Fatalf("Respond returned %v, want a Fault", err)
	}
	if f.Code != FaultUnhandled {
		t.Errorf("Fault.Code = %q, want %q", f.Code, FaultUnhandled)
	}
	if f.Op != "event.files.changed" {
		t.Errorf("the refusal must name the subject, got Op=%q", f.Op)
	}
	if err := (*Msg)(nil).Respond(nil, nil); err == nil {
		t.Error("Respond on a nil Msg refuses rather than panicking")
	}
}

func TestNewMsgRespondsAndLowercases(t *testing.T) {
	var gotHeaders Headers
	var gotData []byte
	m := NewMsg("cmd.files.v1.list", "_INBOX.files.x", Headers{"BD-Corr": "c1", "X-A": "1"}, []byte("req"),
		func(h Headers, d []byte) error { gotHeaders, gotData = h, d; return nil })

	if m.Headers.Get("bd-corr") != "c1" || m.Headers.Get("x-a") != "1" {
		t.Fatalf("NewMsg did not lowercase headers: %v", m.Headers)
	}
	if m.Correlation() != "c1" {
		t.Errorf("Correlation() = %q", m.Correlation())
	}
	if err := m.Respond(Headers{"x-b": "2"}, []byte("resp")); err != nil {
		t.Fatal(err)
	}
	if string(gotData) != "resp" || gotHeaders.Get("x-b") != "2" {
		t.Fatalf("Respond delivered %q %v", gotData, gotHeaders)
	}
}

func TestRespondFaultCarriesTheCode(t *testing.T) {
	var gotHeaders Headers
	m := NewMsg("cmd.files.v1.list", "_INBOX.x", nil, nil,
		func(h Headers, d []byte) error { gotHeaders = h; return nil })
	if err := m.RespondFault(Fault{Code: FaultDenied, Op: "cmd.files.v1.list", Message: "principal files@7"}); err != nil {
		t.Fatal(err)
	}
	if gotHeaders.Get(HeaderFault) != FaultDenied {
		t.Fatalf("bd-fault = %q, want %q", gotHeaders.Get(HeaderFault), FaultDenied)
	}
}

// TestDeniedNamesSubjectAndPrincipal is conformance property
// LoudAttributedRefusal at its source: if the constructor does not put both in
// the text, no substrate built on it can pass that property.
func TestDeniedNamesSubjectAndPrincipal(t *testing.T) {
	err := Denied("event.other.changed", "files@7", "not covered by any publish grant")
	msg := err.Error()
	for _, want := range []string{"event.other.changed", "files@7", "not covered"} {
		if !contains(msg, want) {
			t.Errorf("refusal %q does not name %q", msg, want)
		}
	}
	if err.Code != FaultDenied {
		t.Errorf("Code = %q", err.Code)
	}
}

func TestFaultUnwrapsAndFormats(t *testing.T) {
	inner := errors.New("boom")
	f := Fault{Code: FaultTimeout, Op: "cmd.x.v1.y", Message: "after 5s", Err: inner}
	if f.Error() != "cmd.x.v1.y: timeout: after 5s" {
		t.Errorf("Error() = %q", f.Error())
	}
	if !errors.Is(f, inner) {
		t.Error("Fault must unwrap to its cause")
	}
	if (Fault{Code: FaultDenied}).Error() != "denied" {
		t.Errorf("a bare fault prints just its code, got %q", (Fault{Code: FaultDenied}).Error())
	}
}

func TestUnsupportedNamesTheCapability(t *testing.T) {
	f := Unsupported("Streams().Declare", "durable")
	if f.Code != FaultUnsupported || !contains(f.Error(), "durable") {
		t.Errorf("Unsupported = %q", f.Error())
	}
}
