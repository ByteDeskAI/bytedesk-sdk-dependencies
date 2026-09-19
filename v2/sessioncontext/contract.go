// Package sessioncontext defines the generic, host-owned contract for a
// principal-scoped interaction context. It deliberately carries opaque IDs and
// derived state only. Resolving a terminal, project, process, port, route or
// proxy remains an implementation of the host that serves these commands.
package sessioncontext

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ContractRevision = 1

	// These are public, host-served commands. They deliberately do not use
	// cmd.host.*, which is reserved for host-internal operations that plugins
	// can never invoke.
	CommandOpen    = "cmd.session-context.v1.open"
	CommandRefresh = "cmd.session-context.v1.refresh"
	CommandAction  = "cmd.session-context.v1.action"

	PurposeTaskDashboard = "task_dashboard"

	StateReady       = "ready"
	StateStarting    = "starting"
	StateUnavailable = "unavailable"

	MaxBytes        = 32 << 10
	MaxActions      = 16
	MaxActionInputs = 16
)

var opaqueID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// OpenRequest asks the host to resolve one opaque target for a declared
// purpose. The caller's workload identity, generation and subject lease are
// substrate-stamped and intentionally absent from this wire type.
type OpenRequest struct {
	Purpose  string `json:"purpose" bd:"subject"`
	TargetID string `json:"targetId" bd:"subject"`
}

// Context is the host-derived view a plugin may use for presentation. IDs are
// opaque. State and actions are closed vocabulary; there is no path, port,
// URL, process handle, proxy target, credential, or arbitrary metadata field.
type Context struct {
	ID        string          `json:"id" bd:"subject"`
	Purpose   string          `json:"purpose" bd:"subject"`
	Revision  string          `json:"revision" bd:"subject"`
	ExpiresAt string          `json:"expiresAt" bd:"subject"`
	State     string          `json:"state" bd:"subject"`
	Summary   string          `json:"summary,omitempty" bd:"subject"`
	Actions   []AllowedAction `json:"actions" bd:"subject"`
}

// AllowedAction is an approved host capability. A plugin can request only one of the
// action IDs returned in its current Context; action routing stays host-owned.
type AllowedAction struct {
	ID      string `json:"id" bd:"subject"`
	Label   string `json:"label" bd:"subject"`
	Enabled bool   `json:"enabled" bd:"subject"`
}

type OpenResult struct {
	Context Context `json:"context" bd:"subject"`
}

type RefreshRequest struct {
	ContextID string `json:"contextId" bd:"subject"`
}

type RefreshResult struct {
	Context Context `json:"context" bd:"subject"`
}

// ActionInput is bounded text, not a JSON blob. The host must define the
// accepted names and values per Action ID and refuse everything else.
type ActionInput struct {
	Name  string `json:"name" bd:"subject"`
	Value string `json:"value" bd:"subject"`
}

type ActionRequest struct {
	ContextID      string        `json:"contextId" bd:"subject"`
	Revision       string        `json:"revision" bd:"subject"`
	ActionID       string        `json:"actionId" bd:"subject"`
	IdempotencyKey string        `json:"idempotencyKey" bd:"subject"`
	Inputs         []ActionInput `json:"inputs" bd:"subject"`
}

type ActionResult struct {
	Context Context `json:"context" bd:"subject"`
}

func (v OpenRequest) Validate() error {
	if v.Purpose != PurposeTaskDashboard {
		return fmt.Errorf("unknown session-context purpose %q", v.Purpose)
	}
	if err := validateOpaqueID("targetId", v.TargetID); err != nil {
		return err
	}
	return validateSize(v)
}

func (v Context) Validate() error {
	if err := validateOpaqueID("context.id", v.ID); err != nil {
		return err
	}
	if v.Purpose != PurposeTaskDashboard {
		return fmt.Errorf("unknown session-context purpose %q", v.Purpose)
	}
	if err := validateOpaqueID("context.revision", v.Revision); err != nil {
		return err
	}
	if parsed, err := time.Parse(time.RFC3339, v.ExpiresAt); err != nil || parsed.IsZero() {
		return fmt.Errorf("context.expiresAt must be RFC3339")
	}
	switch v.State {
	case StateReady, StateStarting, StateUnavailable:
	default:
		return fmt.Errorf("unknown session-context state %q", v.State)
	}
	if err := validateText("context.summary", v.Summary, 512, false); err != nil {
		return err
	}
	if v.Actions == nil || len(v.Actions) > MaxActions {
		return fmt.Errorf("context.actions must contain at most %d items", MaxActions)
	}
	seen := make(map[string]struct{}, len(v.Actions))
	for _, action := range v.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[action.ID]; duplicate {
			return fmt.Errorf("duplicate context action %q", action.ID)
		}
		seen[action.ID] = struct{}{}
	}
	return validateSize(v)
}

func (v AllowedAction) Validate() error {
	if err := validateOpaqueID("action.id", v.ID); err != nil {
		return err
	}
	return validateText("action.label", v.Label, 160, true)
}

func (v OpenResult) Validate() error     { return v.Context.Validate() }
func (v RefreshRequest) Validate() error { return validateOpaqueID("contextId", v.ContextID) }
func (v RefreshResult) Validate() error  { return v.Context.Validate() }

func (v ActionInput) Validate() error {
	if err := validateOpaqueID("input.name", v.Name); err != nil {
		return err
	}
	return validateText("input.value", v.Value, 1024, true)
}

func (v ActionRequest) Validate() error {
	for name, value := range map[string]string{
		"contextId": v.ContextID, "revision": v.Revision, "actionId": v.ActionID, "idempotencyKey": v.IdempotencyKey,
	} {
		if err := validateOpaqueID(name, value); err != nil {
			return err
		}
	}
	if v.Inputs == nil || len(v.Inputs) > MaxActionInputs {
		return fmt.Errorf("action inputs must contain at most %d items", MaxActionInputs)
	}
	seen := make(map[string]struct{}, len(v.Inputs))
	for _, input := range v.Inputs {
		if err := input.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[input.Name]; duplicate {
			return fmt.Errorf("duplicate action input %q", input.Name)
		}
		seen[input.Name] = struct{}{}
	}
	return validateSize(v)
}

func (v ActionResult) Validate() error { return v.Context.Validate() }

func validateOpaqueID(name, value string) error {
	if !opaqueID.MatchString(value) {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}

func validateText(name, value string, limit int, required bool) error {
	value = strings.TrimSpace(value)
	if required && value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > limit || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}

func validateSize(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > MaxBytes {
		return fmt.Errorf("session-context payload exceeds %d bytes", MaxBytes)
	}
	return nil
}
