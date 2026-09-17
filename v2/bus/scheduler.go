package bus

import (
	"context"
	"time"
)

// Schedule is one scheduled publish.
type Schedule struct {
	// Name is the schedule's identity within the plugin. Re-declaring the
	// same name replaces the schedule rather than adding a second one.
	Name    string
	Subject Subject
	Data    []byte
	Headers Headers
	// At, Every and Cron are alternatives; exactly one is set.
	At    time.Time
	Every time.Duration
	Cron  string
	// Next is the next fire time, reported by List.
	Next time.Time
}

// Scheduler is durable scheduled publishing. It replaces v1 Host.Every and its
// single timer wheel.
//
// Where the substrate is durable, a schedule survives a restart — and delivery
// therefore becomes AT-LEAST-ONCE, because a fire that was written but not yet
// delivered is re-delivered on recovery. A handler that was idempotent under
// v1's in-process timer by accident has to become idempotent on purpose. This
// is flagged per plugin during migration.
type Scheduler interface {
	// At publishes once at t.
	At(ctx context.Context, name string, t time.Time, subject Subject, data []byte, opts ...PublishOpt) error
	// Every publishes on an interval. This is plugin.Every's implementation.
	Every(ctx context.Context, name string, d time.Duration, subject Subject, data []byte, opts ...PublishOpt) error
	// Cron publishes on a cron expression.
	Cron(ctx context.Context, name string, expr string, subject Subject, data []byte, opts ...PublishOpt) error
	// Cancel removes a schedule by name. Cancelling an unknown name is not an
	// error: cancel is what Stop calls, and Stop must be safe to call twice.
	Cancel(ctx context.Context, name string) error
	// List reports this principal's schedules with their next fire times.
	List(ctx context.Context) ([]Schedule, error)
}
