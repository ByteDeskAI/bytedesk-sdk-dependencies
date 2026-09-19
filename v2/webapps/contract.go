// Package webapps defines the public, host-served Web Apps runtime contract.
// The host owns filesystem resolution, coding sessions, processes, ports,
// preview proxying, credentials, and authorization. Callers receive opaque
// identities and safe presentation state only.
package webapps

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

const (
	ContractRevision = 1

	CommandList                = "cmd.gateway.web-apps.v1.list"
	CommandCreate              = "cmd.gateway.web-apps.v1.create"
	CommandCreationEligibility = "cmd.gateway.web-apps.v1.creation-eligibility"
	CommandConversationSend    = "cmd.gateway.web-apps.v1.conversation.send"
	CommandConversationApprove = "cmd.gateway.web-apps.v1.conversation.approve"
	CommandConversationAnswer  = "cmd.gateway.web-apps.v1.conversation.answer"
	CommandRunStop             = "cmd.gateway.web-apps.v1.run.stop"
	CommandServicesStart       = "cmd.gateway.web-apps.v1.services.start"
	CommandServicesStop        = "cmd.gateway.web-apps.v1.services.stop"
	CommandServicesLogs        = "cmd.gateway.web-apps.v1.services.logs"
	CommandPreviewResolve      = "cmd.gateway.web-apps.v1.preview.resolve"
	CommandPreviewNavigate     = "cmd.gateway.web-apps.v1.preview.navigate"
	CommandPreviewOpenExternal = "cmd.gateway.web-apps.v1.preview.open-external"

	EventChanged = "event.web-apps.v1.changed"

	MaxPayloadBytes = 64 << 10
	MaxItems        = 100
	MaxTextBytes    = 32 << 10
	MaxAttachments  = 20
)

var opaqueID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// ProjectDirectoryContext is the one allowed host-resolved directory shape.
// Its paths select agent working context; they are not filesystem authority.
type ProjectDirectoryContext = plugin.ProjectDirectoryContext

// Target pins every operation to one project checkout, app configuration
// revision, and (when relevant) coding run. A selected app changing in the UI
// cannot retarget an existing request.
type Target struct {
	ProjectID      string `json:"projectId" bd:"subject"`
	CheckoutID     string `json:"checkoutId" bd:"subject"`
	AppID          string `json:"appId" bd:"subject"`
	ConfigRevision string `json:"configRevision" bd:"subject"`
	RunID          string `json:"runId,omitempty" bd:"subject"`
}

type ProjectTarget struct {
	ProjectID  string `json:"projectId" bd:"subject"`
	CheckoutID string `json:"checkoutId" bd:"subject"`
}

type Attachment struct {
	ID        string `json:"id,omitempty" bd:"subject"`
	Name      string `json:"name" bd:"subject"`
	MediaType string `json:"mediaType,omitempty" bd:"subject"`
	Size      uint64 `json:"size,string" bd:"subject"`
}

type Provider struct {
	ID            string   `json:"id" bd:"subject"`
	Label         string   `json:"label" bd:"public"`
	EffortOptions []string `json:"effortOptions" bd:"public"`
	Attachments   bool     `json:"attachments" bd:"public"`
	Approvals     bool     `json:"approvals" bd:"public"`
	Questions     bool     `json:"questions" bd:"public"`
	Inspection    bool     `json:"inspection" bd:"public"`
}

type Message struct {
	ID          string       `json:"id" bd:"subject"`
	Role        string       `json:"role" bd:"public"`
	Kind        string       `json:"kind" bd:"public"`
	Markdown    string       `json:"markdown" bd:"subject"`
	CreatedAt   string       `json:"createdAt" bd:"subject"`
	Pending     bool         `json:"pending,omitempty" bd:"subject"`
	Attachments []Attachment `json:"attachments,omitempty" bd:"subject"`
}

type Conversation struct {
	ID         string    `json:"id" bd:"subject"`
	ReplayFrom string    `json:"replayFrom,omitempty" bd:"subject"`
	NextCursor string    `json:"nextCursor,omitempty" bd:"subject"`
	Messages   []Message `json:"messages" bd:"subject"`
}

type ToolActivity struct {
	ID      string `json:"id" bd:"subject"`
	Label   string `json:"label" bd:"subject"`
	State   string `json:"state" bd:"public"`
	Summary string `json:"summary,omitempty" bd:"subject"`
}

