package plugin

import (
	"encoding/json"
	"testing"
)

func TestProtocolRequiresExplicitSupportedCapabilities(t *testing.T) {
	host := HostCapabilities{Major: ProtocolMajor, Features: []string{FeatureRuntimeSnapshot}}
	for _, tc := range []struct {
		name string
		need ProtocolRequirements
		ok   bool
	}{
		{"legacy", ProtocolRequirements{}, true},
		{"supported", ProtocolRequirements{Major: 1, Required: []string{FeatureRuntimeSnapshot}}, true},
		{"unknown major", ProtocolRequirements{Major: 2}, false},
		{"missing feature", ProtocolRequirements{Major: 1, Required: []string{FeatureScopedHost}}, false},
		{"empty feature", ProtocolRequirements{Major: 1, Required: []string{""}}, false},
		{"unversioned feature", ProtocolRequirements{Required: []string{FeatureRuntimeSnapshot}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckProtocol(host, tc.need); (err == nil) != tc.ok {
				t.Fatalf("CheckProtocol = %v, want success %v", err, tc.ok)
			}
		})
	}
}

func TestRuntimeGenerationSurvivesJSONWithoutNumericPrecisionLoss(t *testing.T) {
	status := RuntimeStatus{ID: "sample", Installed: true, Generation: "18446744073709551615", DesiredState: DesiredEnabled, ObservedState: StateDegraded, Available: false}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["generation"] != status.Generation {
		t.Fatalf("generation changed: %#v", wire["generation"])
	}
	if wire["available"] != false {
		t.Fatal("health must not imply availability")
	}
}

func TestRuntimeManifestRejectsAmbiguousAuthorityAndForeignPanels(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Manifest)
	}{
		{"wildcard authority", func(m *Manifest) { m.Permissions = &Permissions{Request: []string{"cmd.*"}} }},
		{"duplicate authority", func(m *Manifest) { m.Permissions = &Permissions{Publish: []string{"event.x", "event.x"}} }},
		{"blank authority", func(m *Manifest) { m.Permissions = &Permissions{Subscribe: []string{" "}} }},
		{"unversioned requirement", func(m *Manifest) { m.Protocol = &ProtocolRequirements{Required: []string{FeatureScopedHost}} }},
		{"foreign panel", func(m *Manifest) { m.UI[0].PanelID = "foreign" }},
		{"unknown slot", func(m *Manifest) { m.UI[0].Slot = "execute-javascript" }},
		{"duplicate contribution", func(m *Manifest) { m.UI = append(m.UI, m.UI[0]) }},
		{"ambiguous target", func(m *Manifest) { m.UI[0].Command = "cmd.x" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Manifest{ID: "sample", Version: "1.0.0", Panels: []PanelSpec{{ID: "main", Kind: "page", URL: "/"}}, UI: []UIContribution{{ID: "default", Slot: SlotDefaultView, PanelID: "main"}}}
			if err := m.Validate(); err != nil {
				t.Fatalf("valid baseline: %v", err)
			}
			tc.edit(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
}

func TestRuntimeSnapshotRevisionIsLosslessOnWire(t *testing.T) {
	raw, err := json.Marshal(RuntimeSnapshot{Epoch: "host-a", Revision: ^uint64(0), Plugins: []RuntimeStatus{}})
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data["revision"] != "18446744073709551615" {
		t.Fatalf("revision = %#v", data["revision"])
	}
}
