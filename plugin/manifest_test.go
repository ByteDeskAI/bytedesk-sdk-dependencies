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
