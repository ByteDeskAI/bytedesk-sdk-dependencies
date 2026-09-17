package conformance

import (
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// This file is the fence's own fence.
//
// A conformance suite that cannot detect its own vacuity is worse than no
// suite: it converts "we did not look" into "we checked". So the whole
// property list is run, in a child process, against a substrate that is
// deliberately broken — it accepts every publish and drops it, refuses
// nothing, stamps no caller identity and provides none of the durable
// surfaces. Every property must FAIL against it.
//
// The child process is not an affectation. A failing subtest fails its parent,
// so there is no in-process way to observe "this property failed" without the
// observation itself failing the run.

const vacuityEnv = "BUS_CONFORMANCE_VACUITY_CHILD"

// TestPropertyMetadataIsWellFormed keeps the published list honest: a runner
// filters and reports on these fields.
func TestPropertyMetadataIsWellFormed(t *testing.T) {
	known := bus.CapabilityNames()
	seen := make(map[string]bool)
	for _, p := range Properties() {
		if p.Name == "" {
			t.Fatal("a property has no name; the name is the subtest name a runner filters on")
		}
		if seen[p.Name] {
			t.Fatalf("property %q is listed twice", p.Name)
		}
		seen[p.Name] = true
		for _, req := range p.Requires {
			if !slices.Contains(known, req) {
				t.Errorf("property %q requires %q, which is not in bus.CapabilityNames(); Capabilities.Has would silently report false and the property would skip forever", p.Name, req)
			}
		}
	}
	if got := len(Properties()); got != 24 {
		t.Errorf("the list holds %d properties, want 24; adding or removing one is a contract change", got)
	}
	wantRequired := []string{
		"QueueGroupExclusivity", "NoRespondersFastFail", "LoudAttributedRefusal",
		"PermanentDenyWins", "SourceUnforgeable", "RevokeDisconnectsWithinBound",
		"ReplayFromCursorAfterRestart",
	}
	var gotRequired []string
	for _, p := range Properties() {
		if p.RequiredForDefault {
			gotRequired = append(gotRequired, p.Name)
		}
	}
	if !slices.Equal(gotRequired, wantRequired) {
		t.Errorf("requiredForDefault set is %v, want %v", gotRequired, wantRequired)
	}
}

// TestVacuityChild runs the suite against the broken substrate. It is started
// by TestVacuityDetectsBrokenSubstrate and skips otherwise.
func TestVacuityChild(t *testing.T) {
	if os.Getenv(vacuityEnv) != "1" {
		t.Skip("child process only: run TestVacuityDetectsBrokenSubstrate")
	}
	Run(t, Harness{
		New:     func(*testing.T, bus.Identity) bus.Bus { return brokenBus{} },
		Restart: func(*testing.T) {},
		Revoke:  func(*testing.T, string) {},
		Caps: bus.Capabilities{
			Durable: true, KV: true, Objects: true, Services: true,
			Schedule: true, Counters: true, Batch: true,
			MaxPayload: 1024,
		},
		Deny:   []bus.Pattern{"deny.everything.>"},
		Budget: 250 * time.Millisecond,
	})
}

// TestVacuityDetectsBrokenSubstrate asserts that every property catches the
// broken substrate.
func TestVacuityDetectsBrokenSubstrate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVacuityChild$", "-test.v", "-test.timeout=4m")
	cmd.Env = append(os.Environ(), vacuityEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the property suite PASSED against a substrate that drops every publish and refuses nothing:\n%s", out)
	}
	if ctx.Err() != nil {
		t.Fatalf("the vacuity child never finished: %v\n%s", ctx.Err(), out)
	}

	verdicts := parseSubtestVerdicts(string(out), "TestVacuityChild/")
	var passed, skipped, missing []string
	for _, p := range Properties() {
		switch verdicts[p.Name] {
		case "FAIL":
		case "PASS":
			passed = append(passed, p.Name)
		case "SKIP":
			skipped = append(skipped, p.Name)
		default:
			missing = append(missing, p.Name)
		}
	}
	if len(passed) > 0 {
		t.Errorf("these properties PASSED against a do-nothing substrate and therefore prove nothing: %v", passed)
	}
	if len(skipped) > 0 {
		t.Errorf("these properties SKIPPED although the broken harness declares every capability and both hooks: %v", skipped)
	}
	if len(missing) > 0 {
		t.Errorf("these properties never reported a verdict (the child probably died mid-run): %v\n%s", missing, out)
	}
	t.Logf("vacuity: %d/%d properties caught the broken substrate", len(Properties())-len(passed)-len(skipped)-len(missing), len(Properties()))
}

