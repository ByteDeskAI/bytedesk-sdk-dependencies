package bus

import "context"

// Trace propagates correlation across calls. bd-corr is the ByteDesk
// correlation id and traceparent is W3C trace context; both are stamped on
// egress automatically, so a plugin that does nothing still produces traceable
// calls. Trace is for reading and overriding that, not for enabling it.
//
// Broker-side message tracing is a separate thing and is not built yet;
// Capabilities.Trace stays false until it is.
type Trace interface {
	// Correlation reads the correlation id from ctx, minting one if the
	// context carries none.
	Correlation(ctx context.Context) string
	// WithCorrelation returns a context carrying id, so every call made under
	// it is stamped with the same value.
	WithCorrelation(ctx context.Context, id string) context.Context
	// Inject writes the context's correlation and trace headers into h.
	Inject(ctx context.Context, h Headers) Headers
	// Extract returns a context carrying the correlation and trace headers
	// found in h. A handler calls it once and passes the result on.
	Extract(ctx context.Context, h Headers) context.Context
}
