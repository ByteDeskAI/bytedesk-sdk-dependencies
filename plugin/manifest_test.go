package plugin

import "testing"

// TestValidateExtendsNames is TM-250's author-time half: a declared point is
// well formed and namespaced. Whether a host. name is allowed at all is the
// host's decision at admission, because a manifest cannot prove where it runs.
func TestValidateExtendsNames(t *testing.T) {
	acme := &Publisher{ID: "acme", Name: "Acme"}
	cases := []struct {
		name      string
		publisher *Publisher
		point     string
		ok        bool
	}{
		{"publisher namespace", acme, "acme.widgets.panel", true},
		{"host namespace is well formed", nil, "host.widgets.panel", true},
		{"mixed-case publisher id", &Publisher{ID: "Acme"}, "acme.widgets.panel", true},
		{"hyphens and digits", acme, "acme.s3-v2.bucket", true},
		{"bare legacy name", acme, "files.s3", false},
		{"two-part publisher name", acme, "acme.widgets", true},
		{"host name needs an area and a name", nil, "host.files", false},
		{"single segment", acme, "acme", false},
		{"uppercase", acme, "acme.Widgets.panel", false},
		{"empty segment", acme, "acme..panel", false},
		{"another publisher's namespace", acme, "bytedesk.widgets.panel", false},
		{"publisher id is only a prefix of the segment", acme, "acmecorp.widgets.panel", false},
		{"no publisher", nil, "acme.widgets.panel", false},
		{"empty publisher id", &Publisher{}, "acme.widgets.panel", false},
	}
	for _, c := range cases {
		m := Manifest{ID: "example", Version: "1", Publisher: c.publisher, Extends: []ExtensionPoint{{Name: c.point, Interface: "x.v1"}}}
		if err := m.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: %q ok=%v err=%v", c.name, c.point, c.ok, err)
		}
	}
	// Implements is not checked: an installed package may still name a bare
	// legacy point the host aliases, and refusing it at discovery would strand it.
	legacy := Manifest{ID: "example", Version: "1", Implements: []Provider{{Point: "files.s3"}}}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("implements of a legacy bare name must still validate: %v", err)
	}
}

func TestSettingsSectionPointIsHostNamespaced(t *testing.T) {
	if SettingsSectionPoint != "host.settings.section" {
		t.Fatalf("SettingsSectionPoint = %q", SettingsSectionPoint)
	}
}

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
