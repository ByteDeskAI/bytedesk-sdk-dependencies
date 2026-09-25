// Package codingsessions is the host-owned durable coding-agent boundary.
// Consumers select a project and policy, never an executable, PID, native ACP
// session, filesystem root or credential. The host resolves those resources.
// A task binds one provider/model/configuration at its first prompt; followups
// cannot reroute it. Prompt and lifecycle commands return promptly, with progress
// recorded as ordered events. An ACP end_turn is not task completion.
package codingsessions

import (
	"encoding/json"
	"fmt"

	check "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/payloads"
)

const (
	ContractRevision = 1
	CommandCatalog   = "cmd.gateway.coding-sessions.v1.catalog"
	CommandCreate    = "cmd.gateway.coding-sessions.v1.create"
	CommandRead      = "cmd.gateway.coding-sessions.v1.read"
	// Recover attempts capability-gated resume/load, never prompt replay.
	CommandRecover        = "cmd.gateway.coding-sessions.v1.recover"
	CommandList           = "cmd.gateway.coding-sessions.v1.list"
	CommandPrompt         = "cmd.gateway.coding-sessions.v1.prompt"
	CommandStop           = "cmd.gateway.coding-sessions.v1.stop"
	CommandEnd            = "cmd.gateway.coding-sessions.v1.end"
	CommandComplete       = "cmd.gateway.coding-sessions.v1.complete"
	CommandNewTask        = "cmd.gateway.coding-sessions.v1.new-task"
	CommandPreferences    = "cmd.gateway.coding-sessions.v1.preferences"
	CommandApprove        = "cmd.gateway.coding-sessions.v1.approve"
	CommandEvents         = "cmd.gateway.coding-sessions.v1.events"
	CommandOpenSurface    = "cmd.gateway.coding-sessions.v1.open-surface"
	EventChanged          = "event.gateway.coding-sessions.v1.changed"
	PolicyBalanced        = "balanced"
	PolicyEconomy         = "economy"
	PolicyFastest         = "fastest"
	PolicyMaximumQuality  = "maximum-quality"
	PermissionAsk         = "ask"
	PermissionAutoEdit    = "auto-edit"
	PermissionFullAccess  = "full-access"
	StatePending          = "pending"
	StateRouting          = "routing"
	StateQueued           = "queued"
	StateActive           = "active"
	StateRecovering       = "recovering"
	StateRecoveryRequired = "recovery-required"
	StateCompleted        = "completed"
	StateEnded            = "ended"
	StateFailed           = "failed"
)

type TextInput = payloads.TextInput

