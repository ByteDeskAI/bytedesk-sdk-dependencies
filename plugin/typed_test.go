package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// testHost routes Request into a Registrar, so a Call/Handle round trip
// exercises both halves of the typed layer against the real descriptor.
type testHost struct {
	reg         *Registrar
	err         error
	reply       *bus.Envelope
	subs        map[string][]func(bus.Envelope)
	published   []bus.Envelope
	lastRequest bus.Envelope
}

func newTestHost() *testHost {
	return &testHost{reg: NewRegistrar(), subs: map[string][]func(bus.Envelope){}}
}

func (h *testHost) Publish(env bus.Envelope) error {
	h.published = append(h.published, env)
	for _, fn := range h.subs[env.Type] {
		fn(env)
	}
	return nil
}

func (h *testHost) Subscribe(eventType string, fn func(bus.Envelope)) func() {
	h.subs[eventType] = append(h.subs[eventType], fn)
	return func() { delete(h.subs, eventType) }
}

func (h *testHost) Request(ctx context.Context, env bus.Envelope) (bus.Envelope, error) {
	h.lastRequest = env
	if h.err != nil {
		return bus.Envelope{}, h.err
	}
	if h.reply != nil {
		return *h.reply, nil
	}
	return h.reg.HandleCommand(ctx, env)
}

func (h *testHost) Logger() Logger                     { return nil }
func (h *testHost) StateDir(string) string             { return "" }
func (h *testHost) Every(time.Duration, func()) func() { return func() {} }
func (h *testHost) BumpContributions()                 {}

var _ Host = (*testHost)(nil)

// statusHost also implements the optional StatusSubscriber, which is how a new
// host reports the two subscription failures a bare cancel hides.
type statusHost struct {
	*testHost
	sub *testSubscription
}

type testSubscription struct {
	done chan struct{}
	err  error
}

func (s *testSubscription) Cancel()               { close(s.done) }
func (s *testSubscription) Done() <-chan struct{} { return s.done }
func (s *testSubscription) Err() error            { return s.err }
func (h *statusHost) SubscribeStatus(eventType string, fn func(bus.Envelope)) (Subscription, error) {
	h.testHost.Subscribe(eventType, fn)
	return h.sub, nil
}

var _ StatusSubscriber = (*statusHost)(nil)

func sampleRequest() PresentationRequest {
	return PresentationRequest{
		Lease: PresentationLease{
			HostEpoch: "e1", PluginID: "p", ProviderID: "pr", Generation: "g",
			SubjectLease: "opaque-id", RequestID: "r1", ViewRevision: "v1",
		},
		Terminals: []PresentationTerminal{{TerminalID: "t1", Context: TerminalBindingContext{Kind: TerminalBindingNone}}},
	}
}

func TestTypedCallAndHandleRoundTrip(t *testing.T) {
	host := newTestHost()
	var seen Caller
	Handle(host.reg, CmdTerminalPresentationProject, func(_ context.Context, caller Caller, req PresentationRequest) (PresentationResult, error) {
		seen = caller
		return PresentationResult{Lease: req.Lease, MaxAgeMS: 1000, Items: []PresentationItem{}}, nil
	})
	if got := host.reg.Handles(); len(got) != 1 || got[0] != CmdTerminalPresentationProject.Name() {
		t.Fatalf("Handles() = %v", got)
	}
	out, err := Call(context.Background(), host, CmdTerminalPresentationProject, sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if out.MaxAgeMS != 1000 || out.Lease.RequestID != "r1" {
		t.Fatalf("round trip lost the payload: %+v", out)
	}
	if !seen.Autonomous() {
		t.Errorf("caller with no subject lease must read as autonomous, got %+v", seen)
	}
}

// The schema hash is checked before the payload is unmarshalled, so a
// mismatched payload is never decoded.
func TestSchemaMismatchFailsBeforeDecode(t *testing.T) {
	host := newTestHost()
	decoded := false
	Handle(host.reg, CmdTerminalPresentationProject, func(context.Context, Caller, PresentationRequest) (PresentationResult, error) {
		decoded = true
		return PresentationResult{}, nil
	})
	_, err := host.reg.HandleCommand(context.Background(), bus.Envelope{
		Type:    CmdTerminalPresentationProject.Name(),
		Headers: map[string]string{HeaderSchema: "0000"},
		Payload: json.RawMessage(`{"lease":{}}`),
	})
	var fault Fault
	if !errors.As(err, &fault) || fault.Code != FaultSchema {
		t.Fatalf("err = %v, want a schema fault", err)
	}
	if decoded {
		t.Error("handler ran on a mismatched schema; the check must precede decode")
	}
}

func TestCallerComesFromHostStampedHeaders(t *testing.T) {
	caller := CallerOf(bus.Envelope{
		Source: "fallback",
		Headers: map[string]string{
			HeaderCaller: "agent-messaging", HeaderGeneration: "7", HeaderSubject: "lease-9",
		},
	})
	if caller.PluginID != "agent-messaging" || caller.Generation != "7" || caller.SubjectLease != "lease-9" {
		t.Fatalf("caller = %+v", caller)
	}
	if caller.Autonomous() {
		t.Error("a call carrying a subject lease is not autonomous")
	}
	if got := CallerOf(bus.Envelope{Source: "fallback"}); got.PluginID != "fallback" || !got.Autonomous() {
		t.Fatalf("unmigrated host fallback = %+v", got)
	}
}

func TestUnhandledCommandIsATypedFault(t *testing.T) {
	var fault Fault
	_, err := NewRegistrar().HandleCommand(context.Background(), bus.Envelope{Type: "cmd.absent.v1.do"})
	if !errors.As(err, &fault) || fault.Code != FaultUnhandled {
		t.Fatalf("err = %v, want an unhandled fault", err)
	}
}

func TestDuplicateHandlerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering two handlers for one operation must panic")
		}
	}()
	r := NewRegistrar()
	fn := func(context.Context, Caller, PresentationRequest) (PresentationResult, error) {
		return PresentationResult{}, nil
	}
	Handle(r, CmdTerminalPresentationProject, fn)
	Handle(r, CmdTerminalPresentationProject, fn)
}