// parseSubtestVerdicts reads "--- FAIL: Parent/Name (0.00s)" lines out of
// `go test -v` output.
func parseSubtestVerdicts(out, parent string) map[string]string {
	verdicts := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		for _, verdict := range []string{"FAIL", "PASS", "SKIP"} {
			prefix := "--- " + verdict + ": " + parent
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			fields := strings.Fields(strings.TrimPrefix(line, prefix))
			if len(fields) > 0 {
				verdicts[fields[0]] = verdict
			}
		}
	}
	return verdicts
}

// ------------------------------------------------- the deliberately broken bus

// brokenBus accepts every publish and drops it, hands out subscriptions that
// never deliver and never end, answers every request with the wrong fault, and
// implements none of the durable surfaces.
type brokenBus struct{}

func (brokenBus) Publish(context.Context, bus.Subject, []byte, ...bus.PublishOpt) error {
	return nil
}

func (brokenBus) Subscribe(context.Context, bus.Pattern, bus.Handler, ...bus.SubOpt) (bus.Subscription, error) {
	return &deadSub{done: make(chan struct{})}, nil
}

func (brokenBus) Request(_ context.Context, subject bus.Subject, _ []byte, _ ...bus.ReqOpt) (*bus.Msg, error) {
	return nil, bus.Fault{Code: bus.FaultTimeout, Op: string(subject)}
}

func (brokenBus) Streams() bus.Streams    { return brokenStreams{} }
func (brokenBus) KV() bus.KV              { return brokenKV{} }
func (brokenBus) Objects() bus.Objects    { return brokenObjects{} }
func (brokenBus) Services() bus.Services  { return brokenServices{} }
func (brokenBus) Schedule() bus.Scheduler { return brokenScheduler{} }
func (brokenBus) Trace() bus.Trace        { return brokenTrace{} }
func (brokenBus) Close() error            { return nil }

func (brokenBus) Capabilities() bus.Capabilities {
	return bus.Capabilities{
		Durable: true, KV: true, Objects: true, Services: true,
		Schedule: true, Counters: true, Batch: true, MaxPayload: 1024,
	}
}

type deadSub struct {
	once sync.Once
	done chan struct{}
}

func (s *deadSub) Cancel()                     { s.once.Do(func() { close(s.done) }) }
func (s *deadSub) Drain(context.Context) error { s.Cancel(); return nil }
func (s *deadSub) Done() <-chan struct{}       { return s.done }
func (s *deadSub) Err() error                  { return nil }

func broke(op string) error { return bus.Unsupported(op, "anything at all") }

type brokenStreams struct{}

