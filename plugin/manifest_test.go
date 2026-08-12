package plugin

import "testing"

func TestValidateSpawnBasename(t *testing.T) {
	m := Manifest{ID: "example", Version: "0.1.0", Spawn: true, Binary: "example"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Binary = "../evil"
	if err := m.Validate(); err == nil {
		t.Fatal("expected path reject")
	}
}

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(`{"id":"example","version":"0.2.0","spawn":true,"binary":"example-plugin"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "example" || m.Binary != "example-plugin" {
		t.Fatalf("got %+v", m)
	}
}

func TestParseManifestStringPublisher(t *testing.T) {
	m, err := ParseManifest([]byte(`{"id":"example","version":"0.2.0","publisher":"bytedesk"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Publisher == nil || m.Publisher.ID != "bytedesk" {
		t.Fatalf("publisher=%+v", m.Publisher)
	}
}

func TestSupportsTargets(t *testing.T) {
	legacy := Manifest{ID: "x", Version: "1"}
	if !legacy.Supports(TargetGateway) || legacy.Supports(TargetVault) {
		t.Fatal("empty targets default to gateway only")
	}
	both := Manifest{ID: "x", Version: "1", Targets: []string{"gateway", "vault"}}
	if !both.Supports(TargetGateway) || !both.Supports(TargetVault) {
		t.Fatal("both targets")
	}
	vaultOnly := Manifest{ID: "x", Version: "1", Targets: []string{"vault"}}
	if vaultOnly.Supports(TargetGateway) || !vaultOnly.Supports(TargetVault) {
		t.Fatal("vault only")
	}
}

func TestValidateDiscoverAllowsMissingVersion(t *testing.T) {
	m := Manifest{ID: "x", Spawn: false}
	if err := m.ValidateDiscover(); err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("strict Validate should require version")
	}
}
