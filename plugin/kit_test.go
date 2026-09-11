package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// kitHost is a Host that counts negotiations and answers every call with the
// same verdict, so a test can tell what the host decided from what the Kit said.
type kitHost struct {
	negotiations int
	publishes    []string
	refuse       bool
}

var errKitHostRefused = errors.New("host refused")

func (h *kitHost) Negotiate(context.Context, ProtocolRequirements) (HostCapabilities, error) {
	h.negotiations++
	return HostCapabilities{}, nil
}
func (h *kitHost) Publish(env bus.Envelope) error {
	h.publishes = append(h.publishes, env.Type)
	if h.refuse {
		return errKitHostRefused
	}
	return nil
}
func (h *kitHost) Subscribe(string, func(bus.Envelope)) func() { return func() {} }
func (h *kitHost) Request(context.Context, bus.Envelope) (bus.Envelope, error) {
	return bus.Envelope{}, nil
}
func (h *kitHost) Logger() Logger                              { return nil }
func (h *kitHost) StateDir(string) string                      { return "" }
func (h *kitHost) Every(time.Duration, func()) (cancel func()) { return func() {} }
func (h *kitHost) BumpContributions()                          {}

func negotiatedCaps() HostCapabilities {
	return HostCapabilities{
		Major: 1, PluginID: "kit-probe", Generation: "g1",
		Features: []string{"ui.mount.v1"},
		Grants: Permissions{
			Publish:   []string{"event.kitprobe.v1.changed"},
			Subscribe: []string{"event.other.v1.changed"},
			Request:   []string{"cmd.other.v1.read"},
		},
	}
}

// TestNewKitFromDoesNotNegotiate covers TM-266 AC1: a Kit is built from what was
// already negotiated, and costs the host no second negotiation.
func TestNewKitFromDoesNotNegotiate(t *testing.T) {
	host := &kitHost{}
	kit := NewKitFrom(host, negotiatedCaps())
	if host.negotiations != 0 {
		t.Fatalf("NewKitFrom negotiated %d time(s); it must use the capabilities it was given", host.negotiations)
	}
	if kit.Host() != Host(host) {
		t.Fatal("Kit.Host is not the host it was built from")
	}
	if got := kit.Capabilities(); got.PluginID != "kit-probe" || got.Generation != "g1" {
		t.Fatalf("capabilities = %+v", got)
	}
	_ = kit.Can(GrantPublish, "event.kitprobe.v1.changed")
	if host.negotiations != 0 {
		t.Fatal("Can negotiated; it must read the local cache")
	}
}

// TestKitCanIsACacheTheHostStillEnforces covers TM-266 AC2: Can answers from the
// negotiated grants, and nothing it says changes what the host decides.
func TestKitCanIsACacheTheHostStillEnforces(t *testing.T) {
	refusing := &kitHost{refuse: true}
	kit := NewKitFrom(refusing, negotiatedCaps())
	for _, tc := range []struct {
		kind GrantKind
		op   string
		want bool
	}{
		{GrantPublish, "event.kitprobe.v1.changed", true},
		{GrantSubscribe, "event.other.v1.changed", true},
		{GrantRequest, "cmd.other.v1.read", true},
		{GrantPublish, "event.other.v1.changed", false}, // granted for subscribe, not publish
		{GrantRequest, "cmd.other.v1.write", false},
		{GrantPublish, "", false},
		{GrantKind("delete"), "event.kitprobe.v1.changed", false},
	} {
		if got := kit.Can(tc.kind, tc.op); got != tc.want {
			t.Errorf("Can(%s, %q) = %v, want %v", tc.kind, tc.op, got, tc.want)
		}
	}

	// Can says yes, the host says no: the host wins, and the Kit did not intercept.
	if err := kit.Host().Publish(bus.Envelope{Type: "event.kitprobe.v1.changed"}); !errors.Is(err, errKitHostRefused) {
		t.Fatalf("publish through a refusing host = %v; the host must still enforce", err)
	}

	// A fabricated cache claims everything; a permitting host is still the one
	// that answered, and the call reached it exactly as it would without a Kit.
	fabricated := negotiatedCaps()
	fabricated.Grants.Publish = append(fabricated.Grants.Publish, "event.anything.v1.x")
	permitting := &kitHost{}
	forged := NewKitFrom(permitting, fabricated)
	if !forged.Can(GrantPublish, "event.anything.v1.x") {
		t.Fatal("Can did not read the cache it was given")
	}
	if err := forged.Host().Publish(bus.Envelope{Type: "event.anything.v1.x"}); err != nil || len(permitting.publishes) != 1 {
		t.Fatalf("publish = %v, reached host %d time(s); the Kit must pass calls straight through", err, len(permitting.publishes))
	}
}

// TestKitCopiesTheCapabilitiesItIsGiven keeps a caller's later edits from changing
// what an existing Kit reports.
func TestKitCopiesTheCapabilitiesItIsGiven(t *testing.T) {
	caps := negotiatedCaps()
	kit := NewKitFrom(&kitHost{}, caps)
	caps.Grants.Publish[0] = "event.hijacked.v1.x"
	caps.Features[0] = "hijacked"
	if !kit.Can(GrantPublish, "event.kitprobe.v1.changed") || kit.Can(GrantPublish, "event.hijacked.v1.x") {
		t.Fatal("changing the caller's capabilities changed the Kit")
	}
	got := kit.Capabilities()
	got.Grants.Request[0] = "cmd.hijacked.v1.x"
	if !kit.Can(GrantRequest, "cmd.other.v1.read") {
		t.Fatal("changing the returned capabilities changed the Kit")
	}
}

func TestNilKitIsSafe(t *testing.T) {
	var kit *Kit
	if kit.Host() != nil || kit.Can(GrantPublish, "event.x.v1.y") || kit.Capabilities().PluginID != "" {
		t.Fatal("a nil Kit answered as if it held a host or grants")
	}
	if NewKitFrom(nil, negotiatedCaps()).Host() != nil {
		t.Fatal("a Kit built without a host reported one")
	}
}
