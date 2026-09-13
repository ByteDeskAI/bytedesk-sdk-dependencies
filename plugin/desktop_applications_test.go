package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDesktopApplicationsContractIdentity(t *testing.T) {
	if DesktopApplicationsService != "desktop-applications" || DesktopApplicationsContractRevision != 1 {
		t.Fatalf("service identity = %q revision %d", DesktopApplicationsService, DesktopApplicationsContractRevision)
	}
	want := map[string]string{
		"status":   "cmd.desktop-applications.v1.status",
		"scan":     "cmd.desktop-applications.v1.scan",
		"register": "cmd.desktop-applications.v1.register",
		"open":     "cmd.desktop-applications.v1.open",
		"refresh":  "cmd.desktop-applications.v1.refresh",
		"ticket":   "cmd.desktop-applications.v1.viewer-ticket",
		"quit":     "cmd.desktop-applications.v1.quit",
	}
	got := map[string]string{
		"status": DesktopApplicationsStatusCommand, "scan": DesktopApplicationsScanCommand,
		"register": DesktopApplicationsRegisterCommand, "open": DesktopApplicationsOpenCommand,
		"refresh": DesktopApplicationsRefreshCommand, "ticket": DesktopApplicationsViewerTicketCommand,
		"quit": DesktopApplicationsQuitCommand,
	}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("%s command = %q, want %q", name, got[name], value)
		}
	}
}

func TestDesktopApplicationsPayloadValidation(t *testing.T) {
	app := DesktopApplication{
		ID:                   "claude-desktop",
		Name:                 "Claude Desktop",
		Kind:                 DesktopApplicationKindDesktop,
		Status:               DesktopApplicationReady,
		IconURL:              "/api/plugins/applications/icons/claude-desktop",
		LauncherPath:         "/usr/share/applications/claude.desktop",
		InstalledAt:          "2026-09-12T14:30:00-04:00",
		InstalledAtEstimated: true,
	}
	if err := app.Validate(); err != nil {
		t.Fatalf("valid app: %v", err)
	}
	if err := (DesktopApplication{ID: "claude", Name: "Claude", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady, Error: "contradiction"}).Validate(); err == nil {
		t.Fatal("ready app with an error accepted")
	}
	if err := (DesktopApplicationsRegisterRequest{Name: "Claude", Path: "relative"}).Validate(); err == nil {
		t.Fatal("relative host path accepted")
	}
	if err := (DesktopApplicationsOpenRequest{ApplicationID: "claude-desktop", WindowID: "0x123"}).Validate(); err != nil {
		t.Fatalf("valid open request: %v", err)
	}
	if err := (DesktopApplicationsViewerTicketRequest{SessionID: "session-1", Origin: "https://gateway.example"}).Validate(); err != nil {
		t.Fatalf("valid ticket request: %v", err)
	}
	if err := (DesktopApplicationsViewerTicketRequest{SessionID: "session-1", Origin: "https://gateway.example/path"}).Validate(); err == nil {
		t.Fatal("origin with a path accepted")
	}
	for name, app := range map[string]DesktopApplication{
		"relative launcher path": {ID: "claude", Name: "Claude", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady, LauncherPath: "claude.desktop"},
		"invalid install time":   {ID: "claude", Name: "Claude", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady, InstalledAt: "yesterday"},
		"unexplained estimate":   {ID: "claude", Name: "Claude", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady, InstalledAtEstimated: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := app.Validate(); err == nil {
				t.Fatalf("invalid application accepted: %#v", app)
			}
		})
	}
}

func TestDesktopApplicationsResultsExposeOnlyCatalogMetadata(t *testing.T) {
	result := DesktopApplicationsScanResult{
		Desktop: DesktopSessionStatus{Available: true, Message: "Connected to the existing X11 desktop."},
		Applications: []DesktopApplication{{
			ID:           "claude-desktop",
			Name:         "Claude Desktop",
			Kind:         DesktopApplicationKindDesktop,
			Status:       DesktopApplicationReady,
			IconURL:      "/api/plugins/applications/icons/claude-desktop",
			LauncherPath: "/usr/share/applications/claude.desktop",
			InstalledAt:  "2026-09-12T18:30:00Z",
		}},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("valid scan result: %v", err)
	}
	// Catalog display metadata is public to the authorized plugin. Executables,
	// DISPLAY, XAUTHORITY and desktop credentials remain host-private.
	if result.Applications[0].ID != "claude-desktop" {
		t.Fatalf("opaque application id = %q", result.Applications[0].ID)
	}
}

func TestDesktopApplicationMetadataJSON(t *testing.T) {
	minimal := DesktopApplication{ID: "codex", Name: "Codex", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady}
	raw, err := json.Marshal(minimal)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"launcherPath", "installedAt", "installedAtEstimated", "iconUrl"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("optional field %q was not omitted: %s", field, raw)
		}
	}

	populated := minimal
	populated.IconURL = "/api/plugins/applications/icons/codex"
	populated.LauncherPath = "/home/operator/.local/share/applications/codex.desktop"
	populated.InstalledAt = "2026-09-13T09:00:00Z"
	populated.InstalledAtEstimated = true
	raw, err = json.Marshal(populated)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"iconUrl":"/api/plugins/applications/icons/codex"`,
		`"launcherPath":"/home/operator/.local/share/applications/codex.desktop"`,
		`"installedAt":"2026-09-13T09:00:00Z"`,
		`"installedAtEstimated":true`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON %s does not contain %s", raw, want)
		}
	}
}