// Provider-specific IDs, config values, categories and extensions are preserved.
// A client must not invent a model or normalize an unknown effort into a familiar
// one. Set-config responses replace the ENTIRE options list: a model change can
// remove or change thought_level choices. ExtensionsJSON carries bounded ACP
// metadata only, never credentials or authority to execute a command.
type ConfigChoice struct {
	Value string `json:"value" bd:"subject"`
	Name  string `json:"name" bd:"public"`
	Group string `json:"group,omitempty" bd:"public"`
}
type ConfigOption struct {
	ID             string         `json:"id" bd:"subject"`
	Name           string         `json:"name" bd:"public"`
	Category       string         `json:"category,omitempty" bd:"public"`
	Type           string         `json:"type" bd:"public"`
	CurrentValue   string         `json:"currentValue" bd:"subject"`
	Choices        []ConfigChoice `json:"choices,omitempty" bd:"subject"`
	ExtensionsJSON string         `json:"extensionsJson,omitempty" bd:"subject"`
}
type ConfigValue struct {
	ID    string `json:"id" bd:"subject"`
	Value string `json:"value" bd:"subject"`
}
type Model struct {
	ID          string `json:"id" bd:"subject"`
	Name        string `json:"name" bd:"public"`
	Description string `json:"description,omitempty" bd:"public"`
}
type Capabilities struct {
	LoadSession     bool   `json:"loadSession" bd:"public"`
	ResumeSession   bool   `json:"resumeSession" bd:"public"`
	CloseSession    bool   `json:"closeSession" bd:"public"`
	SetConfigOption bool   `json:"setConfigOption" bd:"public"`
	SetModel        bool   `json:"setModel" bd:"public"`
	SetMode         bool   `json:"setMode" bd:"public"`
	ExtensionsJSON  string `json:"extensionsJson,omitempty" bd:"subject"`
}
type Provider struct {
	ID                string         `json:"id" bd:"subject"`
	Generation        string         `json:"generation" bd:"subject"`
	Name              string         `json:"name" bd:"public"`
	Availability      string         `json:"availability" bd:"public"`
	UnavailableReason string         `json:"unavailableReason,omitempty" bd:"subject"`
	Capabilities      Capabilities   `json:"capabilities" bd:"public"`
	Models            []Model        `json:"models" bd:"subject"`
	ConfigOptions     []ConfigOption `json:"configOptions" bd:"subject"`
	// Only modes the host can enforce for this provider may appear here.
	PermissionModes []string `json:"permissionModes" bd:"public"`
	ObservedAt      string   `json:"observedAt" bd:"subject"`
}
type CatalogRequest struct {
	Cursor string `json:"cursor,omitempty" bd:"subject"`
}
type CatalogResult struct {
	Providers  []Provider `json:"providers" bd:"subject"`
	NextCursor string     `json:"nextCursor,omitempty" bd:"subject"`
}
type Overrides struct {
	ProviderID   string        `json:"providerId,omitempty" bd:"subject"`
	ModelID      string        `json:"modelId,omitempty" bd:"subject"`
	ConfigValues []ConfigValue `json:"configValues,omitempty" bd:"subject"`
}
type Preferences struct {
	RoutingPolicy  string `json:"routingPolicy" bd:"public"`
	PermissionMode string `json:"permissionMode" bd:"public"`
	// Overrides apply to the NEXT task only and are consumed on that task's route.
	NextTaskOverrides *Overrides `json:"nextTaskOverrides,omitempty" bd:"subject"`
}
type Route struct {
	ProviderID         string        `json:"providerId" bd:"subject"`
	ProviderGeneration string        `json:"providerGeneration" bd:"subject"`
	ModelID            string        `json:"modelId" bd:"subject"`
	ConfigValues       []ConfigValue `json:"configValues" bd:"subject"`
	Policy             string        `json:"policy" bd:"public"`
	Source             string        `json:"source" bd:"public"`
	Reason             string        `json:"reason" bd:"subject"`
}

// WorkUnitReference explicitly selects an originating Task Management task in
// the host-authorized store of the selected project and checkout. It contains
// no store path, URL, port or caller-chosen binding. Validation checks syntax;
// the host must authorize and resolve the exact live task before accepting it.
type WorkUnitReference struct {
	TaskID string `json:"taskId" bd:"subject"`
}

// BoundWorkUnit is an output-only immutable host binding to the exact originating
// store and task identity. BindingID is opaque and is not input authority. The
// host retains private store/board/task identity, watches authoritative task
// completion, and ends the shared coding session when that work unit completes.
type BoundWorkUnit struct {
	TaskID    string `json:"taskId" bd:"subject"`
	BindingID string `json:"bindingId" bd:"subject"`
}

