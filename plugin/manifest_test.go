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

func TestValidateDiscoverAllowsMissingVersion(t *testing.T) {
	m := Manifest{ID: "x", Spawn: false}
	if err := m.ValidateDiscover(); err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("strict Validate should require version")
	}
}
