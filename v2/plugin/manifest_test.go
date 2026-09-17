package plugin

import (
	"slices"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// fullManifest is one manifest exercising every v2 field. The validator table,
// the digest golden and the digest mutation table all start from it, so a field
// added to Manifest without being added here shows up as an untested field
// rather than as a passing suite.
func fullManifest() Manifest {
	return Manifest{
		ID:           "tmux-manager",
		Version:      "2.0.0",
		Publisher:    &Publisher{ID: "bytedesk", Name: "ByteDesk"},
		Role:         RoleExtension,
		Targets:      []string{TargetGateway},
		Routes:       []string{"/p/tmux-manager/", "/p/tmux-manager/login"},
		PublicRoutes: []string{"/p/tmux-manager/login"},
		Panels:       []PanelSpec{{ID: "sessions", Kind: "iframe", URL: "/p/tmux-manager/panel"}},
		Nav:          []NavItem{{ID: "tmux", Label: "Terminals", Href: "/p/tmux-manager/"}},
		Launchers:    []LauncherSpec{{Kind: "tmux", Title: "New session"}},
		Scopes:       []string{"terminal"},
		Requires:     []Requirement{{ID: "terminal-core", Version: "^2.0.0"}},
		When:         When{OS: []string{"linux", "darwin"}},
		UI: []UIContribution{{
			ID: "sessions", Slot: SlotDefaultView, PanelID: "sessions",
			Bindings: []UIBinding{{Kind: BindCount, Event: "event.tmux-manager.sessions", Field: "count"}},
		}},
		Protocol:   &ProtocolRequirements{Major: ProtocolMajor, Required: []string{FeatureDocumentPaths}, Hooks: []string{"ready", "health"}},
		Implements: []Provider{{Point: string(PointSettingsSection), ID: "tmux-manager", Priority: 10}},
		Permissions: Permissions{
			Publish:   []bus.Pattern{"event.terminal.*"},
			Subscribe: []bus.Pattern{"event.session.>"},
			Request:   []bus.Pattern{"cmd.files.v1.list"},
		},
		Serves: []ServiceDecl{{
			Name: "tmux", Version: "1.0.0", QueueGroup: "tmux",
			Endpoints: []EndpointDecl{
				{Name: "list", Subject: "svc.tmux-manager.list", Point: string(PointSettingsSection)},
				{Name: "attach", Subject: "cmd.tmux-manager.v1.attach"},
			},
		}},
		Streams: []StreamDecl{{
			Name: "tmux-events", Subjects: []bus.Pattern{"event.tmux-manager.>"},
			MaxBytes: 1 << 20, MaxAgeSeconds: 86400, MaxMsgs: 1000,
		}},
		KV:      []KVDecl{{Name: "tmux-prefs", MaxBytes: 1 << 16, TTLSeconds: 3600, History: 3}},
		Objects: []ObjectDecl{{Name: "tmux-captures", MaxBytes: 1 << 24, TTLSeconds: 604800}},
		Needs:   []string{"durable", "kv"},
		Config: &Config{Sections: []ConfigSection{{
			ID: "tmux-manager", Title: "Terminals",
			Fields: []ConfigField{{Key: "prefs.scrollback", Kind: ConfigKindInt, Default: "5000"}},
		}}},
	}
}

// TestOwnNamespaceIsImplicitAndComplete pins the grant a plugin has by being
// that plugin. The host's grant compiler and the validator both read it, so a
// change here changes both at once — which is the reason it is a function and
// not two lists.
func TestOwnNamespaceIsImplicitAndComplete(t *testing.T) {
	g := OwnNamespace("tmux-manager")
	for _, want := range []struct {
		kind bus.GrantKind
		list []bus.Pattern
		have []bus.Pattern
	}{
		{bus.GrantPublish, []bus.Pattern{"event.tmux-manager.>", "tick.tmux-manager.>"}, g.Publish},
		{bus.GrantSubscribe, []bus.Pattern{"tick.tmux-manager.>", "_INBOX.tmux-manager.>"}, g.Subscribe},
		{bus.GrantServe, []bus.Pattern{"cmd.tmux-manager.>", "svc.tmux-manager.>"}, g.Serves},
	} {
		if !slices.Equal(want.have, want.list) {
			t.Errorf("%s = %v, want %v", want.kind, want.have, want.list)
		}
	}
	if len(g.Request) != 0 {
		t.Errorf("own namespace grants request %v; calling out is never implicit", g.Request)
	}
	if got := OwnNamespace("  "); !got.IsZero() {
		t.Errorf("OwnNamespace(blank) = %+v, want nothing", got)
	}
	// Every implicit pattern must be usable by bus.Grants.Can, or the host
	// compiles a grant the matcher cannot honour.
	if !g.Can(bus.GrantPublish, "event.tmux-manager.opened") {
		t.Error("own publish grant does not match its own event")
	}
	if g.Can(bus.GrantPublish, "event.other.opened") {
		t.Error("own publish grant reaches another plugin's events")
	}
}

// TestParseManifestValidates is the difference from v1, where ParseManifest
// decoded and returned anything that was JSON.
func TestParseManifestValidates(t *testing.T) {
	good := `{"id":"example","version":"0.2.0","spawn":true,"binary":"example-plugin"}`
	m, err := ParseManifest([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "example" || m.Binary != "example-plugin" {
		t.Fatalf("got %+v", m)
	}
	bad := `{"id":"example","version":"0.2.0","permissions":{"publish":["$SYS.>"]}}`
	if _, err := ParseManifest([]byte(bad)); err == nil {
		t.Fatal("ParseManifest accepted a manifest Validate refuses")
	}
	if _, err := ParseManifest([]byte(`{`)); err == nil {
		t.Fatal("ParseManifest accepted malformed JSON")
	}
}

func TestParseManifestStringPublisher(t *testing.T) {
	m, err := ParseManifest([]byte(`{"id":"example","version":"0.2.0","publisher":"bytedesk"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Publisher == nil || m.Publisher.ID != "bytedesk" || m.Publisher.Name != "bytedesk" {
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
	if got := legacy.RoleOrDefault(); got != RoleExtension {
		t.Fatalf("RoleOrDefault = %q", got)
	}
}

func TestPublicRouteDefaultsToRequiringASession(t *testing.T) {
	m := Manifest{Routes: []string{"/p/x/", "/p/x/login"}, PublicRoutes: []string{"/p/x/login"}}
	if !m.PublicRoute("/p/x/login") {
		t.Error("declared public route is not public")
	}
	for _, path := range []string{"/p/x/", "/p/x/secret", "", "/p/y/login"} {
		if m.PublicRoute(path) {
			t.Errorf("%q is public without being declared", path)
		}
	}
	prefix := Manifest{Routes: []string{"/p/x/pub/"}, PublicRoutes: []string{"/p/x/pub/"}}
	if !prefix.PublicRoute("/p/x/pub/asset.js") {
		t.Error("a trailing-slash route is a prefix")
	}
}

// TestNeedsFailClosed is the enable-time half: a missing capability is named and
// refused, never degraded around.
func TestNeedsFailClosed(t *testing.T) {
	m := fullManifest()
	if missing := m.MissingNeeds(bus.Capabilities{Durable: true, KV: true}); len(missing) != 0 {
		t.Fatalf("MissingNeeds on a satisfying substrate = %v", missing)
	}
	missing := m.MissingNeeds(bus.Capabilities{Durable: true})
	if !slices.Equal(missing, []string{"kv"}) {
		t.Fatalf("MissingNeeds = %v, want [kv]", missing)
	}
	if !m.OSAllowed("linux") || m.OSAllowed("windows") {
		t.Fatal("OSAllowed does not follow when.os")
	}
	if !(Manifest{}).OSAllowed("plan9") {
		t.Fatal("an empty constraint must match every OS")
	}
}

func TestProtocolMajorIsTwo(t *testing.T) {
	if ProtocolMajor != 2 {
		t.Fatalf("ProtocolMajor = %d", ProtocolMajor)
	}
	if err := Validate(fullManifest()); err != nil {
		t.Fatalf("the reference manifest must validate: %v", err)
	}
}
