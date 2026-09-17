package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

type ping struct {
	Name string `json:"name"`
}

type pong struct {
	Greeting string `json:"greeting"`
}

const (
	pingHash = "0badc0de"
	evtHash  = "feedface"
)

var (
	cmdGreet = NewCommand[ping, pong]("svc.demo.v1.greet", 3, pingHash, "svc.demo.v1.greet")
	evtWoke  = NewEvent[ping]("evt.demo.v1.woke", 1, evtHash, "evt.demo.v1.woke")
)

// testBus embeds bus.Bus so only the methods a test exercises need bodies. Any
// other call panics, which is the right answer in a test: it names the method
// nobody meant to reach.
type testBus struct {
	bus.Bus

	mu        sync.Mutex
	published []published
	reply     *bus.Msg
	replyErr  error
	subs      map[bus.Pattern]bus.Handler
}

type published struct {
	subject bus.Subject
	data    []byte
	headers bus.Headers
}

func newTestBus() *testBus { return &testBus{subs: map[bus.Pattern]bus.Handler{}} }

func (b *testBus) Publish(_ context.Context, s bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	o := bus.ResolvePublish(opts)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, published{subject: s, data: data, headers: o.Headers})
	return nil
}

func (b *testBus) Request(_ context.Context, _ bus.Subject, _ []byte, _ ...bus.ReqOpt) (*bus.Msg, error) {
	return b.reply, b.replyErr
}

func (b *testBus) Subscribe(_ context.Context, p bus.Pattern, h bus.Handler, _ ...bus.SubOpt) (bus.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[p] = h
	return testSub{}, nil
}

type testSub struct{}

func (testSub) Cancel()                     {}
func (testSub) Drain(context.Context) error { return nil }
func (testSub) Done() <-chan struct{}       { return nil }
func (testSub) Err() error                  { return nil }

func TestDescriptorCarriesTheGeneratedIdentity(t *testing.T) {
	d := cmdGreet.Descriptor()
	if d.Name() != "svc.demo.v1.greet" || d.Rev() != 3 || d.SchemaHash() != pingHash {
		t.Errorf("descriptor = %q %d %q", d.Name(), d.Rev(), d.SchemaHash())
	}
	if d.Kind() != KindCommand {
		t.Errorf("Kind() = %q, want %q", d.Kind(), KindCommand)
	}
	if d.Subject() != bus.Subject("svc.demo.v1.greet") {
		t.Errorf("Subject() = %q", d.Subject())
	}
	if evtWoke.Descriptor().Kind() != KindEvent {
		t.Error("an event descriptor does not report KindEvent")
	}
	svc := NewServiceDescriptor("svc.demo", 2, "e7a1b2c3", "svc.demo.>")
	if d := svc.Descriptor(); d.SchemaHash() != "e7a1b2c3" || d.Kind() != KindService {
		t.Errorf("service descriptor = %q %q, want the hash it was built with", d.SchemaHash(), d.Kind())
	}
	// A service version is the semver a manifest ServiceDecl and a discovered
	// ServiceInfo both spell. "v2" would match neither.
	if got := svc.Version(); got != "2.0.0" {
		t.Errorf("service Version() = %q, want 2.0.0", got)
	}
}

