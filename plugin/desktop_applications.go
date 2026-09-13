package plugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	DesktopApplicationsService          = "desktop-applications"
	DesktopApplicationsContractRevision = 1

	DesktopApplicationsStatusCommand       = "cmd.desktop-applications.v1.status"
	DesktopApplicationsScanCommand         = "cmd.desktop-applications.v1.scan"
	DesktopApplicationsScanV2Command       = "cmd.desktop-applications.v2.scan"
	DesktopApplicationsRegisterCommand     = "cmd.desktop-applications.v1.register"
	DesktopApplicationsOpenCommand         = "cmd.desktop-applications.v1.open"
	DesktopApplicationsRefreshCommand      = "cmd.desktop-applications.v1.refresh"
	DesktopApplicationsViewerTicketCommand = "cmd.desktop-applications.v1.viewer-ticket"
	DesktopApplicationsQuitCommand         = "cmd.desktop-applications.v1.quit"

	DesktopApplicationsMaxBytes           = 1 << 20
	DesktopApplicationsScanV2MaxBytes     = 48 << 10
	DesktopApplicationsScanV2DefaultLimit = 100
	DesktopApplicationsScanV2MaxLimit     = 200
)

const (
	DesktopApplicationKindDesktop = "desktop"
	DesktopApplicationKindBundle  = "bundle"
	DesktopApplicationKindBinary  = "binary"

	DesktopApplicationReady   = "ready"
	DesktopApplicationMissing = "missing"
	DesktopApplicationInvalid = "invalid"

	DesktopApplicationsScanV2Scanning = "scanning"
	DesktopApplicationsScanV2Complete = "complete"
	DesktopApplicationsScanV2Failed   = "failed"

	DesktopSessionStarting     = "starting"
	DesktopSessionReady        = "ready"
	DesktopSessionChooseWindow = "choose_window"
	DesktopSessionUnavailable  = "unavailable"
)

var desktopApplicationID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

type DesktopSessionStatus struct {
	Available bool   `json:"available" bd:"public"`
	Message   string `json:"message" bd:"subject"`
}

type DesktopApplication struct {
	ID                   string `json:"id" bd:"subject"`
	Name                 string `json:"name" bd:"subject"`
	Kind                 string `json:"kind" bd:"subject"`
	Preset               string `json:"preset,omitempty" bd:"subject"`
	Status               string `json:"status" bd:"subject"`
	Error                string `json:"error,omitempty" bd:"subject"`
	Manual               bool   `json:"manual,omitempty" bd:"subject"`
	Revision             string `json:"revision,omitempty" bd:"subject"`
	IconURL              string `json:"iconUrl,omitempty" bd:"subject"`
	LauncherPath         string `json:"launcherPath,omitempty" bd:"subject"`
	InstalledAt          string `json:"installedAt,omitempty" bd:"subject"`
	InstalledAtEstimated bool   `json:"installedAtEstimated,omitempty" bd:"subject"`
}

type DesktopApplicationWindow struct {
	ID    string `json:"id" bd:"subject"`
	Title string `json:"title" bd:"subject"`
}

type DesktopApplicationSession struct {
	ID            string                     `json:"id" bd:"subject"`
	ApplicationID string                     `json:"applicationId" bd:"subject"`
	Name          string                     `json:"name" bd:"subject"`
	State         string                     `json:"state" bd:"subject"`
	Windows       []DesktopApplicationWindow `json:"windows" bd:"subject"`
	ViewerURL     string                     `json:"viewerUrl" bd:"subject"`
	Error         string                     `json:"error,omitempty" bd:"subject"`
}

type DesktopApplicationsStatusRequest struct{}
type DesktopApplicationsStatusResult struct {
	Desktop DesktopSessionStatus `json:"desktop" bd:"subject"`
}

type DesktopApplicationsScanRequest struct{}
type DesktopApplicationsScanResult struct {
	Desktop      DesktopSessionStatus `json:"desktop" bd:"subject"`
	Applications []DesktopApplication `json:"applications" bd:"subject"`
}