type Run struct {
	ID             string         `json:"id" bd:"subject"`
	State          string         `json:"state" bd:"public"`
	Mode           string         `json:"mode" bd:"public"`
	ProviderID     string         `json:"providerId" bd:"subject"`
	Effort         string         `json:"effort,omitempty" bd:"subject"`
	ConfigRevision string         `json:"configRevision" bd:"subject"`
	Activity       []ToolActivity `json:"activity" bd:"subject"`
	StartedAt      string         `json:"startedAt,omitempty" bd:"subject"`
	FinishedAt     string         `json:"finishedAt,omitempty" bd:"subject"`
}

type ServiceStatus struct {
	ID        string `json:"id" bd:"subject"`
	State     string `json:"state" bd:"public"`
	Port      uint32 `json:"port,omitempty" bd:"subject"`
	Message   string `json:"message,omitempty" bd:"subject"`
	Ready     bool   `json:"ready" bd:"public"`
	UpdatedAt string `json:"updatedAt" bd:"subject"`
}

type PreviewCapabilities struct {
	Inspection   bool `json:"inspection" bd:"public"`
	WebSocket    bool `json:"webSocket" bd:"public"`
	ExternalOpen bool `json:"externalOpen" bd:"public"`
}

type Preview struct {
	State        string              `json:"state" bd:"public"`
	URL          string              `json:"url,omitempty" bd:"subject"`
	Path         string              `json:"path,omitempty" bd:"subject"`
	ServiceID    string              `json:"serviceId,omitempty" bd:"subject"`
	Capabilities PreviewCapabilities `json:"capabilities" bd:"public"`
	Message      string              `json:"message,omitempty" bd:"subject"`
}

type App struct {
	Target       Target          `json:"target" bd:"subject"`
	Name         string          `json:"name" bd:"subject"`
	Description  string          `json:"description" bd:"subject"`
	StackHint    string          `json:"stackHint,omitempty" bd:"subject"`
	RelativeRoot string          `json:"relativeRoot" bd:"subject"`
	ConfigState  string          `json:"configState" bd:"public"`
	ConfigError  string          `json:"configError,omitempty" bd:"subject"`
	Conversation Conversation    `json:"conversation" bd:"subject"`
	Run          *Run            `json:"run,omitempty" bd:"subject"`
	Providers    []Provider      `json:"providers" bd:"subject"`
	Services     []ServiceStatus `json:"services" bd:"subject"`
	Preview      *Preview        `json:"preview,omitempty" bd:"subject"`
}

type RuntimeEvent struct {
	Cursor    string         `json:"cursor" bd:"subject"`
	Kind      string         `json:"kind" bd:"public"`
	Target    Target         `json:"target" bd:"subject"`
	CreatedAt string         `json:"createdAt" bd:"subject"`
	Message   *Message       `json:"message,omitempty" bd:"subject"`
	Run       *Run           `json:"run,omitempty" bd:"subject"`
	Service   *ServiceStatus `json:"service,omitempty" bd:"subject"`
	Preview   *Preview       `json:"preview,omitempty" bd:"subject"`
}

type ListRequest struct {
	Target ProjectTarget `json:"target" bd:"subject"`
	AppID  string        `json:"appId,omitempty" bd:"subject"`
	Cursor string        `json:"cursor,omitempty" bd:"subject"`
	Limit  int           `json:"limit,omitempty" bd:"public"`
}

type ListResult struct {
	Apps          []App          `json:"apps" bd:"subject"`
	SelectedAppID string         `json:"selectedAppId,omitempty" bd:"subject"`
	Events        []RuntimeEvent `json:"events" bd:"subject"`
	NextCursor    string         `json:"nextCursor,omitempty" bd:"subject"`
}

type CreationEligibilityRequest struct {
	Context ProjectDirectoryContext `json:"context" bd:"subject"`
}

type CreationEligibilityResult struct {
	Eligible bool   `json:"eligible" bd:"public"`
	Reason   string `json:"reason,omitempty" bd:"subject"`
}

type CreateRequest struct {
	Context     ProjectDirectoryContext `json:"context" bd:"subject"`
	Name        string                  `json:"name" bd:"subject"`
	Description string                  `json:"description" bd:"subject"`
	StackHint   string                  `json:"stackHint,omitempty" bd:"subject"`
	References  []Attachment            `json:"references" bd:"subject"`
}

