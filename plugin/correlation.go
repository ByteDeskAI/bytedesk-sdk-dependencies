package plugin

// CorrelationIDLogKey is the structured logging key used for a host-minted
// request correlation id. The id is diagnostic metadata, not authority.
const CorrelationIDLogKey = "correlation_id"

// LoggerWithCorrelationID returns a logger that adds correlationID to every
// entry as structured data. It does not add a method to Logger, so existing
// hosts and plugins remain source compatible. The transport-specific SDK is
// responsible for accepting correlation ids only from its trusted request
// boundary.
func LoggerWithCorrelationID(logger Logger, correlationID string) Logger {
	if logger == nil || correlationID == "" {
		return logger
	}
	return correlationLogger{Logger: logger, correlationID: correlationID}
}

type correlationLogger struct {
	Logger
	correlationID string
}

func (l correlationLogger) Info(msg string, args ...any) {
	l.Logger.Info(msg, l.args(args)...)
}

func (l correlationLogger) Warn(msg string, args ...any) {
	l.Logger.Warn(msg, l.args(args)...)
}

func (l correlationLogger) Error(msg string, args ...any) {
	l.Logger.Error(msg, l.args(args)...)
}

func (l correlationLogger) args(args []any) []any {
	fields := make([]any, 0, len(args)+2)
	fields = append(fields, args...)
	return append(fields, CorrelationIDLogKey, l.correlationID)
}
