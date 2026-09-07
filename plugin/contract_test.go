package plugin

import (
	"context"
	"net/http"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
)

// refPlugin implements Plugin and every optional interface. It exists so the
// compiler checks the contract is implementable as written — an interface with
// no implementation can drift into something unsatisfiable and nothing notices.
type refPlugin struct{}

func (refPlugin) ID() string                                      { return "ref" }
func (refPlugin) Manifest() Manifest                              { return Manifest{ID: "ref"} }
func (refPlugin) Start(context.Context, Host) error               { return nil }
func (refPlugin) Stop(context.Context) error                      { return nil }
func (refPlugin) Handler() http.Handler                           { return http.NotFoundHandler() }
func (refPlugin) HealthSections(context.Context) []map[string]any { return nil }
func (refPlugin) Handles() []string                               { return []string{"cmd.ref"} }
func (refPlugin) HandleCommand(context.Context, bus.Envelope) (bus.Envelope, error) {
	return bus.Envelope{}, nil
}
func (refPlugin) Validate(context.Context, Host) error { return nil }
func (refPlugin) Ready(context.Context) error          { return nil }

var (
	_ Plugin            = refPlugin{}
	_ HTTPPlugin        = refPlugin{}
	_ HealthContributor = refPlugin{}
	_ CommandHandler    = refPlugin{}
	_ Validator         = refPlugin{}
	_ Readier           = refPlugin{}
)

// refHost implements Host, proving the interface is satisfiable by something
// other than the gateway's own kernel.
type refHost struct{}

func (refHost) Publish(bus.Envelope) error                  { return nil }
func (refHost) Subscribe(string, func(bus.Envelope)) func() { return func() {} }
func (refHost) Request(context.Context, bus.Envelope) (bus.Envelope, error) {
	return bus.Envelope{}, nil
}
func (refHost) Logger() Logger                     { return nil }
func (refHost) StateDir(string) string             { return "" }
func (refHost) Every(time.Duration, func()) func() { return func() {} }
func (refHost) BumpContributions()                 {}

var _ Host = refHost{}

// TestHostMethodSetIsPinned makes the cost of changing Host visible. Both
// implementations of Host — in-process and over a socket — must satisfy it, and
// an out-of-process plugin links a copy of this interface compiled into its own
// binary. So adding a method is a breaking change for every deployed external
// plugin, not a routine edit. This test does not forbid that; it makes it a
// deliberate act with a version bump attached.
func TestHostMethodSetIsPinned(t *testing.T) {
	want := []string{
		"BumpContributions",
		"Every",
		"Logger",
		"Publish",
		"Request",
		"StateDir",
		"Subscribe",
	}
	typ := reflect.TypeOf((*Host)(nil)).Elem()
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Host method set changed.\n got: %v\nwant: %v\n\n"+
			"Every Host method crosses a unix socket in the out-of-process mode, and "+
			"external plugins link their own copy of this interface. Adding or "+
			"removing one breaks every deployed external plugin: bump the SDK minor "+
			"version and update this list deliberately.", got, want)
	}
}

// TestPluginMethodSetIsPinned: same reasoning for the core interface.
func TestPluginMethodSetIsPinned(t *testing.T) {
	want := []string{"ID", "Manifest", "Start", "Stop"}
	typ := reflect.TypeOf((*Plugin)(nil)).Elem()
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Plugin method set changed.\n got: %v\nwant: %v\n\n"+
			"Optional capabilities belong in a segregated interface (see HTTPPlugin, "+
			"HealthContributor) so existing plugins keep compiling.", got, want)
	}
}