func (brokenStreams) Declare(context.Context, bus.StreamSpec) error { return broke("streams.declare") }
func (brokenStreams) Publish(context.Context, bus.Subject, []byte, ...bus.PublishOpt) (bus.Seq, error) {
	return 0, broke("streams.publish")
}
func (brokenStreams) PublishBatch(context.Context, []bus.BatchMsg) ([]bus.Seq, error) {
	return nil, broke("streams.batch")
}
func (brokenStreams) Counter(context.Context, bus.Subject, int64) (int64, error) {
	return 0, broke("streams.counter")
}
func (brokenStreams) Consume(context.Context, string, bus.ConsumerSpec, bus.StreamHandler) (bus.Consumer, error) {
	return nil, broke("streams.consume")
}
func (brokenStreams) Fetch(context.Context, string, bus.ConsumerSpec, int) ([]*bus.StreamMsg, error) {
	return nil, broke("streams.fetch")
}
func (brokenStreams) Purge(context.Context, string, bus.Pattern) error { return broke("streams.purge") }
func (brokenStreams) Info(context.Context, string) (bus.StreamInfo, error) {
	return bus.StreamInfo{}, broke("streams.info")
}
func (brokenStreams) Delete(context.Context, string) error { return broke("streams.delete") }

type brokenKV struct{}

func (brokenKV) Declare(context.Context, bus.BucketSpec) error { return broke("kv.declare") }
func (brokenKV) Open(context.Context, string) (bus.Bucket, error) {
	return nil, broke("kv.open")
}
func (brokenKV) Delete(context.Context, string) error   { return broke("kv.delete") }
func (brokenKV) List(context.Context) ([]string, error) { return nil, broke("kv.list") }

type brokenObjects struct{}

func (brokenObjects) Declare(context.Context, bus.BucketSpec) error { return broke("objects.declare") }
func (brokenObjects) Put(context.Context, bus.ObjectMeta, io.Reader) (bus.ObjectMeta, error) {
	return bus.ObjectMeta{}, broke("objects.put")
}
func (brokenObjects) Get(context.Context, string, string) (io.ReadCloser, bus.ObjectMeta, error) {
	return nil, bus.ObjectMeta{}, broke("objects.get")
}
func (brokenObjects) Info(context.Context, string, string) (bus.ObjectMeta, error) {
	return bus.ObjectMeta{}, broke("objects.info")
}
func (brokenObjects) Delete(context.Context, string, string) error { return broke("objects.delete") }
func (brokenObjects) List(context.Context, string) ([]bus.ObjectMeta, error) {
	return nil, broke("objects.list")
}
func (brokenObjects) Watch(context.Context, string) (bus.ObjectWatcher, error) {
	return nil, broke("objects.watch")
}

type brokenServices struct{}

func (brokenServices) Serve(context.Context, bus.ServiceSpec) (bus.Service, error) {
	return nil, broke("services.serve")
}
func (brokenServices) Call(_ context.Context, subject bus.Subject, _ []byte, _ ...bus.ReqOpt) (*bus.Msg, error) {
	return nil, bus.Unsupported(string(subject), "anything at all")
}
func (brokenServices) Discover(context.Context, string) ([]bus.ServiceInfo, error) {
	return nil, broke("services.discover")
}
func (brokenServices) Stats(context.Context, string) ([]bus.ServiceStats, error) {
	return nil, broke("services.stats")
}

type brokenScheduler struct{}

func (brokenScheduler) At(context.Context, string, time.Time, bus.Subject, []byte, ...bus.PublishOpt) error {
	return broke("schedule.at")
}
func (brokenScheduler) Every(context.Context, string, time.Duration, bus.Subject, []byte, ...bus.PublishOpt) error {
	return broke("schedule.every")
}
func (brokenScheduler) Cron(context.Context, string, string, bus.Subject, []byte, ...bus.PublishOpt) error {
	return broke("schedule.cron")
}
func (brokenScheduler) Cancel(context.Context, string) error { return nil }
func (brokenScheduler) List(context.Context) ([]bus.Schedule, error) {
	return nil, broke("schedule.list")
}

type brokenTrace struct{}

func (brokenTrace) Correlation(context.Context) string                            { return "" }
func (brokenTrace) WithCorrelation(ctx context.Context, _ string) context.Context { return ctx }
func (brokenTrace) Inject(_ context.Context, h bus.Headers) bus.Headers           { return h }
func (brokenTrace) Extract(ctx context.Context, _ bus.Headers) context.Context    { return ctx }