func TestTransportErrorsBecomeTypedFaults(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{"host /request: 403 Forbidden", FaultDenied},
		{"host /request: 429 Too Many Requests", FaultBudget},
		{"host /request: 404 Not Found", FaultWithdrawn},
		{"context deadline exceeded", FaultTimeout},
		{"dial unix: no such file", FaultUnavailable},
	} {
		t.Run(tc.want, func(t *testing.T) {
			host := newTestHost()
			host.err = errors.New(tc.text)
			_, err := Call(context.Background(), host, CmdTerminalPresentationProject, sampleRequest())
			var fault Fault
			if !errors.As(err, &fault) || fault.Code != tc.want {
				t.Fatalf("Call error = %v, want code %s", err, tc.want)
			}
		})
	}
}

var evtSnapshot = NewEvent[RuntimeSnapshot]("event.test.snapshot.v1", 1, "test-hash")

func TestEmitAndOnRoundTripAndFilterBySchema(t *testing.T) {
	host := newTestHost()
	got := make(chan RuntimeSnapshot, 4)
	sub, err := On(host, evtSnapshot, func(v RuntimeSnapshot) { got <- v })
	if err != nil {
		t.Fatal(err)
	}
	if err := Emit(host, evtSnapshot, RuntimeSnapshot{Epoch: "e", Revision: 42}); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v.Revision != 42 {
			t.Fatalf("event payload = %+v", v)
		}
	default:
		t.Fatal("no event delivered")
	}
	if h := host.published[0].Headers[HeaderSchema]; h != "test-hash" {
		t.Fatalf("published schema header = %q", h)
	}
	// A different contract revision is dropped rather than mis-decoded.
	_ = host.Publish(bus.Envelope{Type: evtSnapshot.Name(), Headers: map[string]string{HeaderSchema: "other"}, Payload: json.RawMessage(`{"epoch":"x"}`)})
	select {
	case v := <-got:
		t.Fatalf("delivered a mismatched schema: %+v", v)
	default:
	}
	// The legacy fallback cannot report loss: Done never closes, Err is nil.
	if sub.Err() != nil {
		t.Errorf("legacy subscription Err = %v, want nil", sub.Err())
	}
	select {
	case <-sub.Done():
		t.Error("legacy subscription Done must not close")
	default:
	}
	sub.Cancel()
}

func TestOnPrefersStatusSubscriberSoLossIsVisible(t *testing.T) {
	host := &statusHost{testHost: newTestHost(), sub: &testSubscription{done: make(chan struct{}), err: fmt.Errorf("registration refused past the 32 per-generation limit")}}
	sub, err := On(host, evtSnapshot, func(RuntimeSnapshot) {})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Err() == nil {
		t.Fatal("a StatusSubscriber host must be able to report why a subscription does not exist")
	}
}

