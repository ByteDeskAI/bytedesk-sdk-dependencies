package plugin

import "testing"

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
	app := DesktopApplication{ID: "claude-desktop", Name: "Claude Desktop", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady}
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
}

func TestDesktopApplicationsResultsKeepHostDetailsOpaque(t *testing.T) {
	result := DesktopApplicationsScanResult{
		Desktop:      DesktopSessionStatus{Available: true, Message: "Connected to the existing X11 desktop."},
		Applications: []DesktopApplication{{ID: "claude-desktop", Name: "Claude Desktop", Kind: DesktopApplicationKindDesktop, Status: DesktopApplicationReady}},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("valid scan result: %v", err)
	}
	// The public result deliberately has no executable, DISPLAY, XAUTHORITY,
	// or host-path field. Stable ids are the only launch authority a plugin keeps.
	if result.Applications[0].ID != "claude-desktop" {
		t.Fatalf("opaque application id = %q", result.Applications[0].ID)
	}
}