// DesktopApplicationsScanV2Request starts or joins an asynchronous scan when
// ScanID is empty. A ScanID polls the same immutable result snapshot; Cursor
// selects a page from that snapshot.
type DesktopApplicationsScanV2Request struct {
	ScanID string `json:"scanId,omitempty" bd:"subject"`
	Cursor string `json:"cursor,omitempty" bd:"subject"`
	Limit  int    `json:"limit,omitempty" bd:"public"`
}

// DesktopApplicationsScanV2Result reports discovery separately from desktop
// availability. Complete means discovery has finished; NextCursor pages the
// resulting immutable application snapshot.
type DesktopApplicationsScanV2Result struct {
	ScanID       string               `json:"scanId" bd:"subject"`
	State        string               `json:"state" bd:"public"`
	Revision     string               `json:"revision,omitempty" bd:"subject"`
	ScannedAt    string               `json:"scannedAt,omitempty" bd:"subject"`
	Total        int                  `json:"total" bd:"public"`
	Applications []DesktopApplication `json:"applications" bd:"subject"`
	NextCursor   string               `json:"nextCursor,omitempty" bd:"subject"`
	Error        string               `json:"error,omitempty" bd:"subject"`
}

type DesktopApplicationsRegisterRequest struct {
	ApplicationID string `json:"applicationId,omitempty" bd:"subject"`
	Name          string `json:"name" bd:"subject"`
	Path          string `json:"path" bd:"subject"`
}
type DesktopApplicationsRegisterResult struct {
	Application DesktopApplication `json:"application" bd:"subject"`
}

type DesktopApplicationsOpenRequest struct {
	ApplicationID string `json:"applicationId" bd:"subject"`
	WindowID      string `json:"windowId,omitempty" bd:"subject"`
}
type DesktopApplicationsOpenResult struct {
	Session DesktopApplicationSession `json:"session" bd:"subject"`
}

type DesktopApplicationsRefreshRequest struct {
	SessionID string `json:"sessionId" bd:"subject"`
}
type DesktopApplicationsRefreshResult struct {
	Session DesktopApplicationSession `json:"session" bd:"subject"`
}

type DesktopApplicationsViewerTicketRequest struct {
	SessionID string `json:"sessionId" bd:"subject"`
	Origin    string `json:"origin" bd:"subject"`
}
type DesktopApplicationsViewerTicketResult struct {
	Ticket    string `json:"ticket" bd:"subject"`
	ExpiresIn uint32 `json:"expiresIn" bd:"public"`
}

type DesktopApplicationsQuitRequest struct {
	SessionID string `json:"sessionId" bd:"subject"`
}
type DesktopApplicationsQuitResult struct {
	OK bool `json:"ok" bd:"public"`
}

func (v DesktopSessionStatus) Validate() error {
	if err := desktopText("desktop.message", v.Message, 512, true); err != nil {
		return err
	}
	return validateDesktopSize(v)
}

func (v DesktopApplication) Validate() error {
	if !desktopApplicationID.MatchString(v.ID) {
		return fmt.Errorf("invalid application id %q", v.ID)
	}
	if err := desktopText("application.name", v.Name, 160, true); err != nil {
		return err
	}
	switch v.Kind {
	case DesktopApplicationKindDesktop, DesktopApplicationKindBundle, DesktopApplicationKindBinary:
	default:
		return fmt.Errorf("invalid application kind %q", v.Kind)
	}
	switch v.Status {
	case DesktopApplicationReady:
		if v.Error != "" {
			return fmt.Errorf("ready application cannot contain an error")
		}
	case DesktopApplicationMissing, DesktopApplicationInvalid:
		if err := desktopText("application.error", v.Error, 512, true); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid application status %q", v.Status)
	}
	for name, value := range map[string]string{"preset": v.Preset, "revision": v.Revision, "iconUrl": v.IconURL} {
		if err := desktopText("application."+name, value, 1024, false); err != nil {
			return err
		}
	}
	if v.LauncherPath != "" {
		if len(v.LauncherPath) > 4096 || !filepath.IsAbs(v.LauncherPath) || strings.ContainsAny(v.LauncherPath, "\x00\r\n") {
			return fmt.Errorf("application.launcherPath must be an absolute host path")
		}
	}
	if v.InstalledAt != "" {
		if len(v.InstalledAt) > 64 || strings.ContainsAny(v.InstalledAt, "\x00\r\n") {
			return fmt.Errorf("application.installedAt must be RFC3339")
		}
		if _, err := time.Parse(time.RFC3339, v.InstalledAt); err != nil {
			return fmt.Errorf("application.installedAt must be RFC3339: %w", err)
		}
	} else if v.InstalledAtEstimated {
		return fmt.Errorf("application.installedAtEstimated requires installedAt")
	}
	return validateDesktopSize(v)
}