type CreateResult struct {
	App  App    `json:"app" bd:"subject"`
	Href string `json:"href" bd:"subject"`
}

type ConversationSendRequest struct {
	Target      Target       `json:"target" bd:"subject"`
	Content     string       `json:"content" bd:"subject"`
	Mode        string       `json:"mode" bd:"public"`
	ProviderID  string       `json:"providerId" bd:"subject"`
	Effort      string       `json:"effort,omitempty" bd:"subject"`
	Attachments []Attachment `json:"attachments" bd:"subject"`
}

type ConversationSendResult struct {
	Message Message `json:"message" bd:"subject"`
	Run     Run     `json:"run" bd:"subject"`
	Cursor  string  `json:"cursor" bd:"subject"`
}

type ConversationApproveRequest struct {
	Target    Target `json:"target" bd:"subject"`
	RequestID string `json:"requestId" bd:"subject"`
	Approved  bool   `json:"approved" bd:"subject"`
}

type ConversationApproveResult struct {
	Message Message `json:"message" bd:"subject"`
	Run     *Run    `json:"run,omitempty" bd:"subject"`
	Cursor  string  `json:"cursor" bd:"subject"`
}

type ConversationAnswerRequest struct {
	Target    Target `json:"target" bd:"subject"`
	RequestID string `json:"requestId" bd:"subject"`
	Answer    string `json:"answer" bd:"subject"`
}

type ConversationAnswerResult struct {
	Message Message `json:"message" bd:"subject"`
	Run     *Run    `json:"run,omitempty" bd:"subject"`
	Cursor  string  `json:"cursor" bd:"subject"`
}

type RunStopRequest struct {
	Target Target `json:"target" bd:"subject"`
}
type RunStopResult struct {
	Run    Run    `json:"run" bd:"subject"`
	Cursor string `json:"cursor" bd:"subject"`
}

type ServicesStartRequest struct {
	Target     Target   `json:"target" bd:"subject"`
	ServiceIDs []string `json:"serviceIds" bd:"subject"`
}
type ServicesStartResult struct {
	Services []ServiceStatus `json:"services" bd:"subject"`
	Cursor   string          `json:"cursor" bd:"subject"`
}
type ServicesStopRequest struct {
	Target     Target   `json:"target" bd:"subject"`
	ServiceIDs []string `json:"serviceIds" bd:"subject"`
}
type ServicesStopResult struct {
	Services []ServiceStatus `json:"services" bd:"subject"`
	Cursor   string          `json:"cursor" bd:"subject"`
}

type ServicesLogsRequest struct {
	Target    Target `json:"target" bd:"subject"`
	ServiceID string `json:"serviceId,omitempty" bd:"subject"`
	Cursor    string `json:"cursor,omitempty" bd:"subject"`
	Limit     int    `json:"limit,omitempty" bd:"public"`
}
type LogEntry struct {
	Cursor    string `json:"cursor" bd:"subject"`
	ServiceID string `json:"serviceId" bd:"subject"`
	Stream    string `json:"stream" bd:"public"`
	Text      string `json:"text" bd:"subject"`
	CreatedAt string `json:"createdAt" bd:"subject"`
}
type ServicesLogsResult struct {
	Entries    []LogEntry `json:"entries" bd:"subject"`
	NextCursor string     `json:"nextCursor,omitempty" bd:"subject"`
}

type PreviewResolveRequest struct {
	Target Target `json:"target" bd:"subject"`
}
type PreviewResolveResult struct {
	Preview Preview `json:"preview" bd:"subject"`
}
type PreviewNavigateRequest struct {
	Target    Target `json:"target" bd:"subject"`
	Direction string `json:"direction" bd:"public"`
}
type PreviewNavigateResult struct {
	Preview Preview `json:"preview" bd:"subject"`
}
type PreviewOpenExternalRequest struct {
	Target Target `json:"target" bd:"subject"`
}
type PreviewOpenExternalResult struct {
	Opened bool `json:"opened" bd:"public"`
}

