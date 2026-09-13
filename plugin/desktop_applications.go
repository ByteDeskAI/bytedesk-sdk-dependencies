package plugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	DesktopApplicationsService          = "desktop-applications"
	DesktopApplicationsContractRevision = 1

	DesktopApplicationsStatusCommand       = "cmd.desktop-applications.v1.status"
	DesktopApplicationsScanCommand         = "cmd.desktop-applications.v1.scan"
	DesktopApplicationsRegisterCommand     = "cmd.desktop-applications.v1.register"
	DesktopApplicationsOpenCommand         = "cmd.desktop-applications.v1.open"
	DesktopApplicationsRefreshCommand      = "cmd.desktop-applications.v1.refresh"
	DesktopApplicationsViewerTicketCommand = "cmd.desktop-applications.v1.viewer-ticket"
	DesktopApplicationsQuitCommand         = "cmd.desktop-applications.v1.quit"

	DesktopApplicationsMaxBytes = 1 << 20
)

const (
	DesktopApplicationKindDesktop = "desktop"
	DesktopApplicationKindBundle  = "bundle"
	DesktopApplicationKindBinary  = "binary"

	DesktopApplicationReady   = "ready"
	DesktopApplicationMissing = "missing"
	DesktopApplicationInvalid = "invalid"

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
	ID       string `json:"id" bd:"subject"`
	Name     string `json:"name" bd:"subject"`
	Kind     string `json:"kind" bd:"subject"`
	Preset   string `json:"preset,omitempty" bd:"subject"`
	Status   string `json:"status" bd:"subject"`
	Error    string `json:"error,omitempty" bd:"subject"`
	Manual   bool   `json:"manual,omitempty" bd:"subject"`
	Revision string `json:"revision,omitempty" bd:"subject"`
	IconURL  string `json:"iconUrl,omitempty" bd:"subject"`
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
