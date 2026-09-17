package plugin

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// TestBaseMethodSetIsAccessorsOnly is why an embedded base is safe.
//
// Go promotes an embedded type's methods onto its outer type. If Base carried a
// method named Start, Validate, Ready, Handler or CheckActivation, then every
// plugin embedding it would satisfy that interface without its author writing a
// line — and the host, which discovers optional behaviour by type assertion,
// would call it. The two sets below are derived by reflection from the actual
// types rather than from a hand-written list, because a hand-written list is
// exactly the thing that stops being true when someone adds a method.
func TestBaseMethodSetIsAccessorsOnly(t *testing.T) {
	want := map[string]bool{
		"Bus":       true,
		"Logger":    true,
		"Profiling": true,
		"StateDir":  true,
		"Identity":  true,
	}

	baseType := reflect.TypeOf(&Base{})
	got := map[string]bool{}
	for i := 0; i < baseType.NumMethod(); i++ {
		got[baseType.Method(i).Name] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Base exported method set = %v, want %v", keys(got), keys(want))
	}

	// Every interface the host type-asserts for. Base must share no method
	// name with any of them.
	interfaces := map[string]reflect.Type{
		"Plugin":            reflect.TypeOf((*Plugin)(nil)).Elem(),
		"Bound":             reflect.TypeOf((*Bound)(nil)).Elem(),
		"HTTPPlugin":        reflect.TypeOf((*HTTPPlugin)(nil)).Elem(),
		"HealthContributor": reflect.TypeOf((*HealthContributor)(nil)).Elem(),
		"Validator":         reflect.TypeOf((*Validator)(nil)).Elem(),
		"Readier":           reflect.TypeOf((*Readier)(nil)).Elem(),
		"ActivationChecker": reflect.TypeOf((*ActivationChecker)(nil)).Elem(),
		"Draining":          reflect.TypeOf((*Draining)(nil)).Elem(),
		"DataVersioned":     reflect.TypeOf((*DataVersioned)(nil)).Elem(),
	}
	seen := 0
	for name, iface := range interfaces {
		for i := 0; i < iface.NumMethod(); i++ {
			m := iface.Method(i)
			if m.PkgPath != "" {
				continue // unexported: the seal
			}
			seen++
			if got[m.Name] {
				t.Errorf("Base has method %s, which also belongs to %s: embedding it would make every plugin implement %s by accident", m.Name, name, name)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no optional-interface methods were found by reflection; the test proves nothing")
	}

	// The seal itself is unexported and therefore not promotable outside this
	// package, which is what makes Bound unsatisfiable without Base.
	if m, ok := reflect.TypeOf((*Bound)(nil)).Elem().MethodByName("base"); !ok || m.PkgPath == "" {
		t.Error("Bound.base is not an unexported method of this package; Bound is forgeable")
	}
}

// TestZeroBaseIsInert walks every method of the bus a NEVER-BOUND Base returns
// and requires an attributed refusal rather than a nil or a panic. A plugin
// that is constructed but not yet bound — a test fixture, a plugin whose Bind
// failed, a stale generation — must fail at the point of use with a Fault
// naming the operation, not crash the host it was loaded into.
func TestZeroBaseIsInert(t *testing.T) {
	var b Base

	if dir := b.StateDir(); dir != "" {
		t.Errorf("StateDir() = %q, want empty", dir)
	}
	if id := b.Identity(); id.PluginID != "" || id.Generation != "" || !id.Grants.IsZero() || !id.Lease.IsZero() || id.Role != "" {
		t.Errorf("Identity() = %+v, want the zero Identity", id)
	}
	if got := b.Identity().String(); got != "<unbound>" {
		t.Errorf("Identity().String() = %q, want %q", got, "<unbound>")
	}
	if p := b.Profiling(); p == nil || p.Enabled() {
		t.Error("Profiling() must return a profiler that is off, not nil")
	}
	p := b.Profiling()
	p.Set(true)
	if p.Enabled() {
		t.Error("an unbound profiler was turned on")
	}
	if l := b.Logger(); l == nil {
		t.Fatal("Logger() returned nil")
	}
	b.Logger().Info("an unbound logger discards rather than panicking")

	live := b.Bus()
	if live == nil {
		t.Fatal("Bus() returned nil")
	}
	if caps := live.Capabilities(); caps != (bus.Capabilities{}) {
		t.Errorf("Capabilities() = %+v, want the zero Capabilities", caps)
	}

	refusals := 0
	rv := reflect.ValueOf(live)
	for i := 0; i < rv.NumMethod(); i++ {
		name := rv.Type().Method(i).Name
		fn := rv.Method(i)
		ft := fn.Type()
		n := ft.NumIn()
		if ft.IsVariadic() {
			n--
		}
		args := make([]reflect.Value, n)
		for j := 0; j < n; j++ {
			args[j] = reflect.New(ft.In(j)).Elem()
		}

		out := fn.Call(args)
		var err error
		hasErr := false
		for _, res := range out {
			if res.Type() != errorType {
				continue
			}
			hasErr = true
			if !res.IsNil() {
				err, _ = res.Interface().(error)
			}
		}
		if hasErr {
			if err == nil {
				t.Errorf("%s() on an unbound base returned a nil error", name)
				continue
			}
			var fault bus.Fault
			if !errors.As(err, &fault) {
				t.Errorf("%s() returned %v, which is not a bus.Fault", name, err)
				continue
			}
			if fault.Code != bus.FaultWithdrawn {
				t.Errorf("%s() returned code %q, want %q", name, fault.Code, bus.FaultWithdrawn)
			}
			if fault.Op == "" || fault.Message == "" {
				t.Errorf("%s() returned an unattributed refusal %+v", name, fault)
			}
			refusals++
			continue
		}
		// No error to carry a refusal: the handle must still be usable, i.e.
		// non-nil, or the next call dereferences nil.
		for _, res := range out {
			if res.Kind() == reflect.Interface && res.IsNil() {
				t.Errorf("%s() returned a nil %s with no error beside it", name, res.Type())
			}
		}
	}
	if refusals == 0 {
		t.Fatal("no bus method returned a refusal; the test proves nothing")
	}

	// The handles reached without an error return must refuse in turn.
	if err := live.Streams().Declare(context.Background(), bus.StreamSpec{}); err == nil {
		t.Error("an unbound Streams accepted a Declare")
	}
	if _, err := live.KV().Open(context.Background(), "b"); err == nil {
		t.Error("an unbound KV opened a bucket")
	}
	if _, err := live.Services().Serve(context.Background(), bus.ServiceSpec{}); err == nil {
		t.Error("an unbound Services mounted a service")
	}
	if err := live.Schedule().Cancel(context.Background(), "x"); err == nil {
		t.Error("an unbound Scheduler accepted a Cancel")
	}
	if _, err := live.Objects().List(context.Background(), "b"); err == nil {
		t.Error("an unbound Objects listed a bucket")
	}
	// Trace has no error channel anywhere, so it degrades to identity instead
	// of refusing. Leaving a call untraced is not a hazard; panicking is.
	ctx := context.Background()
	if live.Trace().Correlation(ctx) != "" {
		t.Error("an unbound Trace minted a correlation id")
	}
	if live.Trace().WithCorrelation(ctx, "id") != ctx {
		t.Error("an unbound Trace altered the context")
	}
}

// TestNilBaseReceiverDoesNotPanic covers the receiver a host can produce by
// accident: a Bound whose *Base is nil. Every accessor is nil-safe, so the
// failure is still a Fault at the point of use.
func TestNilBaseReceiverDoesNotPanic(t *testing.T) {
	var b *Base
	if b.StateDir() != "" {
		t.Error("nil StateDir")
	}
	if id := b.Identity(); id.PluginID != "" || !id.Grants.IsZero() {
		t.Error("nil Identity")
	}
	if b.Logger() == nil || b.Profiling() == nil || b.Bus() == nil {
		t.Fatal("a nil *Base returned a nil accessor")
	}
	b.Logger().Warn("nil receiver")
	if err := b.Bus().Publish(context.Background(), "a.b", nil); err == nil {
		t.Error("a nil *Base published")
	}
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