type Session struct {
	ID             string         `json:"id" bd:"subject"`
	TaskID         string         `json:"taskId" bd:"subject"`
	ProjectID      string         `json:"projectId" bd:"subject"`
	State          string         `json:"state" bd:"public"`
	Preferences    Preferences    `json:"preferences" bd:"subject"`
	Route          *Route         `json:"route,omitempty" bd:"subject"`
	ConfigOptions  []ConfigOption `json:"configOptions" bd:"subject"`
	ActivePromptID string         `json:"activePromptId,omitempty" bd:"subject"`
	LastSequence   uint64         `json:"lastSequence,string" bd:"subject"`
	Recovery       string         `json:"recovery" bd:"public"`
	CreatedAt      string         `json:"createdAt" bd:"subject"`
	UpdatedAt      string         `json:"updatedAt" bd:"subject"`
	// The host creates a fresh worktree from this committed checkout reference.
	// Dirty source files are excluded, reported by the host, and never deleted.
	CheckoutRef string `json:"checkoutRef" bd:"subject"`
	WorktreeRef string `json:"worktreeRef,omitempty" bd:"subject"`
	// Absent for an unlinked task. TaskID above remains the coding task's ID;
	// WorkUnit.TaskID identifies the distinct originating Task Management task.
	WorkUnit *BoundWorkUnit `json:"workUnit,omitempty" bd:"subject"`
}
type CreateRequest struct {
	ProjectID      string      `json:"projectId" bd:"subject"`
	CheckoutRef    string      `json:"checkoutRef" bd:"subject"`
	Preferences    Preferences `json:"preferences" bd:"subject"`
	IdempotencyKey string      `json:"idempotencyKey" bd:"subject"`
	// Omission creates an unlinked task; never infer a link from prompt text.
	WorkUnit *WorkUnitReference `json:"workUnit,omitempty" bd:"subject"`
}
type SessionResult struct {
	Session Session `json:"session" bd:"subject"`
}
type SessionRequest struct {
	SessionID string `json:"sessionId" bd:"subject"`
}
type ListRequest struct {
	ProjectID string `json:"projectId" bd:"subject"`
	Cursor    string `json:"cursor,omitempty" bd:"subject"`
}
type ListResult struct {
	Sessions   []Session `json:"sessions" bd:"subject"`
	NextCursor string    `json:"nextCursor,omitempty" bd:"subject"`
}
type PromptRequest struct {
	SessionID      string    `json:"sessionId" bd:"subject"`
	Content        TextInput `json:"content" bd:"subject"`
	IdempotencyKey string    `json:"idempotencyKey" bd:"subject"`
}
type PromptResult struct {
	Session  Session `json:"session" bd:"subject"`
	PromptID string  `json:"promptId" bd:"subject"`
}

// Stop cancels only the active prompt and preserves a healthy native session.
// End and Complete terminate the shared session, retaining history and worktree.
// NewTask retires the previous task, creates a new durable/native session and
// worktree, and does not silently replay old conversation or uncertain prompts.
type StopRequest struct {
	SessionID string `json:"sessionId" bd:"subject"`
	PromptID  string `json:"promptId" bd:"subject"`
}
type NewTaskRequest struct {
	SessionID      string      `json:"sessionId" bd:"subject"`
	Preferences    Preferences `json:"preferences" bd:"subject"`
	IdempotencyKey string      `json:"idempotencyKey" bd:"subject"`
	// Omission creates an unlinked new task, never inheriting the previous link.
	WorkUnit *WorkUnitReference `json:"workUnit,omitempty" bd:"subject"`
}
type PreferencesRequest struct {
	SessionID   string      `json:"sessionId" bd:"subject"`
	Preferences Preferences `json:"preferences" bd:"subject"`
}
type ApprovalOption struct {
	ID   string `json:"id" bd:"subject"`
	Name string `json:"name" bd:"public"`
	Kind string `json:"kind" bd:"public"`
}
type Approval struct {
	ID       string           `json:"id" bd:"subject"`
	PromptID string           `json:"promptId" bd:"subject"`
	Title    string           `json:"title" bd:"subject"`
	Options  []ApprovalOption `json:"options" bd:"subject"`
}
type ApproveRequest struct {
	SessionID  string `json:"sessionId" bd:"subject"`
	ApprovalID string `json:"approvalId" bd:"subject"`
	OptionID   string `json:"optionId" bd:"subject"`
}
type Message struct {
	Role    string    `json:"role" bd:"public"`
	Content TextInput `json:"content" bd:"subject"`
}
type ToolUpdate struct {
	ID      string     `json:"id" bd:"subject"`
	Title   string     `json:"title" bd:"subject"`
	Status  string     `json:"status" bd:"public"`
	Content *TextInput `json:"content,omitempty" bd:"subject"`
}
type StateUpdate struct {
	State    string `json:"state" bd:"public"`
	Recovery string `json:"recovery" bd:"public"`
}
type ConfigUpdate struct {
	Options []ConfigOption `json:"options" bd:"subject"`
}
type Failure struct {
	Code    string `json:"code" bd:"public"`
	Message string `json:"message" bd:"subject"`
}
type SessionEvent struct {
	SessionID string        `json:"sessionId" bd:"subject"`
	TaskID    string        `json:"taskId" bd:"subject"`
	Sequence  uint64        `json:"sequence,string" bd:"subject"`
	At        string        `json:"at" bd:"subject"`
	Kind      string        `json:"kind" bd:"public"`
	Message   *Message      `json:"message,omitempty" bd:"subject"`
	Tool      *ToolUpdate   `json:"tool,omitempty" bd:"subject"`
	Approval  *Approval     `json:"approval,omitempty" bd:"subject"`
	Route     *Route        `json:"route,omitempty" bd:"subject"`
	State     *StateUpdate  `json:"state,omitempty" bd:"subject"`
	Config    *ConfigUpdate `json:"config,omitempty" bd:"subject"`
	Failure   *Failure      `json:"failure,omitempty" bd:"subject"`
}