// Invoke is the marshalling boundary a generated wrapper calls, and it stamps
// the schema header the host checks before it decodes.
func TestInvokeIsTheMarshallingBoundary(t *testing.T) {
	host := newTestHost()
	host.reply = &bus.Envelope{Payload: json.RawMessage(`{"ok":true}`)}
	var out struct {
		OK bool `json:"ok"`
	}
	d := NewDescriptor("cmd.messaging.v1.message.send", 1, "hash-1")
	if err := Invoke(context.Background(), host, d, map[string]string{"to": "peer"}, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("Invoke did not decode the reply: %+v", out)
	}
	if got := host.lastRequest.Headers[HeaderSchema]; got != "hash-1" {
		t.Errorf("request schema header = %q, want hash-1", got)
	}
	if host.lastRequest.Type != d.Name() {
		t.Errorf("request type = %q, want %q", host.lastRequest.Type, d.Name())
	}
}

// The regression guard for the whole schema-verification class. NewDescriptor
// is exported and does not refuse an empty hash, so both a hand-built unhashed
// descriptor and one whose hash does not match are constructible. Neither may
// pass verification against a host that stamps the header, in any of the three
// directions.
//
// A descriptor that DOES match can still talk to an unmigrated host that stamps
// nothing — there is nothing to verify against there, and that is the only case
// the got != "" half of the guard exists for.
func TestDescriptorsCannotSkipSchemaVerification(t *testing.T) {
	stamped := map[string]string{HeaderSchema: "hash-1"}
	for _, tc := range []struct {
		name string
		d    Descriptor
	}{
		{"unhashed", NewDescriptor("cmd.legacy.v1.do", 0, "")},
		{"mismatched", NewDescriptor("cmd.legacy.v1.do", 1, "hash-A")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := newTestHost()
			var fault Fault

			// Calling: a schema-stamped reply must not be decoded.
			host.reply = &bus.Envelope{Headers: stamped, Payload: json.RawMessage(`{"ok":true}`)}
			var out struct {
				OK bool `json:"ok"`
			}
			if err := Invoke(context.Background(), host, tc.d, nil, &out); !errors.As(err, &fault) || fault.Code != FaultSchema {
				t.Fatalf("decoded a reply it cannot verify: err = %v", err)
			}

			// Serving: a schema-stamped request must not reach the handler.
			served := false
			HandleRaw(host.reg, tc.d, func(context.Context, Caller, json.RawMessage) (json.RawMessage, error) {
				served = true
				return json.RawMessage(`{}`), nil
			})
			_, err := host.reg.HandleCommand(context.Background(), bus.Envelope{
				Type: tc.d.Name(), Headers: stamped, Payload: json.RawMessage(`{}`),
			})
			if !errors.As(err, &fault) || fault.Code != FaultSchema {
				t.Fatalf("handler accepted a request it cannot verify: err = %v", err)
			}
			if served {
				t.Error("handler ran before the schema check")
			}

			// Observing: a schema-stamped event must not be delivered.
			delivered := 0
			sub, err := Observe(host, tc.d, func(json.RawMessage) { delivered++ })
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Cancel()
			_ = host.Publish(bus.Envelope{Type: tc.d.Name(), Headers: stamped, Payload: json.RawMessage(`{}`)})
			if delivered != 0 {
				t.Error("delivered an event carrying a schema this subscription cannot verify")
			}
		})
	}
}

// The mechanism and the typed API are one code path, not two: a handler
// registered through HandleRaw answers a typed Call for the same operation
// identity, which is what lets one registrar serve plugin's own contracts and a
// consumer package's generated wrappers side by side.
func TestMechanismAndTypedAPIShareOneCodePath(t *testing.T) {
	host := newTestHost()
	cmd := CmdTerminalPresentationProject
	// The literal is the published identity of this operation, from
	// typescript/schemas.json. A descriptor exposes neither its revision nor
	// its hash, so this is the only way to name the same operation twice — and
	// it pins the identity: if the schema drifts without a revision bump, the
	// schema check fails here and this test says so.
	d := NewDescriptor(cmd.Name(), 2, "3b8d09c15b513b8f7c7bd71dd8574c0f3c35fd029d8c99e549295845329ae1b0")
	if d.Name() != cmd.Name() {
		t.Fatalf("Descriptor.Name() = %q", d.Name())
	}
	HandleRaw(host.reg, d, func(_ context.Context, _ Caller, raw json.RawMessage) (json.RawMessage, error) {
		var req PresentationRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		return json.Marshal(PresentationResult{Lease: req.Lease, MaxAgeMS: 250, Items: []PresentationItem{}})
	})
	out, err := Call(context.Background(), host, cmd, sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if out.MaxAgeMS != 250 || out.Lease.RequestID != "r1" {
		t.Fatalf("round trip = %+v", out)
	}
}
