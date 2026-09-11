package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	TerminalPresentationPoint       = HostPointNamespace + "terminal.presentation"
	TerminalPresentationInterface   = "terminal.presentation.v1"
	TerminalPresentationCommand     = "terminal.presentation.project.v1"
	TerminalPresentationBindingRead = "terminal.presentation.binding.read.v1"

	TerminalPresentationMaxBytes     = 1 << 20
	TerminalPresentationMaxTerminals = 512
	TerminalPresentationMaxGroups    = 8
	TerminalPresentationMaxBadges    = 4
	TerminalPresentationDeadlineMS   = 2000
)

const (
	TerminalBindingNone = "none"
	TerminalBindingTmux = "tmux"
	FreshnessFresh      = "fresh"
	FreshnessStale      = "stale"
	FreshnessUnknown    = "unknown"
)

type PresentationLease struct {
	HostEpoch    string `json:"hostEpoch" bd:"public"`
	PluginID     string `json:"pluginId" bd:"public"`
	ProviderID   string `json:"providerId" bd:"public"`
	Generation   string `json:"generation" bd:"public"`
	SubjectLease string `json:"subjectLease" bd:"subject"`
	RequestID    string `json:"requestId" bd:"public"`
	ViewRevision string `json:"viewRevision" bd:"public"`
}

type TerminalBindingContext struct {
	Kind string                   `json:"kind" bd:"subject"`
	Tmux *TmuxPresentationContext `json:"tmux,omitempty" bd:"subject"`
}

type TmuxPresentationContext struct {
	RepositoryKey  string `json:"repositoryKey" bd:"subject"`
	ServerKey      string `json:"serverKey" bd:"subject"`
	ServerPID      string `json:"serverPid" bd:"subject"`
	SessionID      string `json:"sessionId" bd:"subject"`
	SessionCreated string `json:"sessionCreated" bd:"subject"`
	PaneID         string `json:"paneId" bd:"subject"`
	PanePID        string `json:"panePid" bd:"subject"`
}

type PresentationTerminal struct {
	TerminalID string                 `json:"terminalId" bd:"subject"`
	Context    TerminalBindingContext `json:"context" bd:"subject"`
}

type PresentationRequest struct {
	Lease     PresentationLease      `json:"lease" bd:"public"`
	Terminals []PresentationTerminal `json:"terminals" bd:"subject"`
}

type PresentationGroup struct {
	ID    string `json:"id" bd:"subject"`
	Label string `json:"label" bd:"subject"`
}

type PresentationBadge struct {
	Label string `json:"label" bd:"subject"`
	Icon  string `json:"icon,omitempty" bd:"subject"`
}

type PresentationItem struct {
	TerminalID  string              `json:"terminalId" bd:"subject"`
	AgentID     string              `json:"agentId,omitempty" bd:"subject"`
	DisplayName string              `json:"displayName,omitempty" bd:"subject"`
	GroupPath   []PresentationGroup `json:"groupPath" bd:"subject"`
	Badges      []PresentationBadge `json:"badges" bd:"subject"`
	Priority    int32               `json:"priority" bd:"subject"`
	Freshness   string              `json:"freshness" bd:"subject"`
}

type PresentationResult struct {
	Lease    PresentationLease  `json:"lease" bd:"public"`
	MaxAgeMS uint32             `json:"maxAgeMs" bd:"public"`
	Items    []PresentationItem `json:"items" bd:"subject"`
}

// TerminalPresentationProvider is an optional adapter over CommandHandler.
type TerminalPresentationProvider interface {
	Project(context.Context, PresentationRequest) (PresentationResult, error)
}

var presentationIcon = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
var repositoryKey = regexp.MustCompile(`^[a-f0-9]{16}$`)
var positiveDecimal = regexp.MustCompile(`^[1-9][0-9]*$`)

func DecodePresentationRequest(r io.Reader) (PresentationRequest, error) {
	var v PresentationRequest
	err := decodePresentation(r, &v)
	if err == nil {
		err = ValidatePresentationRequest(v)
	}
	return v, err
}