// The host persists event bytes, not transient handle IDs, and remints scoped
// viewer handles when history is read. Notifications are hints. Consumers recover ordered durable events with Events,
// deduplicate by sequence and never infer continuity from a live subscription.
type ChangedEvent struct {
	SessionID    string `json:"sessionId" bd:"subject"`
	LastSequence uint64 `json:"lastSequence,string" bd:"subject"`
}
type EventsRequest struct {
	SessionID     string `json:"sessionId" bd:"subject"`
	AfterSequence uint64 `json:"afterSequence,string" bd:"subject"`
	Limit         uint32 `json:"limit" bd:"public"`
}
type EventsResult struct {
	Events       []SessionEvent `json:"events" bd:"subject"`
	LastSequence uint64         `json:"lastSequence,string" bd:"subject"`
	HasMore      bool           `json:"hasMore" bd:"public"`
}

// OpenSurface opens or focuses the same durable session shown in Projects. It
// does not create a PTY or another agent. Explicit dock close MUST call End;
// navigation, detach, refresh and network disconnect MUST NOT call End.
type OpenSurfaceRequest struct {
	SessionID string `json:"sessionId" bd:"subject"`
}
type OpenSurfaceResult struct {
	SessionID string `json:"sessionId" bd:"subject"`
	SurfaceID string `json:"surfaceId" bd:"subject"`
	Focused   bool   `json:"focused" bd:"public"`
}

