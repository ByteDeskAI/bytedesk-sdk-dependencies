package plugin

import (
	"reflect"
	"testing"
)

type correlationLogEntry struct {
	level string
	msg   string
	args  []any
}

type correlationCaptureLogger struct {
	entries []correlationLogEntry
}

func (l *correlationCaptureLogger) Info(msg string, args ...any) {
	l.entries = append(l.entries, correlationLogEntry{level: "info", msg: msg, args: append([]any(nil), args...)})
}

func (l *correlationCaptureLogger) Warn(msg string, args ...any) {
	l.entries = append(l.entries, correlationLogEntry{level: "warn", msg: msg, args: append([]any(nil), args...)})
}

func (l *correlationCaptureLogger) Error(msg string, args ...any) {
	l.entries = append(l.entries, correlationLogEntry{level: "error", msg: msg, args: append([]any(nil), args...)})
}

func TestLoggerWithCorrelationIDAddsStructuredField(t *testing.T) {
	base := &correlationCaptureLogger{}
	logger := LoggerWithCorrelationID(base, "01K5ABCD1234EFGH5678JKLMNP")
	original := []any{"operation", "scan"}
	logger.Info("finished", original...)

	want := correlationLogEntry{
		level: "info",
		msg:   "finished",
		args:  []any{"operation", "scan", CorrelationIDLogKey, "01K5ABCD1234EFGH5678JKLMNP"},
	}
	if len(base.entries) != 1 || !reflect.DeepEqual(base.entries[0], want) {
		t.Fatalf("entries = %#v, want %#v", base.entries, []correlationLogEntry{want})
	}
	if !reflect.DeepEqual(original, []any{"operation", "scan"}) {
		t.Fatalf("caller args mutated: %#v", original)
	}
}

func TestLoggerWithCorrelationIDPreservesLegacyLogger(t *testing.T) {
	base := &correlationCaptureLogger{}
	if got := LoggerWithCorrelationID(base, ""); got != base {
		t.Fatal("empty correlation id should preserve the legacy logger")
	}
	if got := LoggerWithCorrelationID(nil, "01K5ABCD1234EFGH5678JKLMNP"); got != nil {
		t.Fatal("nil logger should remain nil")
	}
	if _, exists := reflect.TypeOf((*Logger)(nil)).Elem().MethodByName("WithCorrelationID"); exists {
		t.Fatal("correlation support must remain additive; Logger method set changed")
	}
}