func DecodePresentationResult(r io.Reader, request PresentationRequest, current []PresentationTerminal) (PresentationResult, error) {
	var v PresentationResult
	err := decodePresentation(r, &v)
	if err == nil {
		err = ValidatePresentationResult(request, current, v)
	}
	return v, err
}

func decodePresentation(r io.Reader, dst any) error {
	limited := io.LimitReader(r, TerminalPresentationMaxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(raw) > TerminalPresentationMaxBytes {
		return fmt.Errorf("terminal presentation payload exceeds %d bytes", TerminalPresentationMaxBytes)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("invalid terminal presentation payload: %w", err)
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid terminal presentation payload: trailing data")
	}
	return nil
}

func ValidatePresentationRequest(v PresentationRequest) error {
	if err := validatePresentationLease(v.Lease); err != nil {
		return err
	}
	if v.Terminals == nil || len(v.Terminals) > TerminalPresentationMaxTerminals {
		return fmt.Errorf("terminals must be an array of at most %d items", TerminalPresentationMaxTerminals)
	}
	seen := map[string]bool{}
	for _, terminal := range v.Terminals {
		if err := presentationText("terminalId", terminal.TerminalID, 128, true); err != nil {
			return err
		}
		if seen[terminal.TerminalID] {
			return fmt.Errorf("duplicate terminalId %q", terminal.TerminalID)
		}
		seen[terminal.TerminalID] = true
		if err := validateTerminalContext(terminal.Context); err != nil {
			return err
		}
	}
	return validatePresentationSize(v)
}

// ValidatePresentationResult validates a complete replacement against both the
// captured request and the current principal-authorized terminal incarnations.
func ValidatePresentationResult(request PresentationRequest, current []PresentationTerminal, result PresentationResult) error {
	if err := ValidatePresentationRequest(request); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if result.Lease != request.Lease {
		return fmt.Errorf("presentation lease does not match the outstanding request")
	}
	if result.MaxAgeMS < 1 || result.MaxAgeMS > 30000 {
		return fmt.Errorf("maxAgeMs must be in 1..30000")
	}
	if result.Items == nil || len(result.Items) > TerminalPresentationMaxTerminals || len(result.Items) > len(request.Terminals) {
		return fmt.Errorf("items must be an array bounded by the requested subset")
	}
	requested, authorized := map[string]PresentationTerminal{}, map[string]PresentationTerminal{}
	for _, terminal := range request.Terminals {
		requested[terminal.TerminalID] = terminal
	}
	if len(current) > TerminalPresentationMaxTerminals {
		return fmt.Errorf("current principal subset exceeds %d terminals", TerminalPresentationMaxTerminals)
	}
	for _, terminal := range current {
		if err := presentationText("current terminalId", terminal.TerminalID, 128, true); err != nil {
			return err
		}
		if _, exists := authorized[terminal.TerminalID]; exists {
			return fmt.Errorf("current principal subset contains duplicate terminalId %q", terminal.TerminalID)
		}
		if err := validateTerminalContext(terminal.Context); err != nil {
			return fmt.Errorf("current terminalId %q: %w", terminal.TerminalID, err)
		}
		authorized[terminal.TerminalID] = terminal
	}
	seen, labels := map[string]bool{}, map[string]string{}
	for _, item := range result.Items {
		want, ok := requested[item.TerminalID]
		if !ok || seen[item.TerminalID] {
			return fmt.Errorf("unknown, unauthorized, or duplicate terminalId %q", item.TerminalID)
		}
		if now, ok := authorized[item.TerminalID]; !ok || !sameTerminalIncarnation(want, now) {
			return fmt.Errorf("terminalId %q is not currently authorized at the requested incarnation", item.TerminalID)
		}
		seen[item.TerminalID] = true
		if item.AgentID != "" {
			if err := presentationText("agentId", item.AgentID, 128, true); err != nil {
				return err
			}
		}
		if item.DisplayName != "" {
			if err := presentationText("displayName", item.DisplayName, 160, true); err != nil {
				return err
			}
		}
		if item.GroupPath == nil || len(item.GroupPath) > TerminalPresentationMaxGroups {
			return fmt.Errorf("groupPath must be an array of at most %d segments", TerminalPresentationMaxGroups)
		}
		prefix := ""
		for _, group := range item.GroupPath {
			if err := presentationText("group.id", group.ID, 128, true); err != nil {
				return err
			}
			if err := presentationText("group.label", group.Label, 160, true); err != nil {
				return err
			}
			prefix += "\x00" + group.ID
			if label, exists := labels[prefix]; exists && label != group.Label {
				return fmt.Errorf("group path prefix %q has inconsistent labels", prefix)
			}
			labels[prefix] = group.Label
		}
		if item.Badges == nil || len(item.Badges) > TerminalPresentationMaxBadges {
			return fmt.Errorf("badges must be an array of at most %d entries", TerminalPresentationMaxBadges)
		}
		for _, badge := range item.Badges {
			if err := presentationText("badge.label", badge.Label, 80, true); err != nil {
				return err
			}
			if badge.Icon != "" && !presentationIcon.MatchString(badge.Icon) {
				return fmt.Errorf("badge.icon is not an ASCII icon token")
			}
		}
		if item.Priority < -1000 || item.Priority > 1000 {
			return fmt.Errorf("priority must be in -1000..1000")
		}
		switch item.Freshness {
		case FreshnessFresh, FreshnessStale, FreshnessUnknown:
		default:
			return fmt.Errorf("unknown freshness %q", item.Freshness)
		}
	}
	return validatePresentationSize(result)
}

func validatePresentationLease(v PresentationLease) error {
	for name, value := range map[string]string{"hostEpoch": v.HostEpoch, "pluginId": v.PluginID, "providerId": v.ProviderID, "generation": v.Generation, "subjectLease": v.SubjectLease, "requestId": v.RequestID, "viewRevision": v.ViewRevision} {
		if err := presentationText(name, value, 128, true); err != nil {
			return err
		}
	}
	return nil
}

func validateTerminalContext(v TerminalBindingContext) error {
	switch v.Kind {
	case TerminalBindingNone:
		if v.Tmux != nil {
			return fmt.Errorf("kind none forbids tmux context")
		}
	case TerminalBindingTmux:
		if v.Tmux == nil {
			return fmt.Errorf("kind tmux requires tmux context")
		}
		if !repositoryKey.MatchString(v.Tmux.RepositoryKey) {
			return fmt.Errorf("repositoryKey must be 16 lowercase hex")
		}
		if err := presentationText("serverKey", v.Tmux.ServerKey, 128, true); err != nil {
			return err
		}
		for name, value := range map[string]string{"serverPid": v.Tmux.ServerPID, "sessionId": v.Tmux.SessionID, "sessionCreated": v.Tmux.SessionCreated, "paneId": v.Tmux.PaneID, "panePid": v.Tmux.PanePID} {
			if !positiveDecimal.MatchString(value) {
				return fmt.Errorf("%s must be a positive ASCII decimal int64", name)
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n <= 0 {
				return fmt.Errorf("%s must be a positive ASCII decimal int64", name)
			}
		}
	default:
		return fmt.Errorf("unknown terminal binding kind %q", v.Kind)
	}
	return nil
}

// sameTerminalIncarnation compares bindings by value. The tmux context is a
// pointer, so == would refuse a freshly re-resolved binding with identical fields.
func sameTerminalIncarnation(a, b PresentationTerminal) bool {
	if a.TerminalID != b.TerminalID || a.Context.Kind != b.Context.Kind || (a.Context.Tmux == nil) != (b.Context.Tmux == nil) {
		return false
	}
	return a.Context.Tmux == nil || *a.Context.Tmux == *b.Context.Tmux
}

func presentationText(name, value string, max int, nonempty bool) error {
	if !utf8.ValidString(value) || len(value) > max || (nonempty && value == "") || strings.ContainsRune(value, 0) || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s must be valid text of %d UTF-8 bytes or fewer without controls", name, max)
	}
	return nil
}

func validatePresentationSize(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > TerminalPresentationMaxBytes {
		return fmt.Errorf("terminal presentation payload exceeds %d bytes", TerminalPresentationMaxBytes)
	}
	return nil
}
