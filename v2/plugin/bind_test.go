package plugin

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// boundFixture is the smallest thing Bind accepts: a type that embeds Base and
// therefore satisfies Bound.
type boundFixture struct{ Base }

// markerBus identifies WHICH bus a base is holding, so the test can prove a
// refused Bind left the original one in place rather than merely returning an
// error.
type markerBus struct {
	unboundBus
	id string
}

func (m markerBus) Publish(context.Context, bus.Subject, []byte, ...bus.PublishOpt) error {
	return errors.New(m.id)
}

func busID(t *testing.T, b bus.Bus) string {
	t.Helper()
	err := b.Publish(context.Background(), "a.b", nil)
	if err == nil {
		t.Fatal("marker bus did not identify itself")
	}
	return err.Error()
}

type countingProfiler struct{ on bool }

func (p *countingProfiler) Enabled() bool { return p.on }
func (p *countingProfiler) Set(v bool)    { p.on = v }

// TestBindOncePerGeneration is the whole reason Bind returns an error.
//
// A second Bind for a generation that is already live would swap the bus out
// from under a running plugin. Nothing would log, nothing would fail, and the
// plugin would go on publishing into a connection the host had already torn
// down — which is indistinguishable, from inside the plugin, from a bus that
// simply stopped delivering. So a repeat is a refusal, and the existing binding
// is left exactly as it was.
func TestBindOncePerGeneration(t *testing.T) {
	gen1 := bus.Identity{PluginID: "files", Generation: "g1", Role: bus.RolePlugin}

	t.Run("first bind installs everything", func(t *testing.T) {
		p := &boundFixture{}
		prof := &countingProfiler{}
		err := Bind(p, Binding{
			Bus:      markerBus{id: "first"},
			Logger:   nopLogger{},
			Profiler: prof,
			StateDir: "/var/lib/files",
			Identity: gen1,
			Caps:     bus.Capabilities{KV: true, MaxPayload: 65536},
		})
		if err != nil {
			t.Fatalf("Bind: %v", err)
		}
		if got := busID(t, p.Bus()); got != "first" {
			t.Errorf("Bus() = %q, want the bound one", got)
		}
		if p.StateDir() != "/var/lib/files" {
			t.Errorf("StateDir() = %q", p.StateDir())
		}
		if p.Identity().Generation != "g1" {
			t.Errorf("Identity() = %+v", p.Identity())
		}
		if p.Profiling() != prof {
			t.Error("Profiling() did not return the bound profiler")
		}
		// Capabilities come from the NEGOTIATED set in the Binding, not from
		// whatever the raw substrate reports.
		if caps := p.Bus().Capabilities(); !caps.KV || caps.MaxPayload != 65536 {
			t.Errorf("Capabilities() = %+v, want the negotiated set", caps)
		}
	})

	t.Run("second bind for the same generation is a conflict and changes nothing", func(t *testing.T) {
		p := &boundFixture{}
		if err := Bind(p, Binding{Bus: markerBus{id: "first"}, StateDir: "/one", Identity: gen1}); err != nil {
			t.Fatalf("first Bind: %v", err)
		}
		err := Bind(p, Binding{Bus: markerBus{id: "second"}, StateDir: "/two", Identity: gen1})
		var fault bus.Fault
		if !errors.As(err, &fault) {
			t.Fatalf("second Bind returned %v, want a bus.Fault", err)
		}
		if fault.Code != bus.FaultConflict {
			t.Errorf("code = %q, want %q", fault.Code, bus.FaultConflict)
		}
		if got := busID(t, p.Bus()); got != "first" {
			t.Errorf("the refused Bind replaced the bus: %q", got)
		}
		if p.StateDir() != "/one" {
			t.Errorf("the refused Bind replaced the state dir: %q", p.StateDir())
		}
	})

	t.Run("a later generation replaces the binding", func(t *testing.T) {
		p := &boundFixture{}
		if err := Bind(p, Binding{Bus: markerBus{id: "first"}, Identity: gen1}); err != nil {
			t.Fatalf("first Bind: %v", err)
		}
		gen2 := bus.Identity{PluginID: "files", Generation: "g2", Role: bus.RolePlugin}
		if err := Bind(p, Binding{Bus: markerBus{id: "second"}, StateDir: "/two", Identity: gen2}); err != nil {
			t.Fatalf("Bind for a new generation: %v", err)
		}
		if got := busID(t, p.Bus()); got != "second" {
			t.Errorf("Bus() = %q, want the new generation's", got)
		}
		if p.Identity().Generation != "g2" {
			t.Errorf("Identity() = %+v", p.Identity())
		}
	})

	t.Run("nil plugin and a binding with no bus are faults, not panics", func(t *testing.T) {
		var fault bus.Fault
		if err := Bind(nil, Binding{Bus: markerBus{}}); !errors.As(err, &fault) || fault.Code != bus.FaultUnavailable {
			t.Errorf("Bind(nil, ...) = %v, want FaultUnavailable", err)
		}
		p := &boundFixture{}
		if err := Bind(p, Binding{Identity: gen1}); !errors.As(err, &fault) || fault.Code != bus.FaultUnavailable {
			t.Errorf("Bind with no bus = %v, want FaultUnavailable", err)
		}
		// The refused Bind left the base unbound rather than half-installed.
		if err := p.Bus().Publish(context.Background(), "a.b", nil); err == nil {
			t.Error("a base refused at Bind became live anyway")
		}
	})

	t.Run("grants are cloned, so a host cannot widen them afterwards", func(t *testing.T) {
		p := &boundFixture{}
		id := bus.Identity{PluginID: "files", Generation: "g1", Grants: bus.Grants{Publish: []bus.Pattern{"evt.files.>"}}}
		if err := Bind(p, Binding{Bus: markerBus{}, Identity: id}); err != nil {
			t.Fatalf("Bind: %v", err)
		}
		id.Grants.Publish[0] = "evt.>"
		if got := p.Identity().Grants.Publish[0]; got != "evt.files.>" {
			t.Errorf("the host widened a bound plugin's grants to %q", got)
		}
	})

	t.Run("concurrent Bind and accessors are race-free", func(t *testing.T) {
		p := &boundFixture{}
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = Bind(p, Binding{
					Bus:      markerBus{id: "g"},
					Identity: bus.Identity{PluginID: "files", Generation: string(rune('a' + i))},
				})
			}(i)
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = p.Bus()
				_ = p.Identity()
				_ = p.StateDir()
				_ = p.Logger()
				_ = p.Profiling()
			}()
		}
		wg.Wait()
	})
}