func TestCallDecodesAMatchingReply(t *testing.T) {
	b := newTestBus()
	b.reply = &bus.Msg{
		Headers: bus.Headers{bus.HeaderSchema: pingHash},
		Data:    []byte(`{"greeting":"hi ryan"}`),
	}
	got, err := Call(context.Background(), b, cmdGreet, ping{Name: "ryan"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.Greeting != "hi ryan" {
		t.Errorf("Call = %+v", got)
	}
}

// TestCallRefusesAMismatchedSchemaBeforeDecoding is the guarantee that matters:
// a payload from another revision is never decoded, however well it happens to
// fit the struct in front of it.
func TestCallRefusesAMismatchedSchemaBeforeDecoding(t *testing.T) {
	b := newTestBus()
	b.reply = &bus.Msg{
		Headers: bus.Headers{bus.HeaderSchema: "some-other-revision"},
		Data:    []byte(`{"greeting":"decodes perfectly well"}`),
	}
	_, err := Call(context.Background(), b, cmdGreet, ping{})
	var fault bus.Fault
	if !errors.As(err, &fault) || fault.Code != bus.FaultSchema {
		t.Fatalf("Call = %v, want FaultSchema", err)
	}
	if fault.Op != cmdGreet.Descriptor().Name() {
		t.Errorf("refusal is unattributed: %+v", fault)
	}
}

// TestCallTypesAServiceErrorReply also pins the ORDER of the two checks. An
// error reply carries a fault code and an operator message, not a payload of
// the declared type, so it has no schema hash to stamp. Now that an absent
// stamp is a refusal, checking the schema first would turn every refusal into
// FaultSchema and lose the reason the call actually failed.
func TestCallTypesAServiceErrorReply(t *testing.T) {
	b := newTestBus()
	b.reply = &bus.Msg{
		Headers: bus.Headers{bus.HeaderFault: bus.FaultDenied},
		Data:    []byte("principal files@g1: subject not granted"),
	}
	_, err := Call(context.Background(), b, cmdGreet, ping{})
	var fault bus.Fault
	if !errors.As(err, &fault) || fault.Code != bus.FaultDenied {
		t.Fatalf("Call = %v, want a typed FaultDenied", err)
	}
}

func TestEmitAndOnRoundTripThroughTheBus(t *testing.T) {
	b := newTestBus()
	if err := Emit(context.Background(), b, evtWoke, ping{Name: "ryan"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(b.published) != 1 || b.published[0].subject != "evt.demo.v1.woke" {
		t.Fatalf("published = %+v", b.published)
	}

	type got struct {
		v      ping
		caller bus.Caller
	}
	seen := make(chan got, 1)
	if _, err := On(context.Background(), b, evtWoke, func(_ context.Context, v ping, c bus.Caller) {
		seen <- got{v: v, caller: c}
	}); err != nil {
		t.Fatalf("On: %v", err)
	}

	h := b.subs["evt.demo.v1.woke"]
	if h == nil {
		t.Fatal("On did not subscribe to the event's subject")
	}
	h(context.Background(), bus.NewMsg("evt.demo.v1.woke", "", bus.Headers{
		bus.HeaderSchema:     evtHash,
		bus.HeaderCaller:     "demo",
		bus.HeaderGeneration: "g1",
	}, []byte(`{"name":"ryan"}`), nil))

	select {
	case g := <-seen:
		if g.v.Name != "ryan" {
			t.Errorf("handler saw %+v", g.v)
		}
		if g.caller.PluginID != "demo" || g.caller.Generation != "g1" {
			t.Errorf("handler saw caller %+v", g.caller)
		}
		if !g.caller.Autonomous() {
			t.Error("a message with no bd-subject is not reported as autonomous")
		}
	default:
		t.Fatal("the handler was not called")
	}

	// A message from another revision is dropped, not delivered.
	h(context.Background(), bus.NewMsg("evt.demo.v1.woke", "", bus.Headers{bus.HeaderSchema: "other"}, []byte(`{"name":"nope"}`), nil))
	select {
	case g := <-seen:
		t.Fatalf("a mismatched-schema message was delivered: %+v", g.v)
	default:
	}

	// So is one that stamps nothing at all. This is the case the old lenient
	// check let through, and it is the one a misconfigured peer produces.
	h(context.Background(), bus.NewMsg("evt.demo.v1.woke", "", bus.Headers{bus.HeaderCaller: "demo"}, []byte(`{"name":"nope"}`), nil))
	select {
	case g := <-seen:
		t.Fatalf("an unstamped message was delivered: %+v", g.v)
	default:
	}
}

// TestSchemaStampSurvivesTheStrip is the opposite of what this test used to
// assert, and the reason the check is worth having.
//
// bd- is reserved to the host, so bus.WithHeaders strips the whole namespace
// from anything a principal supplies — that is still true, and still what stops
// a caller forging bd-caller. bus.WithSchema writes bd-schema after and
// independently of that strip, so the typed layer's stamp now reaches the
// substrate while a hand-supplied one does not.
func TestSchemaStampSurvivesTheStrip(t *testing.T) {
	b := newTestBus()
	if err := Emit(context.Background(), b, evtWoke, ping{}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if got := b.published[0].headers.Get(bus.HeaderSchema); got != evtHash {
		t.Fatalf("bd-schema = %q, want %q: the stamp did not survive egress", got, evtHash)
	}

	// The strip it survives is still doing its job on everything a caller
	// supplies itself. A forged schema hash only fails the forger's own call;
	// a forged caller would buy authority, which is why there is no option for
	// it and never should be.
	_ = b.Publish(context.Background(), "evt.demo.v1.woke", nil,
		bus.WithHeaders(bus.Headers{bus.HeaderCaller: "not-me", bus.HeaderGeneration: "g9"}))
	forged := b.published[1].headers
	if forged.Get(bus.HeaderCaller) != "" || forged.Get(bus.HeaderGeneration) != "" {
		t.Errorf("a caller-supplied identity header survived the strip: %+v", forged)
	}
}

// tripwire records whether it was ever unmarshalled. It is how the tests below
// prove the schema check happens BEFORE decoding rather than merely instead of
// returning the decoded value: a payload that would decode perfectly well must
// still never be handed to the decoder.
type tripwire struct {
	Greeting string `json:"greeting"`
	Name     string `json:"name"`
}

var decoded atomic.Bool

func (w *tripwire) UnmarshalJSON(b []byte) error {
	decoded.Store(true)
	type raw tripwire
	return json.Unmarshal(b, (*raw)(w))
}

var cmdTripwire = NewCommand[tripwire, tripwire]("svc.demo.v1.tripwire", 1, pingHash, "svc.demo.v1.tripwire")

// TestCallRefusesAnUnstampedReplyBeforeDecoding is the case the old lenient
// check could never catch: a peer that stamps nothing at all. The payload here
// decodes perfectly; the point is that it is never given the chance.
func TestCallRefusesAnUnstampedReplyBeforeDecoding(t *testing.T) {
	b := newTestBus()
	b.reply = &bus.Msg{Data: []byte(`{"greeting":"decodes perfectly well"}`)} // no bd-schema

	decoded.Store(false)
	_, err := Call(context.Background(), b, cmdTripwire, tripwire{})
	var fault bus.Fault
	if !errors.As(err, &fault) || fault.Code != bus.FaultSchema {
		t.Fatalf("Call = %v, want FaultSchema for an unstamped reply", err)
	}
	if decoded.Load() {
		t.Fatal("the payload was unmarshalled before the schema check refused it")
	}
	if fault.Op != "svc.demo.v1.tripwire" || !contains(fault.Message, bus.HeaderSchema) {
		t.Errorf("refusal does not name the descriptor and the missing header: %+v", fault)
	}

	// A mismatched stamp is refused before decoding too, and says so
	// differently: one peer never stamped, the other speaks another revision.
	b.reply = &bus.Msg{Headers: bus.Headers{bus.HeaderSchema: "another-revision"}, Data: []byte(`{"greeting":"also fine"}`)}
	decoded.Store(false)
	_, err = Call(context.Background(), b, cmdTripwire, tripwire{})
	if !errors.As(err, &fault) || fault.Code != bus.FaultSchema {
		t.Fatalf("Call = %v, want FaultSchema for a mismatched reply", err)
	}
	if decoded.Load() {
		t.Fatal("a mismatched payload was unmarshalled")
	}

	// And the matching case still decodes, so the check is not simply refusing
	// everything.
	b.reply = &bus.Msg{Headers: bus.Headers{bus.HeaderSchema: pingHash}, Data: []byte(`{"greeting":"hi"}`)}
	decoded.Store(false)
	got, err := Call(context.Background(), b, cmdTripwire, tripwire{})
	if err != nil {
		t.Fatalf("Call with a matching stamp: %v", err)
	}
	if !decoded.Load() || got.Greeting != "hi" {
		t.Errorf("a matching reply did not decode: %+v", got)
	}
}

// TestServeRefusesAnUnstampedRequestBeforeDecoding is the same guarantee on the
// serving side, where it matters more: the handler must never see a request
// from a revision it does not speak.
func TestServeRefusesAnUnstampedRequestBeforeDecoding(t *testing.T) {
	svc := &captureServices{}
	b := &serviceBus{services: svc}
	handled := false
	if _, err := Serve(context.Background(), b, cmdTripwire, func(_ context.Context, req tripwire, _ bus.Caller) (tripwire, error) {
		handled = true
		return tripwire{Greeting: "hi " + req.Name}, nil
	}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	handler := svc.spec.Endpoints[0].Handler

	answer := make(chan *bus.Msg, 1)
	respond := func(h bus.Headers, data []byte) error {
		answer <- &bus.Msg{Headers: h, Data: data}
		return nil
	}

	decoded.Store(false)
	handler(context.Background(), bus.NewMsg("svc.demo.v1.tripwire", "_INBOX.1", nil, []byte(`{"name":"ryan"}`), respond))
	refusal := <-answer
	if refusal.Headers.Get(bus.HeaderFault) != bus.FaultSchema {
		t.Fatalf("an unstamped request was not refused with bd-fault: %+v", refusal.Headers)
	}
	if decoded.Load() {
		t.Fatal("an unstamped request was unmarshalled before being refused")
	}
	if handled {
		t.Fatal("the handler ran for an unstamped request")
	}

	decoded.Store(false)
	handler(context.Background(), bus.NewMsg("svc.demo.v1.tripwire", "_INBOX.2",
		bus.Headers{bus.HeaderSchema: pingHash}, []byte(`{"name":"ryan"}`), respond))
	reply := <-answer
	if !handled || !decoded.Load() {
		t.Fatal("a correctly stamped request did not reach the handler")
	}
	if reply.Headers.Get(bus.HeaderSchema) != pingHash {
		t.Errorf("the reply carries no schema stamp: %+v", reply.Headers)
	}
}

func TestRegistrarRefusesEndpointsOutsideServes(t *testing.T) {
	r := NewRegistrar([]bus.Pattern{"svc.demo.>"})
	ok := bus.EndpointSpec{Name: "greet", Subject: "svc.demo.v1.greet", Handler: func(context.Context, *bus.Msg) {}}
	if err := r.Mount(ok); err != nil {
		t.Fatalf("Mount of a covered subject: %v", err)
	}

	outside := bus.EndpointSpec{Name: "steal", Subject: "svc.files.v1.delete", Handler: func(context.Context, *bus.Msg) {}}
	err := r.Mount(outside)
	var fault bus.Fault
	if !errors.As(err, &fault) || fault.Code != bus.FaultDenied {
		t.Fatalf("Mount outside serves = %v, want FaultDenied", err)
	}
	// Naming the subject is the requirement: an author with twenty endpoints
	// needs to know WHICH one was refused.
	if fault.Op != "svc.files.v1.delete" || !contains(fault.Message, "svc.files.v1.delete") {
		t.Errorf("refusal does not name the subject: %+v", fault)
	}

	if err := r.Mount(ok); !errors.As(err, &fault) || fault.Code != bus.FaultConflict {
		t.Errorf("duplicate Mount = %v, want FaultConflict", err)
	}
	if got := r.Endpoints(); len(got) != 1 {
		t.Errorf("Endpoints() = %d entries, want 1", len(got))
	}

	if err := r.Mount(bus.EndpointSpec{Name: "nosubject", Handler: func(context.Context, *bus.Msg) {}}); err == nil {
		t.Error("an endpoint with no subject was mounted")
	}
	if err := r.Mount(bus.EndpointSpec{Name: "nohandler", Subject: "svc.demo.v1.other"}); err == nil {
		t.Error("an endpoint with no handler was mounted")
	}

	// A caller cannot widen its own containment after construction.
	serves := []bus.Pattern{"svc.demo.>"}
	r2 := NewRegistrar(serves)
	serves[0] = "svc.>"
	if err := r2.Mount(outside); err == nil {
		t.Error("mutating the serves slice widened the registrar")
	}
}

func TestTypedHelpersRefuseANilBus(t *testing.T) {
	ctx := context.Background()
	var fault bus.Fault
	if _, err := Call(ctx, nil, cmdGreet, ping{}); !errors.As(err, &fault) || fault.Code != bus.FaultUnavailable {
		t.Errorf("Call with no bus = %v", err)
	}
	if err := Emit(ctx, nil, evtWoke, ping{}); !errors.As(err, &fault) || fault.Code != bus.FaultUnavailable {
		t.Errorf("Emit with no bus = %v", err)
	}
	if _, err := On(ctx, nil, evtWoke, func(context.Context, ping, bus.Caller) {}); !errors.As(err, &fault) {
		t.Errorf("On with no bus = %v", err)
	}
	if _, err := On(ctx, newTestBus(), evtWoke, nil); !errors.As(err, &fault) {
		t.Errorf("On with no handler = %v", err)
	}
	if _, err := Serve(ctx, nil, cmdGreet, func(context.Context, ping, bus.Caller) (pong, error) { return pong{}, nil }); !errors.As(err, &fault) {
		t.Errorf("Serve with no bus = %v", err)
	}
	if _, err := OpenBucket(ctx, nil, NewBucketDescriptor[ping]("demo", 1, pingHash)); !errors.As(err, &fault) {
		t.Errorf("OpenBucket with no bus = %v", err)
	}
	if _, err := Stream(nil, NewStreamDescriptor[ping]("demo", 1, pingHash, "str.demo.v1")).Append(ctx, ping{}); !errors.As(err, &fault) {
		t.Errorf("Append with no bus = %v", err)
	}
	var zero TypedBucket[ping]
	if _, _, err := zero.Get(ctx, "k"); !errors.As(err, &fault) {
		t.Errorf("Get on an unopened bucket = %v", err)
	}
}

// TestServeAnswersThroughTheEndpointHandler drives the handler Serve mounts,
// which is where decode, schema check and encode actually live.
func TestServeAnswersThroughTheEndpointHandler(t *testing.T) {
	svc := &captureServices{}
	b := &serviceBus{services: svc}
	if _, err := Serve(context.Background(), b, cmdGreet, func(_ context.Context, req ping, _ bus.Caller) (pong, error) {
		return pong{Greeting: "hi " + req.Name}, nil
	}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if svc.spec.Name != "svc.demo.v1.greet" || svc.spec.Version != "3.0.0" {
		t.Fatalf("ServiceSpec = %+v", svc.spec)
	}
	// Serve and ServiceDescriptor.Version must render a revision identically,
	// or a service mounted through one disagrees with a descriptor built by the
	// other about what version is live.
	if want := NewServiceDescriptor("svc.demo.v1.greet", 3, pingHash, "svc.demo.v1.greet").Version(); svc.spec.Version != want {
		t.Errorf("Serve advertised %q, ServiceDescriptor.Version says %q", svc.spec.Version, want)
	}
	if len(svc.spec.Endpoints) != 1 || svc.spec.Endpoints[0].Subject != "svc.demo.v1.greet" {
		t.Fatalf("endpoints = %+v", svc.spec.Endpoints)
	}

	answer := make(chan *bus.Msg, 1)
	respond := func(h bus.Headers, data []byte) error {
		answer <- &bus.Msg{Headers: h, Data: data}
		return nil
	}

	svc.spec.Endpoints[0].Handler(context.Background(), bus.NewMsg(
		"svc.demo.v1.greet", "_INBOX.1",
		bus.Headers{bus.HeaderSchema: pingHash, bus.HeaderCaller: "peer"},
		[]byte(`{"name":"ryan"}`), respond))
	reply := <-answer
	if reply.Headers.Get(bus.HeaderSchema) != pingHash {
		t.Errorf("the reply carries no schema stamp: %+v", reply.Headers)
	}
	var got pong
	if err := json.Unmarshal(reply.Data, &got); err != nil || got.Greeting != "hi ryan" {
		t.Errorf("reply = %s (%v)", reply.Data, err)
	}

	// A request from another revision is refused with bd-fault, not decoded.
	svc.spec.Endpoints[0].Handler(context.Background(), bus.NewMsg(
		"svc.demo.v1.greet", "_INBOX.2",
		bus.Headers{bus.HeaderSchema: "other"},
		[]byte(`{"name":"ryan"}`), respond))
	refusal := <-answer
	if refusal.Headers.Get(bus.HeaderFault) != bus.FaultSchema {
		t.Errorf("a mismatched request was not refused with bd-fault: %+v", refusal.Headers)
	}
}

type serviceBus struct {
	bus.Bus
	services bus.Services
}

func (b *serviceBus) Services() bus.Services { return b.services }

type captureServices struct {
	bus.Services
	spec bus.ServiceSpec
}

func (s *captureServices) Serve(_ context.Context, spec bus.ServiceSpec) (bus.Service, error) {
	s.spec = spec
	return nil, nil
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
