package plugin

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWhenMatches(t *testing.T) {
	if !(When{}).Matches("linux") {
		t.Error("an empty constraint must match every OS")
	}
	w := When{OS: []string{"linux", "darwin"}}
	if !w.Matches("darwin") || !w.Matches("DARWIN") {
		t.Error("darwin should match, case-insensitively")
	}
	if w.Matches("windows") {
		t.Error("windows should not match")
	}
}

func TestSelectedFamilyMembersFollowsGOOS(t *testing.T) {
	m := Manifest{
		ID: "user-management",
		Family: &Family{
			Selector: "runtime.os",
			Members: []FamilyMember{
				{ID: "user-management-linux", When: When{OS: []string{"linux"}}},
				{ID: "user-management-macos", When: When{OS: []string{"darwin"}}},
				{ID: "user-management-windows", When: When{OS: []string{"windows"}}},
			},
		},
	}
	for goos, want := range map[string]string{
		"linux":   "user-management-linux",
		"darwin":  "user-management-macos",
		"windows": "user-management-windows",
	} {
		got := m.SelectedFamilyMembers(goos)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: selected %v, want [%s]", goos, got, want)
		}
	}
	if got := m.FamilyMemberIDs(); len(got) != 3 {
		t.Errorf("FamilyMemberIDs = %v, want 3", got)
	}
	if m.SelectedFamilyMembers("plan9") != nil && len(m.SelectedFamilyMembers("plan9")) != 0 {
		t.Error("an unmatched OS must select no member")
	}
}

func TestLifecycleEventNames(t *testing.T) {
	if got := LifecycleEvent(StateRunning); got != "event.plugin.running" {
		t.Errorf("LifecycleEvent(running) = %q", got)
	}
	if got := LifecycleEvent(""); got != "" {
		t.Errorf("LifecycleEvent(\"\") = %q, want empty", got)
	}
}

// TestManifestRoundTripsNewFields pins the wire shape: a host on an older SDK
// must still parse a manifest written by a newer one, and vice versa.
func TestManifestRoundTripsNewFields(t *testing.T) {
	raw := []byte(`{
	  "id": "identity",
	  "role": "system",
	  "critical": true,
	  "when": {"os": ["linux"]},
	  "extends": [{"name": "auth.method", "interface": "identity.v1.AuthMethod"}],
	  "implements": [{"point": "mcp.tool", "id": "identity", "priority": 20}],
	  "launchers": [{"kind": "shell", "title": "Shell", "group": "terminals"}],
	  "family": {"selector": "runtime.os", "members": [{"id": "identity-linux", "when": {"os": ["linux"]}}]}
	}`)
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !m.Critical {
		t.Error("critical not parsed")
	}
	if !m.OSAllowed("linux") || m.OSAllowed("windows") {
		t.Error("when.os not honoured")
	}
	if got := m.DeclaredPoints(); !reflect.DeepEqual(got, []string{"auth.method"}) {
		t.Errorf("DeclaredPoints = %v", got)
	}
	if len(m.Implements) != 1 || m.Implements[0].Point != "mcp.tool" || m.Implements[0].Priority != 20 {
		t.Errorf("implements = %+v", m.Implements)
	}
	if len(m.Launchers) != 1 || m.Launchers[0].Group != "terminals" {
		t.Errorf("launcher group lost: %+v", m.Launchers)
	}
	if got := m.SelectedFamilyMembers("linux"); len(got) != 1 || got[0] != "identity-linux" {
		t.Errorf("family = %v", got)
	}

	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := ParseManifest(out)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if !reflect.DeepEqual(m, again) {
		t.Error("manifest did not survive a JSON round trip")
	}
}
