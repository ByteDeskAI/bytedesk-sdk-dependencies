package plugin

import "strings"

// Plugin lifecycle states and the bus events a host publishes on each
// transition (ADR 0023). A plugin subscribes to these to learn that it is
// ready, that a peer arrived, or that a dependency went away — none of which
// were observable when a plugin had only Start and Stop.

const (
	StateDiscovered  = "discovered"
	StateValidated   = "validated"
	StateStarting    = "starting"
	StateRunning     = "running"
	StateEnabled     = "enabled" // enabled, no process (manifest-only)
	StateDegraded    = "degraded"
	StateStopping    = "stopping"
	StateDisabled    = "disabled"
	StateQuarantined = "quarantined"
	StateFailed      = "failed"
	StateExited      = "exited"
)

// LifecycleEvent is the bus type published on entering state. The payload
// carries {id, role, from, to, reason}.
func LifecycleEvent(state string) string {
	state = strings.TrimSpace(state)
	if state == "" {
		return ""
	}
	return "event.plugin." + state
}

// Extension point registration events. The payload carries {point, id}.
const (
	EventExtensionRegistered = "event.extension.registered"
	EventExtensionRevoked    = "event.extension.revoked"
)

// SelectedFamilyMembers returns the family member ids whose when.os matches
// goos. The host expands these as requires peers; a plugin never starts them
// itself.
func (m Manifest) SelectedFamilyMembers(goos string) []string {
	if m.Family == nil {
		return nil
	}
	out := make([]string, 0, 1)
	for _, mem := range m.Family.Members {
		id := strings.TrimSpace(mem.ID)
		if id == "" {
			continue
		}
		if mem.When.Matches(goos) {
			out = append(out, id)
		}
	}
	return out
}

// FamilyMemberIDs returns every declared member id regardless of OS.
func (m Manifest) FamilyMemberIDs() []string {
	if m.Family == nil {
		return nil
	}
	out := make([]string, 0, len(m.Family.Members))
	for _, mem := range m.Family.Members {
		if id := strings.TrimSpace(mem.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// Matches reports whether goos satisfies the constraint. An empty constraint
// matches every OS.
func (w When) Matches(goos string) bool {
	if len(w.OS) == 0 {
		return true
	}
	goos = strings.TrimSpace(strings.ToLower(goos))
	for _, v := range w.OS {
		if strings.EqualFold(strings.TrimSpace(v), goos) {
			return true
		}
	}
	return false
}

// OSAllowed reports whether this plugin may run on goos.
func (m Manifest) OSAllowed(goos string) bool { return m.When.Matches(goos) }

// DeclaredPoints returns the names of the extension points this plugin owns.
func (m Manifest) DeclaredPoints() []string {
	out := make([]string, 0, len(m.Extends))
	for _, p := range m.Extends {
		if n := strings.TrimSpace(p.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}