func (v DesktopApplicationSession) Validate() error {
	if !desktopApplicationID.MatchString(v.ID) || !desktopApplicationID.MatchString(v.ApplicationID) {
		return fmt.Errorf("invalid application session identity")
	}
	if err := desktopText("session.name", v.Name, 160, true); err != nil {
		return err
	}
	switch v.State {
	case DesktopSessionStarting, DesktopSessionReady, DesktopSessionChooseWindow, DesktopSessionUnavailable:
	default:
		return fmt.Errorf("invalid application session state %q", v.State)
	}
	if v.Windows == nil || len(v.Windows) > 64 {
		return fmt.Errorf("session windows must be an array of at most 64 items")
	}
	for _, w := range v.Windows {
		if err := desktopText("window.id", w.ID, 128, true); err != nil {
			return err
		}
		if err := desktopText("window.title", w.Title, 512, true); err != nil {
			return err
		}
	}
	if err := desktopText("session.viewerUrl", v.ViewerURL, 1024, true); err != nil {
		return err
	}
	if err := desktopText("session.error", v.Error, 512, false); err != nil {
		return err
	}
	return validateDesktopSize(v)
}

func (DesktopApplicationsStatusRequest) Validate() error  { return nil }
func (v DesktopApplicationsStatusResult) Validate() error { return v.Desktop.Validate() }
func (DesktopApplicationsScanRequest) Validate() error    { return nil }
func (v DesktopApplicationsScanResult) Validate() error {
	if err := v.Desktop.Validate(); err != nil {
		return err
	}
	if v.Applications == nil || len(v.Applications) > 512 {
		return fmt.Errorf("applications must be an array of at most 512 items")
	}
	for _, app := range v.Applications {
		if err := app.Validate(); err != nil {
			return err
		}
	}
	return validateDesktopSize(v)
}
func (v DesktopApplicationsScanV2Request) Validate() error {
	if v.ScanID != "" {
		if err := desktopText("scanId", v.ScanID, 256, true); err != nil {
			return err
		}
	}
	if v.Cursor != "" {
		if err := desktopText("cursor", v.Cursor, 256, true); err != nil {
			return err
		}
		if v.ScanID == "" {
			return fmt.Errorf("cursor requires scanId")
		}
	}
	if v.Limit < 0 || v.Limit > DesktopApplicationsScanV2MaxLimit {
		return fmt.Errorf("limit must be zero or in 1..200")
	}
	return validateDesktopScanV2Size(v)
}
func (v DesktopApplicationsScanV2Result) Validate() error {
	if err := desktopText("scanId", v.ScanID, 256, true); err != nil {
		return err
	}
	if err := desktopText("revision", v.Revision, 256, false); err != nil {
		return err
	}
	if err := desktopText("nextCursor", v.NextCursor, 256, false); err != nil {
		return err
	}
	if err := desktopText("error", v.Error, 512, false); err != nil {
		return err
	}
	if v.ScannedAt != "" {
		if len(v.ScannedAt) > 64 || strings.ContainsAny(v.ScannedAt, "\x00\r\n") {
			return fmt.Errorf("scannedAt must be RFC3339")
		}
		if _, err := time.Parse(time.RFC3339, v.ScannedAt); err != nil {
			return fmt.Errorf("scannedAt must be RFC3339: %w", err)
		}
	}
	if v.Total < 0 {
		return fmt.Errorf("total must be nonnegative")
	}
	if v.Applications == nil || len(v.Applications) > DesktopApplicationsScanV2MaxLimit {
		return fmt.Errorf("applications must be an array of at most 200 items")
	}
	for _, app := range v.Applications {
		if err := app.Validate(); err != nil {
			return err
		}
	}
	if len(v.Applications) > v.Total {
		return fmt.Errorf("application page cannot exceed total")
	}
	switch v.State {
	case DesktopApplicationsScanV2Scanning:
		if v.Revision != "" || v.ScannedAt != "" || v.Total != 0 || len(v.Applications) != 0 || v.NextCursor != "" || v.Error != "" {
			return fmt.Errorf("scanning result cannot contain snapshot data or an error")
		}
	case DesktopApplicationsScanV2Complete:
		if strings.TrimSpace(v.Revision) == "" || v.ScannedAt == "" {
			return fmt.Errorf("complete result requires revision and scannedAt")
		}
		if v.Error != "" {
			return fmt.Errorf("complete result cannot contain an error")
		}
	case DesktopApplicationsScanV2Failed:
		if strings.TrimSpace(v.Error) == "" {
			return fmt.Errorf("failed result requires an error")
		}
		if v.Revision != "" || v.ScannedAt != "" || v.Total != 0 || len(v.Applications) != 0 || v.NextCursor != "" {
			return fmt.Errorf("failed result cannot contain snapshot data")
		}
	default:
		return fmt.Errorf("invalid scan state %q", v.State)
	}
	return validateDesktopScanV2Size(v)
}
func (v DesktopApplicationsRegisterRequest) Validate() error {
	if v.ApplicationID != "" && !desktopApplicationID.MatchString(v.ApplicationID) {
		return fmt.Errorf("invalid application id %q", v.ApplicationID)
	}
	if err := desktopText("name", v.Name, 160, false); err != nil {
		return err
	}
	if !filepath.IsAbs(v.Path) || strings.ContainsAny(v.Path, "\x00\r\n") {
		return fmt.Errorf("path must be an absolute host path")
	}
	return validateDesktopSize(v)
}
func (v DesktopApplicationsRegisterResult) Validate() error { return v.Application.Validate() }
func (v DesktopApplicationsOpenRequest) Validate() error {
	if !desktopApplicationID.MatchString(v.ApplicationID) {
		return fmt.Errorf("invalid application id %q", v.ApplicationID)
	}
	return desktopText("windowId", v.WindowID, 128, false)
}
func (v DesktopApplicationsOpenResult) Validate() error { return v.Session.Validate() }
func (v DesktopApplicationsRefreshRequest) Validate() error {
	return desktopOpaqueID("sessionId", v.SessionID)
}
func (v DesktopApplicationsRefreshResult) Validate() error { return v.Session.Validate() }
func (v DesktopApplicationsViewerTicketRequest) Validate() error {
	if err := desktopOpaqueID("sessionId", v.SessionID); err != nil {
		return err
	}
	u, err := url.Parse(v.Origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("origin must be an HTTP origin")
	}
	return validateDesktopSize(v)
}
func (v DesktopApplicationsViewerTicketResult) Validate() error {
	if err := desktopText("ticket", v.Ticket, 256, true); err != nil {
		return err
	}
	if v.ExpiresIn == 0 || v.ExpiresIn > 300 {
		return fmt.Errorf("expiresIn must be in 1..300")
	}
	return nil
}
func (v DesktopApplicationsQuitRequest) Validate() error {
	return desktopOpaqueID("sessionId", v.SessionID)
}
func (v DesktopApplicationsQuitResult) Validate() error {
	if !v.OK {
		return fmt.Errorf("quit result must acknowledge completion")
	}
	return nil
}

func desktopOpaqueID(name, value string) error {
	if !desktopApplicationID.MatchString(value) {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	return nil
}

func desktopText(name, value string, limit int, required bool) error {
	value = strings.TrimSpace(value)
	if required && value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > limit || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s exceeds its text limit", name)
	}
	return nil
}

func validateDesktopSize(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > DesktopApplicationsMaxBytes {
		return fmt.Errorf("desktop applications payload exceeds %d bytes", DesktopApplicationsMaxBytes)
	}
	return nil
}

func validateDesktopScanV2Size(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > DesktopApplicationsScanV2MaxBytes {
		return fmt.Errorf("desktop applications scan v2 payload exceeds %d bytes", DesktopApplicationsScanV2MaxBytes)
	}
	return nil
}
