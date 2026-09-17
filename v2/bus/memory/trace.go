package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

type ctxKey int

const (
	corrKey ctxKey = iota
	traceparentKey
)

// trace propagates bd-corr and passes traceparent through untouched.
//
// It is deliberately NOT gated on Capabilities.Trace: bus/trace.go says
// Capabilities.Trace describes broker-side message tracing, which is not built,
// while "bd-corr and traceparent propagation work regardless". Gating the
// accessor would make the default substrate's Trace() useless.
type trace struct{}

var _ bus.Trace = trace{}

func traceOf(*conn) bus.Trace { return trace{} }

func (t trace) Correlation(ctx context.Context) string {
	if ctx != nil {
		if v, ok := ctx.Value(corrKey).(string); ok && v != "" {
			return v
		}
	}
	return newCorrelation()
}

func (t trace) WithCorrelation(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, corrKey, id)
}

func (t trace) Inject(ctx context.Context, h bus.Headers) bus.Headers {
	out := h.Clone()
	if out == nil {
		out = bus.Headers{}
	}
	out[bus.HeaderCorrelation] = t.Correlation(ctx)
	if ctx != nil {
		if tp, ok := ctx.Value(traceparentKey).(string); ok && tp != "" {
			out[bus.HeaderTraceparent] = tp
		}
	}
	return out
}

func (t trace) Extract(ctx context.Context, h bus.Headers) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if corr := h.Get(bus.HeaderCorrelation); corr != "" {
		ctx = context.WithValue(ctx, corrKey, corr)
	}
	if tp := h.Get(bus.HeaderTraceparent); tp != "" {
		ctx = context.WithValue(ctx, traceparentKey, tp)
	}
	return ctx
}

func newCorrelation() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "bd-corr-unavailable"
	}
	return hex.EncodeToString(b[:])
}

func (c *conn) Trace() bus.Trace { return trace{} }
