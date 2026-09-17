package plugin

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Base is what a plugin embeds. Everything on it is inherited; nothing is
// wired. Accessors only.
//
// The accessor-only rule is not style. Go promotes an embedded type's methods
// onto the outer type, so any method here becomes a method on every plugin in
// the fleet — and a method named Start, Validate or Ready would make each of
// them satisfy a lifecycle interface the author never wrote and the host would
// then call. Base therefore exposes five readers and one unexported seal, and
// TestBaseMethodSetIsAccessorsOnly proves the set by reflection so the rule
// survives the next person who wants "just one helper" on it.
//
// A zero Base is INERT, not broken: every accessor answers with a refusing
// double rather than a nil or a panic, so a plugin constructed but never bound
// fails at the point of use with an attributed Fault instead of crashing the
// host it was loaded into.
type Base struct {
	mu       sync.RWMutex
	bound    bool
	bus      bus.Bus
	logger   Logger
	profiler Profiler
	stateDir string
	identity bus.Identity
}

// Bus returns this generation's messaging surface. On an unbound Base it
// returns a bus whose every operation refuses with FaultWithdrawn, naming the
// operation, so the refusal is attributed rather than a nil dereference.
func (b *Base) Bus() bus.Bus {
	if b == nil {
		return unboundBus{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.bus == nil {
		return unboundBus{}
	}
	return b.bus
}

// Logger returns the host logger, already tagged with this plugin's id. On an
// unbound Base it discards.
func (b *Base) Logger() Logger {
	if b == nil {
		return nopLogger{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.logger == nil {
		return nopLogger{}
	}
	return b.logger
}

// Profiling returns this plugin's profiler switch. Off is the default, and an
// unbound Base returns a profiler that is always off and cannot be turned on.
func (b *Base) Profiling() Profiler {
	if b == nil {
		return NopProfiler()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.profiler == nil {
		return NopProfiler()
	}
	return b.profiler
}

// StateDir is where this plugin may persist state. The host owns the path; a
// plugin must not assume it is under any particular root. An unbound Base
// returns "", which every path join and os.Open refuses on its own.
func (b *Base) StateDir() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stateDir
}

// Identity is who this generation is bound to, including its effective grants.
// An unbound Base returns the zero Identity, whose String is "<unbound>" and
// whose Grants deny everything.
func (b *Base) Identity() bus.Identity {
	if b == nil {
		return bus.Identity{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.identity
}

// base is the seal. It is unexported and declared in this package, so Bound is
// satisfiable only by embedding Base — a plugin cannot hand the host a type
// that merely looks bindable.
func (b *Base) base() *Base { return b }

// Bound is satisfiable ONLY by embedding Base, because base() is unexported and
// lives in this package. Bind takes a Bound rather than a Plugin so the host can
// install a binding before the plugin is complete, and so a test can bind a
// fixture that is not yet a whole plugin.
type Bound interface{ base() *Base }

// capsBus reports the capabilities the host NEGOTIATED for this generation
// rather than everything the substrate happens to implement. Negotiation
// happens before Bind and can only narrow, so a plugin that reads
// Bus().Capabilities() sees what it was actually admitted to use.
type capsBus struct {
	bus.Bus
	caps bus.Capabilities
}

func (c capsBus) Capabilities() bus.Capabilities { return c.caps }

// The refusing doubles.
//
// Each one answers every call with an attributed Fault. Methods that hand back
// a further handle (a Bucket, a Consumer, a Service) return a nil handle WITH
// that Fault rather than a second double: the error is the signal, and a caller
// that ignores it and uses the handle anyway has a bug the SDK should not hide.
// The handles a plugin reaches without an error return — Streams, KV, Objects,
// Services, Schedule, Trace — are always non-nil, because those are field-style
// accesses with nowhere to put an error.

type unboundBus struct{}

func (unboundBus) Publish(context.Context, bus.Subject, []byte, ...bus.PublishOpt) error {
	return unbound("bus.Publish")
}

func (unboundBus) Subscribe(context.Context, bus.Pattern, bus.Handler, ...bus.SubOpt) (bus.Subscription, error) {
	return nil, unbound("bus.Subscribe")
}

func (unboundBus) Request(context.Context, bus.Subject, []byte, ...bus.ReqOpt) (*bus.Msg, error) {
	return nil, unbound("bus.Request")
}

func (unboundBus) Streams() bus.Streams           { return unboundStreams{} }
func (unboundBus) KV() bus.KV                     { return unboundKV{} }
func (unboundBus) Objects() bus.Objects           { return unboundObjects{} }
func (unboundBus) Services() bus.Services         { return unboundServices{} }
func (unboundBus) Schedule() bus.Scheduler        { return unboundScheduler{} }
func (unboundBus) Trace() bus.Trace               { return unboundTrace{} }
func (unboundBus) Capabilities() bus.Capabilities { return bus.Capabilities{} }
func (unboundBus) Close() error                   { return unbound("bus.Close") }

type unboundStreams struct{}

func (unboundStreams) Declare(context.Context, bus.StreamSpec) error {
	return unbound("streams.Declare")
}

func (unboundStreams) Publish(context.Context, bus.Subject, []byte, ...bus.PublishOpt) (bus.Seq, error) {
	return 0, unbound("streams.Publish")
}

func (unboundStreams) PublishBatch(context.Context, []bus.BatchMsg) ([]bus.Seq, error) {
	return nil, unbound("streams.PublishBatch")
}

func (unboundStreams) Counter(context.Context, bus.Subject, int64) (int64, error) {
	return 0, unbound("streams.Counter")
}

func (unboundStreams) Consume(context.Context, string, bus.ConsumerSpec, bus.StreamHandler) (bus.Consumer, error) {
	return nil, unbound("streams.Consume")
}

func (unboundStreams) Fetch(context.Context, string, bus.ConsumerSpec, int) ([]*bus.StreamMsg, error) {
	return nil, unbound("streams.Fetch")
}

func (unboundStreams) Purge(context.Context, string, bus.Pattern) error {
	return unbound("streams.Purge")
}

func (unboundStreams) Info(context.Context, string) (bus.StreamInfo, error) {
	return bus.StreamInfo{}, unbound("streams.Info")
}

func (unboundStreams) Delete(context.Context, string) error { return unbound("streams.Delete") }

type unboundKV struct{}

func (unboundKV) Declare(context.Context, bus.BucketSpec) error { return unbound("kv.Declare") }

func (unboundKV) Open(context.Context, string) (bus.Bucket, error) {
	return nil, unbound("kv.Open")
}

func (unboundKV) Delete(context.Context, string) error   { return unbound("kv.Delete") }
func (unboundKV) List(context.Context) ([]string, error) { return nil, unbound("kv.List") }

type unboundObjects struct{}

func (unboundObjects) Declare(context.Context, bus.BucketSpec) error {
	return unbound("objects.Declare")
}

func (unboundObjects) Put(context.Context, bus.ObjectMeta, io.Reader) (bus.ObjectMeta, error) {
	return bus.ObjectMeta{}, unbound("objects.Put")
}

func (unboundObjects) Get(context.Context, string, string) (io.ReadCloser, bus.ObjectMeta, error) {
	return nil, bus.ObjectMeta{}, unbound("objects.Get")
}

func (unboundObjects) Info(context.Context, string, string) (bus.ObjectMeta, error) {
	return bus.ObjectMeta{}, unbound("objects.Info")
}

func (unboundObjects) Delete(context.Context, string, string) error {
	return unbound("objects.Delete")
}

func (unboundObjects) List(context.Context, string) ([]bus.ObjectMeta, error) {
	return nil, unbound("objects.List")
}

func (unboundObjects) Watch(context.Context, string) (bus.ObjectWatcher, error) {
	return nil, unbound("objects.Watch")
}

type unboundServices struct{}

func (unboundServices) Serve(context.Context, bus.ServiceSpec) (bus.Service, error) {
	return nil, unbound("services.Serve")
}

func (unboundServices) Call(context.Context, bus.Subject, []byte, ...bus.ReqOpt) (*bus.Msg, error) {
	return nil, unbound("services.Call")
}

func (unboundServices) Discover(context.Context, string) ([]bus.ServiceInfo, error) {
	return nil, unbound("services.Discover")
}

func (unboundServices) Stats(context.Context, string) ([]bus.ServiceStats, error) {
	return nil, unbound("services.Stats")
}

type unboundScheduler struct{}

func (unboundScheduler) At(context.Context, string, time.Time, bus.Subject, []byte, ...bus.PublishOpt) error {
	return unbound("schedule.At")
}

func (unboundScheduler) Every(context.Context, string, time.Duration, bus.Subject, []byte, ...bus.PublishOpt) error {
	return unbound("schedule.Every")
}

func (unboundScheduler) Cron(context.Context, string, string, bus.Subject, []byte, ...bus.PublishOpt) error {
	return unbound("schedule.Cron")
}

func (unboundScheduler) Cancel(context.Context, string) error { return unbound("schedule.Cancel") }

func (unboundScheduler) List(context.Context) ([]bus.Schedule, error) {
	return nil, unbound("schedule.List")
}

// unboundTrace is the one double that cannot refuse: every Trace method returns
// a value with no error beside it. It degrades to the identity function instead
// — no correlation id is minted, and nothing is injected or extracted — which
// leaves a call untraced rather than failing it. An untraced call from an
// unbound plugin is not a hazard, because an unbound plugin cannot make one.
type unboundTrace struct{}

func (unboundTrace) Correlation(context.Context) string { return "" }

func (unboundTrace) WithCorrelation(ctx context.Context, _ string) context.Context { return ctx }

func (unboundTrace) Inject(_ context.Context, h bus.Headers) bus.Headers { return h }

func (unboundTrace) Extract(ctx context.Context, _ bus.Headers) context.Context { return ctx }