func policy(v string) error {
	return check.Enum("routing policy", v, PolicyBalanced, PolicyEconomy, PolicyFastest, PolicyMaximumQuality)
}
func permission(v string) error {
	return check.Enum("permission mode", v, PermissionAsk, PermissionAutoEdit, PermissionFullAccess)
}
func state(v string) error {
	return check.Enum("session state", v, StatePending, StateRouting, StateQueued, StateActive, StateRecovering, StateRecoveryRequired, StateCompleted, StateEnded, StateFailed)
}
func recovery(v string) error {
	return check.Enum("recovery", v, "none", "resumable", "loadable", "unavailable", "uncertain-prompt")
}
func extension(v string) error {
	if v == "" {
		return nil
	}
	if len(v) > 8192 || !json.Valid([]byte(v)) || v[0] != '{' {
		return fmt.Errorf("extensions must be a JSON object of at most 8192 bytes")
	}
	return nil
}
func (v ConfigChoice) Validate() error {
	return check.All(check.Text("choice value", v.Value, 512, true), check.Text("choice name", v.Name, 512, true), check.Text("choice group", v.Group, 512, false))
}
func (v ConfigOption) Validate() error {
	if err := check.All(check.Text("config.id", v.ID, 255, true), check.Text("config.name", v.Name, 512, true), check.Text("config.category", v.Category, 255, false), check.Text("config.type", v.Type, 255, true), check.Text("config.currentValue", v.CurrentValue, 512, false), check.Count("config choices", len(v.Choices), 0, 255), extension(v.ExtensionsJSON)); err != nil {
		return err
	}
	ids := []string{}
	for _, c := range v.Choices {
		if err := c.Validate(); err != nil {
			return err
		}
		ids = append(ids, c.Value)
	}
	return check.Unique("config choice", ids)
}
func configs(values []ConfigOption) error {
	if err := check.Count("config options", len(values), 0, 64); err != nil {
		return err
	}
	ids := []string{}
	for _, v := range values {
		if err := v.Validate(); err != nil {
			return err
		}
		ids = append(ids, v.ID)
	}
	return check.Unique("config option", ids)
}
func (v ConfigValue) Validate() error {
	return check.All(check.Text("config.id", v.ID, 255, true), check.Text("config.value", v.Value, 512, true))
}
func values(vs []ConfigValue) error {
	if err := check.Count("config values", len(vs), 0, 64); err != nil {
		return err
	}
	ids := []string{}
	for _, v := range vs {
		if err := v.Validate(); err != nil {
			return err
		}
		ids = append(ids, v.ID)
	}
	return check.Unique("config value", ids)
}
func (v Model) Validate() error {
	return check.All(check.Text("model.id", v.ID, 255, true), check.Text("model.name", v.Name, 512, true), check.Text("model.description", v.Description, 4096, false))
}
func (v Capabilities) Validate() error { return extension(v.ExtensionsJSON) }
func (v Provider) Validate() error {
	if err := check.All(check.ID("provider.id", v.ID), check.ID("provider.generation", v.Generation), check.Text("provider.name", v.Name, 512, true), check.Enum("availability", v.Availability, "unknown", "probing", "available", "unavailable"), check.Text("unavailableReason", v.UnavailableReason, 2048, false), v.Capabilities.Validate(), configs(v.ConfigOptions), check.Count("models", len(v.Models), 0, 255), check.Count("permission modes", len(v.PermissionModes), 0, 3), check.Unique("permission mode", v.PermissionModes), check.Timestamp("observedAt", v.ObservedAt)); err != nil {
		return err
	}
	ids := []string{}
	for _, m := range v.Models {
		if err := m.Validate(); err != nil {
			return err
		}
		ids = append(ids, m.ID)
	}
	if err := check.Unique("model", ids); err != nil {
		return err
	}
	for _, p := range v.PermissionModes {
		if err := permission(p); err != nil {
			return err
		}
	}
	return nil
}
func (v CatalogRequest) Validate() error { return check.OptionalID("cursor", v.Cursor) }
func (v CatalogResult) Validate() error {
	if err := check.Count("providers", len(v.Providers), 0, 100); err != nil {
		return err
	}
	ids := []string{}
	for _, p := range v.Providers {
		if err := p.Validate(); err != nil {
			return err
		}
		ids = append(ids, p.ID)
	}
	return check.All(check.Unique("provider", ids), check.OptionalID("nextCursor", v.NextCursor), check.Size(v))
}
func (v Overrides) Validate() error {
	return check.All(check.OptionalID("override.providerId", v.ProviderID), check.Text("override.modelId", v.ModelID, 255, false), values(v.ConfigValues))
}
func (v Preferences) Validate() error {
	if err := check.All(policy(v.RoutingPolicy), permission(v.PermissionMode)); err != nil {
		return err
	}
	if v.NextTaskOverrides != nil {
		return v.NextTaskOverrides.Validate()
	}
	return nil
}
func (v Route) Validate() error {
	return check.All(check.ID("route.providerId", v.ProviderID), check.ID("route.providerGeneration", v.ProviderGeneration), check.Text("route.modelId", v.ModelID, 255, true), values(v.ConfigValues), policy(v.Policy), check.Enum("route source", v.Source, "decision", "override", "fallback"), check.Text("route.reason", v.Reason, 4096, true))
}
func (v WorkUnitReference) Validate() error {
	if len(v.TaskID) < 4 || len(v.TaskID) > 64 || v.TaskID[:3] != "TM-" {
		return fmt.Errorf("workUnit.taskId must be TM- followed by a positive integer (at most 64 bytes)")
	}
	nonzero := false
	for i := 3; i < len(v.TaskID); i++ {
		if v.TaskID[i] < '0' || v.TaskID[i] > '9' {
			return fmt.Errorf("workUnit.taskId must contain ASCII digits only after TM-")
		}
		nonzero = nonzero || v.TaskID[i] != '0'
	}
	if !nonzero {
		return fmt.Errorf("workUnit.taskId must identify a positive task number")
	}
	return nil
}
func (v BoundWorkUnit) Validate() error {
	return check.All((WorkUnitReference{TaskID: v.TaskID}).Validate(), check.ID("workUnit.bindingId", v.BindingID))
}
func optionalWorkUnit(v *WorkUnitReference) error {
	if v == nil {
		return nil
	}
	return v.Validate()
}
func (v Session) Validate() error {
	if err := check.All(check.ID("session.id", v.ID), check.ID("taskId", v.TaskID), check.ID("projectId", v.ProjectID), state(v.State), v.Preferences.Validate(), configs(v.ConfigOptions), check.OptionalID("activePromptId", v.ActivePromptID), recovery(v.Recovery), check.Timestamp("createdAt", v.CreatedAt), check.Timestamp("updatedAt", v.UpdatedAt), check.Text("checkoutRef", v.CheckoutRef, 512, true), check.OptionalID("worktreeRef", v.WorktreeRef)); err != nil {
		return err
	}
	if v.Route != nil {
		if err := v.Route.Validate(); err != nil {
			return err
		}
	}
	if v.WorkUnit != nil {
		if err := v.WorkUnit.Validate(); err != nil {
			return err
		}
	}
	if v.State == StateActive && v.Route == nil {
		return fmt.Errorf("active session requires a bound route")
	}
	if v.ActivePromptID != "" && (v.State == StateEnded || v.State == StateCompleted) {
		return fmt.Errorf("terminal session cannot have an active prompt")
	}
	return nil
}
func (v CreateRequest) Validate() error {
	return check.All(check.ID("projectId", v.ProjectID), check.Text("checkoutRef", v.CheckoutRef, 512, true), v.Preferences.Validate(), check.ID("idempotencyKey", v.IdempotencyKey), optionalWorkUnit(v.WorkUnit), check.Size(v))
}
func (v SessionRequest) Validate() error { return check.ID("sessionId", v.SessionID) }
func (v SessionResult) Validate() error  { return check.All(v.Session.Validate(), check.Size(v)) }
func (v ListRequest) Validate() error {
	return check.All(check.ID("projectId", v.ProjectID), check.OptionalID("cursor", v.Cursor))
}
func (v ListResult) Validate() error {
	if err := check.Count("sessions", len(v.Sessions), 0, 100); err != nil {
		return err
	}
	for _, s := range v.Sessions {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return check.All(check.OptionalID("nextCursor", v.NextCursor), check.Size(v))
}
func (v PromptRequest) Validate() error {
	return check.All(check.ID("sessionId", v.SessionID), v.Content.Validate(), check.ID("idempotencyKey", v.IdempotencyKey), check.Size(v))
}
func (v PromptResult) Validate() error {
	return check.All(v.Session.Validate(), check.ID("promptId", v.PromptID), check.Size(v))
}
func (v StopRequest) Validate() error {
	return check.All(check.ID("sessionId", v.SessionID), check.ID("promptId", v.PromptID))
}
func (v NewTaskRequest) Validate() error {
	return check.All(check.ID("sessionId", v.SessionID), v.Preferences.Validate(), check.ID("idempotencyKey", v.IdempotencyKey), optionalWorkUnit(v.WorkUnit), check.Size(v))
}
func (v PreferencesRequest) Validate() error {
	return check.All(check.ID("sessionId", v.SessionID), v.Preferences.Validate(), check.Size(v))
}
func (v ApprovalOption) Validate() error {
	return check.All(check.ID("approval option.id", v.ID), check.Text("approval option.name", v.Name, 512, true), check.Enum("approval option.kind", v.Kind, "allow_once", "allow_always", "reject_once", "reject_always"))
}
func (v Approval) Validate() error {
	if err := check.All(check.ID("approval.id", v.ID), check.ID("approval.promptId", v.PromptID), check.Text("approval.title", v.Title, 2048, true), check.Count("approval options", len(v.Options), 1, 16)); err != nil {
		return err
	}
	ids := []string{}
	for _, o := range v.Options {
		if err := o.Validate(); err != nil {
			return err
		}
		ids = append(ids, o.ID)
	}
	return check.Unique("approval option", ids)
}
func (v ApproveRequest) Validate() error {
	return check.All(check.ID("sessionId", v.SessionID), check.ID("approvalId", v.ApprovalID), check.ID("optionId", v.OptionID))
}
func (v Message) Validate() error {
	return check.All(check.Enum("message role", v.Role, "user", "assistant", "thought", "system"), v.Content.Validate())
}
func (v ToolUpdate) Validate() error {
	if err := check.All(check.ID("tool.id", v.ID), check.Text("tool.title", v.Title, 2048, true), check.Enum("tool status", v.Status, "pending", "in_progress", "completed", "failed")); err != nil {
		return err
	}
	if v.Content != nil {
		return v.Content.Validate()
	}
	return nil
}
func (v StateUpdate) Validate() error  { return check.All(state(v.State), recovery(v.Recovery)) }
func (v ConfigUpdate) Validate() error { return configs(v.Options) }
func (v Failure) Validate() error {
	return check.All(check.ID("failure.code", v.Code), check.Text("failure.message", v.Message, 2048, true))
}
func (v SessionEvent) Validate() error {
	if v.Sequence == 0 {
		return fmt.Errorf("event sequence starts at one")
	}
	if err := check.All(check.ID("sessionId", v.SessionID), check.ID("taskId", v.TaskID), check.Timestamp("event.at", v.At)); err != nil {
		return err
	}
	count := 0
	var validate func() error
	kinds := []struct {
		kind    string
		present bool
		fn      func() error
	}{{"message", v.Message != nil, func() error { return v.Message.Validate() }}, {"tool", v.Tool != nil, func() error { return v.Tool.Validate() }}, {"approval", v.Approval != nil, func() error { return v.Approval.Validate() }}, {"route", v.Route != nil, func() error { return v.Route.Validate() }}, {"state", v.State != nil, func() error { return v.State.Validate() }}, {"config", v.Config != nil, func() error { return v.Config.Validate() }}, {"failure", v.Failure != nil, func() error { return v.Failure.Validate() }}}
	for _, k := range kinds {
		if k.present {
			count++
			if k.kind == v.Kind {
				validate = k.fn
			}
		}
	}
	if count != 1 || validate == nil {
		return fmt.Errorf("event requires exactly its kind payload")
	}
	return check.All(validate(), check.Size(v))
}
func (v ChangedEvent) Validate() error { return check.ID("sessionId", v.SessionID) }
func (v EventsRequest) Validate() error {
	if v.Limit < 1 || v.Limit > 100 {
		return fmt.Errorf("event limit must be 1 to 100")
	}
	return check.ID("sessionId", v.SessionID)
}
func (v EventsResult) Validate() error {
	if err := check.Count("events", len(v.Events), 0, 100); err != nil {
		return err
	}
	var previous uint64
	var session string
	for _, e := range v.Events {
		if err := e.Validate(); err != nil {
			return err
		}
		if e.Sequence <= previous || e.Sequence > v.LastSequence || session != "" && session != e.SessionID {
			return fmt.Errorf("events must be strictly ordered within one session")
		}
		previous = e.Sequence
		session = e.SessionID
	}
	return check.Size(v)
}
func (v OpenSurfaceRequest) Validate() error { return check.ID("sessionId", v.SessionID) }
func (v OpenSurfaceResult) Validate() error {
	return check.All(check.ID("sessionId", v.SessionID), check.ID("surfaceId", v.SurfaceID))
}

// ValidateFor rejects invented candidate IDs and unenforceable permission modes.
// It supplements wire validation; admission, current generation and task locking
// remain host checks and must be repeated immediately before starting work.
func (v Overrides) ValidateFor(p Provider, mode string) error {
	if err := check.All(v.Validate(), p.Validate(), permission(mode)); err != nil {
		return err
	}
	if p.Availability != "available" || v.ProviderID != "" && v.ProviderID != p.ID {
		return fmt.Errorf("provider is not eligible")
	}
	allowed := false
	for _, m := range p.PermissionModes {
		if m == mode {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("provider cannot enforce permission mode")
	}
	if v.ModelID != "" {
		found := false
		for _, m := range p.Models {
			if m.ID == v.ModelID {
				found = true
			}
		}
		for _, o := range p.ConfigOptions {
			if o.Category == "model" {
				for _, c := range o.Choices {
					if c.Value == v.ModelID {
						found = true
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("model is not an advertised candidate")
		}
	}
	for _, value := range v.ConfigValues {
		found := false
		for _, option := range p.ConfigOptions {
			if option.ID == value.ID {
				for _, choice := range option.Choices {
					if choice.Value == value.Value {
						found = true
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("configuration is not an advertised candidate")
		}
	}
	return nil
}
