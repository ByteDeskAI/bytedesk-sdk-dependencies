// Package bus is the shared event/command envelope (host + plugins).
package bus

import (
	"encoding/json"
	"time"
)

// Envelope matches the gateway BusEnvelope JSON shape.
type Envelope struct {
	ID            string            `json:"id"`
	CorrelationID string            `json:"correlationId,omitempty"`
	Type          string            `json:"type"`
	Source        string            `json:"source"`
	Timestamp     time.Time         `json:"ts"`
	Headers       map[string]string `json:"headers,omitempty"`
	Payload       json.RawMessage   `json:"payload,omitempty"`
}

// EventType returns event.<pluginID>.<name> (plugins must not publish host auth types).
func EventType(pluginID, name string) string {
	return "event." + pluginID + "." + name
}
