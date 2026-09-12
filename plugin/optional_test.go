package plugin

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// fullHost implements Host plus every optional interface, which is what a
// current gateway does.
type fullHost struct{ kitHost }

func (h *fullHost) SubscribeErr(string, func(bus.Envelope)) (func(), error) {
	return func() {}, nil
}
func (h *fullHost) EveryErr(time.Duration, func()) (func(), error) { return func() {}, nil }
func (h *fullHost) DataVersion() (DataVersion, bool)               { return "v1", true }
func (h *fullHost) SetDataVersion(DataVersion) error               { return nil }

// TestOptionalInterfacesSitBesideHost is TM-268: the new surface is additive.
// A plugin asks for what it wants and keeps working against a host that has
// none of it, which is what stops a contract addition from forcing 11 spawned
// plugins to be rebuilt and re-consented.
func TestOptionalInterfacesSitBesideHost(t *testing.T) {
	// The plain host still satisfies Host, and satisfies none of the optional
	// interfaces, so an older host is a legitimate host.
	var plain Host = &kitHost{}
	if _, ok := plain.(ObservableRegistrar); ok {
		t.Error("the plain host claims ObservableRegistrar")
	}
	if _, ok := plain.(DataVersioned); ok {
		t.Error("the plain host claims DataVersioned")
	}

	var current Host = &fullHost{}
	reg, ok := current.(ObservableRegistrar)
	if !ok {
		t.Fatal("a host implementing the erroring forms is not recognised")
	}
	if _, err := reg.SubscribeErr("event.x.v1.y", func(bus.Envelope) {}); err != nil {
		t.Fatalf("SubscribeErr: %v", err)
	}
	if _, err := reg.EveryErr(time.Second, func() {}); err != nil {
		t.Fatalf("EveryErr: %v", err)
	}
	versioned, ok := current.(DataVersioned)
	if !ok {
		t.Fatal("a host recording data versions is not recognised")
	}
	if v, found := versioned.DataVersion(); !found || v != DataVersion("v1") {
		t.Fatalf("DataVersion = %q %v", v, found)
	}

	// Host's own method set is untouched: adding to it is what this design
	// exists to avoid.
	for _, added := range []string{"SubscribeErr", "EveryErr", "DataVersion", "SetDataVersion", "OnDrain"} {
		if _, exists := reflect.TypeOf((*Host)(nil)).Elem().MethodByName(added); exists {
			t.Errorf("%s was added to Host; it must stay beside it", added)
		}
	}
}

// drainingPlugin is the plugin side.
type drainingPlugin struct {
	kitPlugin
	drained bool
}

func (p *drainingPlugin) OnDrain(context.Context) error { p.drained = true; return nil }

type kitPlugin struct{}

func (kitPlugin) ID() string                        { return "drain-probe" }
func (kitPlugin) Manifest() Manifest                { return Manifest{ID: "drain-probe"} }
func (kitPlugin) Start(context.Context, Host) error { return nil }
func (kitPlugin) Stop(context.Context) error        { return nil }

// TestDrainingIsOptionalAndCannotVeto documents the contract the host relies on:
// a plugin that does not implement it is normal, and an error does not stop
// teardown.
func TestDrainingIsOptionalAndCannotVeto(t *testing.T) {
	var plain Plugin = kitPlugin{}
	if _, ok := plain.(Draining); ok {
		t.Error("a plugin without OnDrain claims Draining")
	}
	p := &drainingPlugin{}
	var draining Plugin = p
	d, ok := draining.(Draining)
	if !ok {
		t.Fatal("a plugin with OnDrain is not recognised")
	}
	if err := d.OnDrain(context.Background()); err != nil {
		t.Fatalf("OnDrain: %v", err)
	}
	if !p.drained {
		t.Fatal("OnDrain did not run")
	}
	if _, exists := reflect.TypeOf((*Plugin)(nil)).Elem().MethodByName("OnDrain"); exists {
		t.Error("OnDrain was added to Plugin; it must stay optional")
	}
}
