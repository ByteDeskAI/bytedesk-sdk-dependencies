package consumer

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
)

// host routes Request into a Registrar, so a Call/Handle round trip exercises
// the generated wrappers, the mechanism under them, and one registrar shared
// between packages.
type host struct {
	reg       *plugin.Registrar
	subs      map[string][]func(bus.Envelope)
	published []bus.Envelope
}

func newHost() *host {
	return &host{reg: plugin.NewRegistrar(), subs: map[string][]func(bus.Envelope){}}
}

func (h *host) Publish(env bus.Envelope) error {
	h.published = append(h.published, env)
	for _, fn := range h.subs[env.Type] {
		fn(env)
	}
	return nil
}

func (h *host) Subscribe(eventType string, fn func(bus.Envelope)) func() {
	h.subs[eventType] = append(h.subs[eventType], fn)
	return func() { delete(h.subs, eventType) }
}

func (h *host) Request(ctx context.Context, env bus.Envelope) (bus.Envelope, error) {
	return h.reg.HandleCommand(ctx, env)
}

func (h *host) Logger() plugin.Logger              { return nil }
func (h *host) StateDir(string) string             { return "" }
func (h *host) Every(time.Duration, func()) func() { return func() {} }
func (h *host) BumpContributions()                 {}

var _ plugin.Host = (*host)(nil)

// The generated wrappers work end to end: an author calls consumer.Call and
// consumer.Handle and never sees a descriptor's insides, an envelope or an any.
func TestGeneratedConsumerWrappersRoundTrip(t *testing.T) {
	h := newHost()
	var seen plugin.Caller
	Handle(h.reg, PingCommand, func(_ context.Context, caller plugin.Caller, req Ping) (Pong, error) {
		seen = caller
		return Pong{Note: "pong:" + req.Note, Echo: req}, nil
	})
	if got := h.reg.Handles(); len(got) != 1 || got[0] != "cmd.consumer.v1.ping" {
		t.Fatalf("Handles() = %v", got)
	}
	out, err := Call(context.Background(), h, PingCommand, Ping{Note: "hello", Seq: 1 << 60})
	if err != nil {
		t.Fatal(err)
	}
	if out.Note != "pong:hello" || out.Echo.Seq != 1<<60 {
		t.Fatalf("round trip = %+v; a uint64 above 2^53 must survive as a decimal string", out)
	}
	if !seen.Autonomous() {
		t.Errorf("caller with no subject lease must read as autonomous, got %+v", seen)
	}
}

func TestGeneratedConsumerEventRoundTrip(t *testing.T) {
	h := newHost()
	got := make(chan Pong, 2)
	sub, err := On(h, PongedEvent, func(v Pong) { got <- v })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()
	if err := Emit(h, PongedEvent, Pong{Note: "emitted"}); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v.Note != "emitted" {
			t.Fatalf("event payload = %+v", v)
		}
	default:
		t.Fatal("no event delivered")
	}
	if h.published[0].Headers[plugin.HeaderSchema] == "" {
		t.Fatal("the published event carried no schema header")
	}
	// A different contract revision is dropped rather than mis-decoded.
	_ = h.Publish(bus.Envelope{
		Type:    PongedEvent.Name(),
		Headers: map[string]string{plugin.HeaderSchema: "other"},
		Payload: []byte(`{"note":"stale"}`),
	})
	select {
	case v := <-got:
		t.Fatalf("delivered a mismatched schema: %+v", v)
	default:
	}
}

// The classification guarantee has to be a build failure in the CONSUMER's own
// package, not only in plugin's, or a foreign package could carry an
// unclassified DTO. Forged is the case that matters: it embeds a union member,
// so it satisfies any marker method through promotion, and the union still
// refuses it.
func TestConsumerUnionRefusesUnclassifiedTypes(t *testing.T) {
	out, err := exec.Command("go", "build", "-tags", "compilefail", ".").CombinedOutput()
	if err == nil {
		t.Fatal("negative_forbidden.go compiled; an unclassified or forged payload must not reach a call site")
	}
	for _, want := range []string{
		"Unclassified does not satisfy Payload",
		"Forged does not satisfy Payload",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("compiler output missing %q:\n%s", want, out)
		}
	}
}