func (v ProjectTarget) Validate() error {
	return validateIDs(map[string]string{"projectId": v.ProjectID, "checkoutId": v.CheckoutID})
}
func (v Target) Validate() error {
	if err := validateIDs(map[string]string{"projectId": v.ProjectID, "checkoutId": v.CheckoutID, "appId": v.AppID, "configRevision": v.ConfigRevision}); err != nil {
		return err
	}
	if v.RunID != "" {
		return validateID("runId", v.RunID)
	}
	return nil
}
func (v Attachment) Validate() error {
	if v.ID != "" {
		if err := validateID("attachment.id", v.ID); err != nil {
			return err
		}
	}
	if err := text("attachment.name", v.Name, 255, true); err != nil {
		return err
	}
	return text("attachment.mediaType", v.MediaType, 255, false)
}
func (v Provider) Validate() error {
	if err := validateID("provider.id", v.ID); err != nil {
		return err
	}
	if err := text("provider.label", v.Label, 160, true); err != nil {
		return err
	}
	return stringList("provider.effortOptions", v.EffortOptions, 16, false)
}
func (v Message) Validate() error {
	if err := validateID("message.id", v.ID); err != nil {
		return err
	}
	if !oneOf(v.Role, "user", "assistant", "system", "tool") {
		return fmt.Errorf("unknown message role %q", v.Role)
	}
	if !oneOf(v.Kind, "markdown", "approval", "question", "activity", "error") {
		return fmt.Errorf("unknown message kind %q", v.Kind)
	}
	if err := text("message.markdown", v.Markdown, MaxTextBytes, true); err != nil {
		return err
	}
	if err := timestamp("message.createdAt", v.CreatedAt, false); err != nil {
		return err
	}
	return attachments(v.Attachments)
}
func (v Conversation) Validate() error {
	if err := validateID("conversation.id", v.ID); err != nil {
		return err
	}
	if v.Messages == nil || len(v.Messages) > MaxItems {
		return fmt.Errorf("conversation.messages must contain at most %d items", MaxItems)
	}
	for _, item := range v.Messages {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v ToolActivity) Validate() error {
	if err := validateID("activity.id", v.ID); err != nil {
		return err
	}
	if err := text("activity.label", v.Label, 512, true); err != nil {
		return err
	}
	if !oneOf(v.State, "queued", "running", "completed", "failed", "cancelled") {
		return fmt.Errorf("unknown activity state %q", v.State)
	}
	return text("activity.summary", v.Summary, 2048, false)
}
func (v Run) Validate() error {
	if err := validateIDs(map[string]string{"run.id": v.ID, "run.providerId": v.ProviderID, "run.configRevision": v.ConfigRevision}); err != nil {
		return err
	}
	if !oneOf(v.State, "queued", "running", "waiting_approval", "waiting_input", "completed", "failed", "cancelled", "interrupted") {
		return fmt.Errorf("unknown run state %q", v.State)
	}
	if !oneOf(v.Mode, "plan", "build") {
		return fmt.Errorf("unknown run mode %q", v.Mode)
	}
	if len(v.Activity) > MaxItems {
		return fmt.Errorf("run.activity exceeds %d items", MaxItems)
	}
	for _, item := range v.Activity {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if err := timestamp("run.startedAt", v.StartedAt, true); err != nil {
		return err
	}
	return timestamp("run.finishedAt", v.FinishedAt, true)
}
func (v ServiceStatus) Validate() error {
	if err := validateID("service.id", v.ID); err != nil {
		return err
	}
	if !oneOf(v.State, "stopped", "starting", "running", "stopping", "failed", "blocked", "lost") {
		return fmt.Errorf("unknown service state %q", v.State)
	}
	if v.Port > 65535 {
		return fmt.Errorf("service.port is invalid")
	}
	if err := text("service.message", v.Message, 2048, false); err != nil {
		return err
	}
	return timestamp("service.updatedAt", v.UpdatedAt, false)
}
func (v Preview) Validate() error {
	if !oneOf(v.State, "unconfigured", "starting", "ready", "stopped", "unavailable", "failed") {
		return fmt.Errorf("unknown preview state %q", v.State)
	}
	if err := text("preview.url", v.URL, 4096, false); err != nil {
		return err
	}
	if err := text("preview.path", v.Path, 2048, false); err != nil {
		return err
	}
	if v.ServiceID != "" {
		if err := validateID("preview.serviceId", v.ServiceID); err != nil {
			return err
		}
	}
	return text("preview.message", v.Message, 2048, false)
}
func (v App) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if err := text("app.name", v.Name, 160, true); err != nil {
		return err
	}
	if err := text("app.description", v.Description, 4096, true); err != nil {
		return err
	}
	if err := text("app.relativeRoot", v.RelativeRoot, 2048, true); err != nil {
		return err
	}
	if !oneOf(v.ConfigState, "planning", "valid", "malformed", "unsupported", "duplicate_id", "overlapping_root") {
		return fmt.Errorf("unknown app config state %q", v.ConfigState)
	}
	if err := v.Conversation.Validate(); err != nil {
		return err
	}
	if v.Run != nil {
		if err := v.Run.Validate(); err != nil {
			return err
		}
	}
	if len(v.Providers) > MaxItems || len(v.Services) > MaxItems {
		return fmt.Errorf("app collections exceed %d items", MaxItems)
	}
	for _, p := range v.Providers {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	for _, s := range v.Services {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if v.Preview != nil {
		return v.Preview.Validate()
	}
	return nil
}
func (v RuntimeEvent) Validate() error {
	if err := validateID("event.cursor", v.Cursor); err != nil {
		return err
	}
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if err := text("event.kind", v.Kind, 128, true); err != nil {
		return err
	}
	if err := timestamp("event.createdAt", v.CreatedAt, false); err != nil {
		return err
	}
	set := 0
	if v.Message != nil {
		set++
		if err := v.Message.Validate(); err != nil {
			return err
		}
	}
	if v.Run != nil {
		set++
		if err := v.Run.Validate(); err != nil {
			return err
		}
	}
	if v.Service != nil {
		set++
		if err := v.Service.Validate(); err != nil {
			return err
		}
	}
	if v.Preview != nil {
		set++
		if err := v.Preview.Validate(); err != nil {
			return err
		}
	}
	if set > 1 {
		return fmt.Errorf("event contains multiple payloads")
	}
	return nil
}
func (v ListRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if v.AppID != "" {
		if err := validateID("appId", v.AppID); err != nil {
			return err
		}
	}
	return limit(v.Limit)
}
func (v ListResult) Validate() error {
	if len(v.Apps) > MaxItems || len(v.Events) > MaxItems {
		return fmt.Errorf("list result exceeds %d items", MaxItems)
	}
	for _, a := range v.Apps {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	for _, e := range v.Events {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	return size(v)
}
func (v CreationEligibilityRequest) Validate() error { return directoryContext(v.Context) }
func (v CreationEligibilityResult) Validate() error {
	return text("eligibility.reason", v.Reason, 2048, false)
}
func (v CreateRequest) Validate() error {
	if err := directoryContext(v.Context); err != nil {
		return err
	}
	if err := text("name", v.Name, 160, true); err != nil {
		return err
	}
	if err := text("description", v.Description, 4096, true); err != nil {
		return err
	}
	if err := text("stackHint", v.StackHint, 2048, false); err != nil {
		return err
	}
	return attachments(v.References)
}
func (v CreateResult) Validate() error {
	if err := v.App.Validate(); err != nil {
		return err
	}
	if err := text("href", v.Href, 4096, true); err != nil {
		return err
	}
	return size(v)
}
func (v ConversationSendRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if err := text("content", v.Content, MaxTextBytes, true); err != nil {
		return err
	}
	if !oneOf(v.Mode, "plan", "build") {
		return fmt.Errorf("unknown conversation mode %q", v.Mode)
	}
	if err := validateID("providerId", v.ProviderID); err != nil {
		return err
	}
	return attachments(v.Attachments)
}
func (v ConversationSendResult) Validate() error {
	if err := v.Message.Validate(); err != nil {
		return err
	}
	if err := v.Run.Validate(); err != nil {
		return err
	}
	return validateID("cursor", v.Cursor)
}
func (v ConversationApproveRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	return validateID("requestId", v.RequestID)
}
func (v ConversationApproveResult) Validate() error {
	if err := v.Message.Validate(); err != nil {
		return err
	}
	if v.Run != nil {
		if err := v.Run.Validate(); err != nil {
			return err
		}
	}
	return validateID("cursor", v.Cursor)
}
func (v ConversationAnswerRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if err := validateID("requestId", v.RequestID); err != nil {
		return err
	}
	return text("answer", v.Answer, MaxTextBytes, true)
}
func (v ConversationAnswerResult) Validate() error {
	if err := v.Message.Validate(); err != nil {
		return err
	}
	if v.Run != nil {
		if err := v.Run.Validate(); err != nil {
			return err
		}
	}
	return validateID("cursor", v.Cursor)
}
func (v RunStopRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if v.Target.RunID == "" {
		return fmt.Errorf("runId is required")
	}
	return nil
}
func (v RunStopResult) Validate() error {
	if err := v.Run.Validate(); err != nil {
		return err
	}
	return validateID("cursor", v.Cursor)
}
func (v ServicesStartRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	return stringList("serviceIds", v.ServiceIDs, MaxItems, false)
}
func (v ServicesStartResult) Validate() error { return serviceResult(v.Services, v.Cursor) }
func (v ServicesStopRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	return stringList("serviceIds", v.ServiceIDs, MaxItems, false)
}
func (v ServicesStopResult) Validate() error { return serviceResult(v.Services, v.Cursor) }
func (v ServicesLogsRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if v.ServiceID != "" {
		if err := validateID("serviceId", v.ServiceID); err != nil {
			return err
		}
	}
	return limit(v.Limit)
}
func (v LogEntry) Validate() error {
	if err := validateIDs(map[string]string{"log.cursor": v.Cursor, "log.serviceId": v.ServiceID}); err != nil {
		return err
	}
	if !oneOf(v.Stream, "stdout", "stderr", "system") {
		return fmt.Errorf("unknown log stream %q", v.Stream)
	}
	if err := text("log.text", v.Text, MaxTextBytes, true); err != nil {
		return err
	}
	return timestamp("log.createdAt", v.CreatedAt, false)
}
func (v ServicesLogsResult) Validate() error {
	if len(v.Entries) > MaxItems {
		return fmt.Errorf("log result exceeds %d items", MaxItems)
	}
	for _, e := range v.Entries {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	return size(v)
}
func (v PreviewResolveRequest) Validate() error { return v.Target.Validate() }
func (v PreviewResolveResult) Validate() error  { return v.Preview.Validate() }
func (v PreviewNavigateRequest) Validate() error {
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if !oneOf(v.Direction, "back", "forward", "refresh") {
		return fmt.Errorf("unknown preview direction %q", v.Direction)
	}
	return nil
}
func (v PreviewNavigateResult) Validate() error      { return v.Preview.Validate() }
func (v PreviewOpenExternalRequest) Validate() error { return v.Target.Validate() }
func (v PreviewOpenExternalResult) Validate() error  { return nil }

func directoryContext(v ProjectDirectoryContext) error {
	return validateIDs(map[string]string{"context.projectId": v.ProjectID, "context.checkoutId": v.CheckoutID, "context.worktreeId": v.WorktreeID})
}
func validateID(name, value string) error {
	if !opaqueID.MatchString(value) {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}
func validateIDs(values map[string]string) error {
	for name, value := range values {
		if err := validateID(name, value); err != nil {
			return err
		}
	}
	return nil
}
func text(name, value string, max int, required bool) error {
	value = strings.TrimSpace(value)
	if required && value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > max || strings.ContainsRune(value, 0) {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}
func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
func timestamp(name, value string, optional bool) error {
	if optional && value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err != nil || parsed.IsZero() {
		return fmt.Errorf("%s must be RFC3339", name)
	}
	return nil
}
func attachments(values []Attachment) error {
	if values == nil || len(values) > MaxAttachments {
		return fmt.Errorf("attachments must contain at most %d items", MaxAttachments)
	}
	for _, item := range values {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return size(values)
}
func stringList(name string, values []string, max int, required bool) error {
	if values == nil || len(values) > max || (required && len(values) == 0) {
		return fmt.Errorf("invalid %s", name)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if err := validateID(name, value); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", name, value)
		}
		seen[value] = true
	}
	return nil
}
func limit(value int) error {
	if value < 0 || value > MaxItems {
		return fmt.Errorf("limit must be between 0 and %d", MaxItems)
	}
	return nil
}
func serviceResult(values []ServiceStatus, cursor string) error {
	if len(values) > MaxItems {
		return fmt.Errorf("services exceed %d items", MaxItems)
	}
	for _, item := range values {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return validateID("cursor", cursor)
}
func size(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > MaxPayloadBytes {
		return fmt.Errorf("web-apps payload exceeds %d bytes", MaxPayloadBytes)
	}
	return nil
}
