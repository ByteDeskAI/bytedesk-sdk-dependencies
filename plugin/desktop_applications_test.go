package plugin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDesktopApplicationExecutablePath(t *testing.T) {
	base := DesktopApplication{ID: "native-editor", Name: "Native Editor", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady, LauncherPath: "/usr/share/applications/editor.desktop"}
	for _, path := range []string{"", "/opt/editor/editor", "/" + strings.Repeat("a", 4095)} {
		app := base
		app.ExecutablePath = path
		if err := app.Validate(); err != nil {
			t.Fatalf("valid executable path rejected: %v", err)
		}
		raw, err := json.Marshal(app)
		if err != nil {
			t.Fatal(err)
		}
		var decoded DesktopApplication
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ExecutablePath != path || decoded.LauncherPath != base.LauncherPath {
			t.Fatalf("path round trip: %+v %v", decoded, err)
		}
		if path == "" && strings.Contains(string(raw), "executablePath") {
			t.Fatal("optional absent path was serialized")
		}
	}
	for _, path := range []string{"editor", "../editor", "/" + strings.Repeat("a", 4096), "/opt/editor\x00", "/opt/editor\r", "/opt/editor\n"} {
		app := base
		app.ExecutablePath = path
		if err := app.Validate(); err == nil {
			t.Fatalf("invalid executable path accepted: %q", path)
		}
	}
	field, ok := reflect.TypeOf(base).FieldByName("ExecutablePath")
	if !ok || field.Tag.Get("bd") != "subject" || field.Tag.Get("json") != "executablePath,omitempty" {
		t.Fatalf("executable path metadata = %+v", field)
	}
}

func TestDesktopApplicationsContractIdentity(t *testing.T) {
	if DesktopApplicationsService != "desktop-applications" || DesktopApplicationsContractRevision != 1 {
		t.Fatalf("service identity = %q revision %d", DesktopApplicationsService, DesktopApplicationsContractRevision)
	}
	want := map[string]string{
		"status":   "cmd.desktop-applications.v1.status",
		"scan":     "cmd.desktop-applications.v1.scan",
		"scan-v2":  "cmd.desktop-applications.v2.scan",
		"register": "cmd.desktop-applications.v1.register",
		"open":     "cmd.desktop-applications.v1.open",
		"refresh":  "cmd.desktop-applications.v1.refresh",
		"ticket":   "cmd.desktop-applications.v1.viewer-ticket",
		"quit":     "cmd.desktop-applications.v1.quit",
	}
	got := map[string]string{
		"status": DesktopApplicationsStatusCommand, "scan": DesktopApplicationsScanCommand,
		"scan-v2":  DesktopApplicationsScanV2Command,
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

func TestDesktopApplicationsScanV2RequestValidation(t *testing.T) {
	for name, request := range map[string]DesktopApplicationsScanV2Request{
		"start default page": {},
		"start max page":     {Limit: DesktopApplicationsScanV2MaxLimit},
		"poll":               {ScanID: "scan-1"},
		"page":               {ScanID: "scan-1", Cursor: "page-2", Limit: 25},
	} {
		t.Run(name, func(t *testing.T) {
			if err := request.Validate(); err != nil {
				t.Fatalf("valid request: %v", err)
			}
		})
	}
	for name, request := range map[string]DesktopApplicationsScanV2Request{
		"negative limit": {Limit: -1},
		"large limit":    {Limit: DesktopApplicationsScanV2MaxLimit + 1},
		"orphan cursor":  {Cursor: "page-2"},
		"large scan id":  {ScanID: strings.Repeat("s", 257)},
		"large cursor":   {ScanID: "scan-1", Cursor: strings.Repeat("c", 257)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := request.Validate(); err == nil {
				t.Fatalf("invalid request accepted: %#v", request)
			}
		})
	}
	if DesktopApplicationsScanV2DefaultLimit != 100 || DesktopApplicationsScanV2MaxLimit != 200 {
		t.Fatalf("scan v2 page limits = %d/%d", DesktopApplicationsScanV2DefaultLimit, DesktopApplicationsScanV2MaxLimit)
	}
}

func TestDesktopApplicationsScanV2ResultValidation(t *testing.T) {
	app := DesktopApplication{ID: "codex", Name: "Codex", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady}
	valid := []DesktopApplicationsScanV2Result{
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Scanning, Applications: []DesktopApplication{}},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Complete, Revision: "revision-1", ScannedAt: "2026-09-13T15:00:00Z", Total: 1, Applications: []DesktopApplication{app}},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Complete, Revision: "revision-1", ScannedAt: "2026-09-13T15:00:00Z", Total: 250, Applications: []DesktopApplication{app}, NextCursor: "page-2"},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Failed, Applications: []DesktopApplication{}, Error: "scan unavailable"},
	}
	for _, result := range valid {
		if err := result.Validate(); err != nil {
			t.Errorf("valid %s result: %v", result.State, err)
		}
	}

	invalid := []DesktopApplicationsScanV2Result{
		{State: DesktopApplicationsScanV2Scanning, Applications: []DesktopApplication{}},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Scanning, Applications: nil},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Scanning, Applications: []DesktopApplication{}, Error: "not ready"},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Complete, Revision: "revision-1", Applications: []DesktopApplication{}},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Complete, Revision: "revision-1", ScannedAt: "yesterday", Applications: []DesktopApplication{}},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Complete, Revision: "revision-1", ScannedAt: "2026-09-13T15:00:00Z", Applications: []DesktopApplication{app}},
		{ScanID: "scan-1", State: DesktopApplicationsScanV2Failed, Applications: []DesktopApplication{}},
	}
	for i, result := range invalid {
		if err := result.Validate(); err == nil {
			t.Errorf("invalid result %d accepted: %#v", i, result)
		}
	}
}

func TestDesktopApplicationsScanV2PayloadLimit(t *testing.T) {
	apps := make([]DesktopApplication, 13)
	for i := range apps {
		apps[i] = DesktopApplication{
			ID:           "app-" + strings.Repeat("x", i+1),
			Name:         "Application",
			Kind:         DesktopApplicationKindDesktop,
			Status:       DesktopApplicationReady,
			LauncherPath: "/" + strings.Repeat("p", 4000),
		}
	}
	result := DesktopApplicationsScanV2Result{
		ScanID: "scan-1", State: DesktopApplicationsScanV2Complete,
		Revision: "revision-1", ScannedAt: "2026-09-13T15:00:00Z",
		Total: len(apps), Applications: apps,
	}
	if err := result.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds 49152 bytes") {
		t.Fatalf("oversize result error = %v", err)
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
