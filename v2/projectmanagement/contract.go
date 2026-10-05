// Package projectmanagement owns the shared task and knowledge connector model.
// Hosts resolve providers and authenticate actors. These records carry authority
// references, never credentials, local paths, or permission to impersonate users.
package projectmanagement

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
)

const ContractRevision = 1

// Scope identifies shared project authority, independent of a checkout or host registry.
type Scope struct {
	ProjectID          string `json:"projectId" bd:"subject"`
	EnvironmentID      string `json:"environmentId" bd:"subject"`
	TenantID           string `json:"tenantId" bd:"subject"`
	BindingID          string `json:"bindingId" bd:"subject"`
	BindingRevision    string `json:"bindingRevision" bd:"public"`
	ProviderGeneration string `json:"providerGeneration" bd:"public"`
	CapabilityRevision string `json:"capabilityRevision" bd:"public"`
}

// Binding is the shared authority selected for a logical project and slot.
// A machine-local default cannot replace this record while writers are active.
type Binding struct {
	Scope      Scope       `json:"scope" bd:"subject"`
	Slot       string      `json:"slot" bd:"public"`
	ProviderID string      `json:"providerId" bd:"subject"`
	Resource   ResourceRef `json:"resource" bd:"subject"`
	State      string      `json:"state" bd:"public"`
}

// ResourceRef preserves the provider's opaque native identifier. Kind is an
// advertised resource kind, not a vendor-specific Go type.
type ResourceRef struct {
	ProviderID     string `json:"providerId" bd:"subject"`
	InstallationID string `json:"installationId" bd:"subject"`
	Kind           string `json:"kind" bd:"public"`
	ID             string `json:"id" bd:"subject"`
	Key            string `json:"key,omitempty" bd:"subject"`
	URL            string `json:"url,omitempty" bd:"subject"`
}

// ActingContext is minted by the host after authenticating the bus caller.
// OriginalActorID is provenance only; it never grants impersonation authority.
type ActingContext struct {
	ActorID         string `json:"actorId" bd:"subject"`
	ConnectionID    string `json:"connectionId" bd:"subject"`
	DelegatedBy     string `json:"delegatedBy,omitempty" bd:"subject"`
	OriginalActorID string `json:"originalActorId,omitempty" bd:"subject"`
	ExecutionMode   string `json:"executionMode" bd:"public"`
}

type Invocation struct {
	ID        string        `json:"id" bd:"subject"`
	Scope     Scope         `json:"scope" bd:"subject"`
	Actor     ActingContext `json:"actor" bd:"subject"`
	ExpiresAt string        `json:"expiresAt" bd:"public"`
}

type Capability struct {
	Operation     string   `json:"operation" bd:"public"`
	Revision      int      `json:"revision" bd:"public"`
	ResourceKinds []string `json:"resourceKinds,omitempty" bd:"public"`
	Formats       []string `json:"formats,omitempty" bd:"public"`
	Supported     bool     `json:"supported" bd:"public"`
	Authorized    bool     `json:"authorized" bd:"public"`
	Reason        string   `json:"reason,omitempty" bd:"public"`
	Concurrency   string   `json:"concurrency" bd:"public"`
	Offline       string   `json:"offline" bd:"public"`
	MaxBatchItems int      `json:"maxBatchItems,omitempty" bd:"public"`
}

type Capabilities struct {
	Revision   string         `json:"revision" bd:"public"`
	Operations []Capability   `json:"operations" bd:"public"`
	Transport  Transport      `json:"transport" bd:"public"`
	Recovery   RecoveryPolicy `json:"recovery" bd:"public"`
}

// Transport describes the negotiated remote product interface. MCP discovery
// does not imply that every advertised API operation is available over MCP.
type Transport struct {
	Kind            string `json:"kind" bd:"public"`
	ProtocolVersion string `json:"protocolVersion,omitempty" bd:"public"`
	CatalogRevision string `json:"catalogRevision" bd:"public"`
	Discovery       string `json:"discovery" bd:"public"`
}

type RecoveryPolicy struct {
	Idempotency             string `json:"idempotency" bd:"public"`
	DeduplicationSeconds    int64  `json:"deduplicationSeconds,string" bd:"public"`
	ReceiptRetentionSeconds int64  `json:"receiptRetentionSeconds,string" bd:"public"`
	LookupByOperationID     bool   `json:"lookupByOperationId" bd:"public"`
	ReconcileBeforeReplay   bool   `json:"reconcileBeforeReplay" bd:"public"`
}

// SupportsReplay refuses operations older than the provider's deduplication
// and recovery windows. Zero retention means indefinite only for a durable
// provider with operation lookup. Unknown outcomes must be recovered first.
func (p RecoveryPolicy) SupportsReplay(ageSeconds int64) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if ageSeconds < 0 || !p.LookupByOperationID || !p.ReconcileBeforeReplay || p.Idempotency == "none" {
		return fmt.Errorf("durable replay is unavailable")
	}
	if p.Idempotency == "windowed" && (p.DeduplicationSeconds == 0 || ageSeconds >= p.DeduplicationSeconds) {
		return fmt.Errorf("operation exceeds deduplication window")
	}
	if p.ReceiptRetentionSeconds > 0 && ageSeconds >= p.ReceiptRetentionSeconds || p.ReceiptRetentionSeconds == 0 && p.Idempotency != "durable" {
		return fmt.Errorf("operation exceeds recovery window")
	}
	return nil
}

// RichBody keeps the native source format. A renderer is never the migration source.
type RichBody struct {
	Format    string `json:"format" bd:"public"`
	Version   string `json:"version" bd:"public"`
	Source    string `json:"source" bd:"subject"`
	PlainText string `json:"plainText,omitempty" bd:"subject"`
	Rendered  string `json:"rendered,omitempty" bd:"subject"`
	Lossy     bool   `json:"lossy" bd:"public"`
}

// FieldValue is typed extension data. JSON is a bounded valid JSON value, not
// an arbitrary provider request. Unknown namespaced fields survive transfer.
type FieldValue struct {
	Name string `json:"name" bd:"public"`
	Type string `json:"type" bd:"public"`
	JSON string `json:"json" bd:"subject"`
}

type FieldSchema struct {
	Name          string       `json:"name" bd:"public"`
	Type          string       `json:"type" bd:"public"`
	Required      bool         `json:"required" bd:"public"`
	Editable      bool         `json:"editable" bd:"public"`
	Multiple      bool         `json:"multiple" bd:"public"`
	AllowedValues []FieldValue `json:"allowedValues,omitempty" bd:"subject"`
}

type PageRequest struct {
	Cursor string `json:"cursor,omitempty" bd:"subject"`
	Limit  int    `json:"limit" bd:"public"`
}

type PageInfo struct {
	NextCursor  string `json:"nextCursor,omitempty" bd:"subject"`
	Total       int64  `json:"total,string" bd:"public"`
	TotalKind   string `json:"totalKind" bd:"public"`
	Consistency string `json:"consistency" bd:"public"`
	ObservedAt  string `json:"observedAt" bd:"public"`
}

type Filter struct {
	Field    string       `json:"field" bd:"public"`
	Operator string       `json:"operator" bd:"public"`
	Values   []FieldValue `json:"values,omitempty" bd:"subject"`
}

type Sort struct {
	Field     string `json:"field" bd:"public"`
	Direction string `json:"direction" bd:"public"`
}

type Query struct {
	Text           string       `json:"text,omitempty" bd:"subject"`
	Filters        []Filter     `json:"filters,omitempty" bd:"subject"`
	Sort           []Sort       `json:"sort,omitempty" bd:"public"`
	SavedView      *ResourceRef `json:"savedView,omitempty" bd:"subject"`
	NativeLanguage string       `json:"nativeLanguage,omitempty" bd:"public"`
	NativeQuery    string       `json:"nativeQuery,omitempty" bd:"subject"`
	Page           PageRequest  `json:"page" bd:"subject"`
}

// Mutation is an immutable durable operation identity. A retry preserves ID
// and payload. Offline persistence does not mean remote acceptance.
type Mutation struct {
	OperationID           string   `json:"operationId" bd:"subject"`
	ExpectedRevision      string   `json:"expectedRevision,omitempty" bd:"public"`
	SourceBindingRevision string   `json:"sourceBindingRevision" bd:"public"`
	DependsOn             []string `json:"dependsOn,omitempty" bd:"subject"`
	Origin                string   `json:"origin" bd:"public"`
	CreatedAt             string   `json:"createdAt" bd:"public"`
}

type Problem struct {
	Code              string `json:"code" bd:"public"`
	Message           string `json:"message" bd:"subject"`
	Field             string `json:"field,omitempty" bd:"public"`
	Retry             string `json:"retry" bd:"public"`
	RetryAfterSeconds int    `json:"retryAfterSeconds,omitempty" bd:"public"`
}

type ItemReceipt struct {
	ItemID   string       `json:"itemId" bd:"subject"`
	State    string       `json:"state" bd:"public"`
	Resource *ResourceRef `json:"resource,omitempty" bd:"subject"`
	Revision string       `json:"revision,omitempty" bd:"public"`
	Problem  *Problem     `json:"problem,omitempty" bd:"subject"`
}

type Receipt struct {
	OperationID    string        `json:"operationId" bd:"subject"`
	State          string        `json:"state" bd:"public"`
	RemoteAccepted bool          `json:"remoteAccepted" bd:"public"`
	Resource       *ResourceRef  `json:"resource,omitempty" bd:"subject"`
	Revision       string        `json:"revision,omitempty" bd:"public"`
	Items          []ItemReceipt `json:"items,omitempty" bd:"subject"`
	Problem        *Problem      `json:"problem,omitempty" bd:"subject"`
	RecoveryToken  string        `json:"recoveryToken,omitempty" bd:"subject"`
	ObservedAt     string        `json:"observedAt" bd:"public"`
	Conflict       *Conflict     `json:"conflict,omitempty" bd:"subject"`
}

// Conflict preserves both sides; resolution is an explicit new operation.
type Conflict struct {
	BaseRevision      string       `json:"baseRevision" bd:"public"`
	RemoteRevision    string       `json:"remoteRevision" bd:"public"`
	LocalBody         *RichBody    `json:"localBody,omitempty" bd:"subject"`
	RemoteBody        *RichBody    `json:"remoteBody,omitempty" bd:"subject"`
	LocalFields       []FieldValue `json:"localFields,omitempty" bd:"subject"`
	RemoteFields      []FieldValue `json:"remoteFields,omitempty" bd:"subject"`
	PayloadRef        string       `json:"payloadRef,omitempty" bd:"subject"`
	ResolutionOptions []string     `json:"resolutionOptions" bd:"public"`
}

// OfflineOperation describes a host-durable queued request. PayloadRef points
// to the exact typed request plus checksum in host storage, not executable code.
// Actor is retained provenance and must be reauthorized before replay.
type OfflineOperation struct {
	Scope           Scope         `json:"scope" bd:"subject"`
	Actor           ActingContext `json:"actor" bd:"subject"`
	Mutation        Mutation      `json:"mutation" bd:"subject"`
	Operation       string        `json:"operation" bd:"public"`
	PayloadRef      string        `json:"payloadRef" bd:"subject"`
	PayloadChecksum string        `json:"payloadChecksum" bd:"public"`
	Receipt         Receipt       `json:"receipt" bd:"subject"`
	PersistedAt     string        `json:"persistedAt" bd:"public"`
	SessionID       string        `json:"sessionId,omitempty" bd:"subject"`
	NativeID        string        `json:"nativeId,omitempty" bd:"subject"`
}

type Change struct {
	Resource    ResourceRef `json:"resource" bd:"subject"`
	Revision    string      `json:"revision" bd:"public"`
	ActorID     string      `json:"actorId" bd:"subject"`
	Origin      string      `json:"origin" bd:"public"`
	OperationID string      `json:"operationId,omitempty" bd:"subject"`
	Kind        string      `json:"kind" bd:"public"`
	OccurredAt  string      `json:"occurredAt" bd:"public"`
}

// Transfer grants bytes only. Headers and commands are intentionally absent;
// hosts bind credentials and signed URLs to the opaque ID outside this record.
// URI is a credential-free resource locator: query strings and fragments are refused.
type Transfer struct {
	ID         string      `json:"id" bd:"subject"`
	Resource   ResourceRef `json:"resource" bd:"subject"`
	Direction  string      `json:"direction" bd:"public"`
	Method     string      `json:"method" bd:"public"`
	URI        string      `json:"uri" bd:"subject"`
	ExpiresAt  string      `json:"expiresAt" bd:"public"`
	Size       int64       `json:"size,string" bd:"public"`
	MediaType  string      `json:"mediaType" bd:"public"`
	Checksum   string      `json:"checksum" bd:"public"`
	Provenance string      `json:"provenance" bd:"subject"`
	Verified   bool        `json:"verified" bd:"public"`
}

type MigrationEntity struct {
	// SourceRecordID distinguishes source files, duplicate native IDs, and
	// historical versions without rewriting the opaque native Source.ID.
	SourceRecordID string       `json:"sourceRecordId" bd:"subject"`
	Source         ResourceRef  `json:"source" bd:"subject"`
	Destination    *ResourceRef `json:"destination,omitempty" bd:"subject"`
	Revision       string       `json:"revision" bd:"public"`
	Checksum       string       `json:"checksum" bd:"public"`
	Record         *Transfer    `json:"record,omitempty" bd:"subject"`
	Problems       []Problem    `json:"problems,omitempty" bd:"subject"`
}

// Migration is a resumable lossless manifest. Activation is host-owned and
// requires verification of every entity, relation, version, and byte stream.
type Migration struct {
	ID                string            `json:"id" bd:"subject"`
	Source            Scope             `json:"source" bd:"subject"`
	Destination       Scope             `json:"destination" bd:"subject"`
	State             string            `json:"state" bd:"public"`
	SnapshotRevision  string            `json:"snapshotRevision" bd:"public"`
	Cursor            string            `json:"cursor,omitempty" bd:"subject"`
	Entities          []MigrationEntity `json:"entities,omitempty" bd:"subject"`
	InventoryChecksum string            `json:"inventoryChecksum" bd:"public"`
	Lossless          bool              `json:"lossless" bd:"public"`
	WritesPaused      bool              `json:"writesPaused" bd:"public"`
}

type Attachment struct {
	Ref       ResourceRef `json:"ref" bd:"subject"`
	Name      string      `json:"name" bd:"subject"`
	MediaType string      `json:"mediaType" bd:"public"`
	Size      int64       `json:"size,string" bd:"public"`
	Checksum  string      `json:"checksum" bd:"public"`
	Revision  string      `json:"revision" bd:"public"`
}

type Comment struct {
	Ref       ResourceRef  `json:"ref" bd:"subject"`
	Body      RichBody     `json:"body" bd:"subject"`
	AuthorID  string       `json:"authorId" bd:"subject"`
	CreatedAt string       `json:"createdAt" bd:"public"`
	Revision  string       `json:"revision" bd:"public"`
	Parent    *ResourceRef `json:"parent,omitempty" bd:"subject"`
	Anchor    *FieldValue  `json:"anchor,omitempty" bd:"subject"`
	Resolved  bool         `json:"resolved" bd:"public"`
}

// CommentInput deliberately has no author or historical timestamps. The host
// authenticates the author; imported provenance belongs to migration records.
type CommentInput struct {
	Body   RichBody     `json:"body" bd:"subject"`
	Parent *ResourceRef `json:"parent,omitempty" bd:"subject"`
	Anchor *FieldValue  `json:"anchor,omitempty" bd:"subject"`
}

type Relation struct {
	Kind   string      `json:"kind" bd:"public"`
	Source ResourceRef `json:"source" bd:"subject"`
	Target ResourceRef `json:"target" bd:"subject"`
	Type   string      `json:"type" bd:"public"`
}

type Grant struct {
	PrincipalID   string `json:"principalId" bd:"subject"`
	PrincipalKind string `json:"principalKind" bd:"public"`
	Operation     string `json:"operation" bd:"public"`
	Effect        string `json:"effect" bd:"public"`
	Inherited     bool   `json:"inherited" bd:"public"`
}

func (s Scope) Validate() error {
	return contractcheck.All(contractcheck.ID("projectId", s.ProjectID), contractcheck.ID("environmentId", s.EnvironmentID), contractcheck.Text("tenantId", s.TenantID, 512, true), contractcheck.ID("bindingId", s.BindingID), required("bindingRevision", s.BindingRevision), required("providerGeneration", s.ProviderGeneration), required("capabilityRevision", s.CapabilityRevision))
}
func (b Binding) Validate() error {
	if b.ProviderID != b.Resource.ProviderID {
		return fmt.Errorf("binding resource belongs to another provider")
	}
	return contractcheck.All(b.Scope.Validate(), b.Resource.Validate(), contractcheck.Enum("slot", b.Slot, "project.tasks", "project.knowledge"), contractcheck.ID("providerId", b.ProviderID), contractcheck.Enum("binding state", b.State, "active", "paused", "migrating", "blocked"))
}
func (r ResourceRef) Validate() error {
	return contractcheck.All(contractcheck.ID("providerId", r.ProviderID), required("installationId", r.InstallationID), required("kind", r.Kind), required("id", r.ID), optionalURL(r.URL))
}

// SameResource compares stable identity, ignoring mutable display key and URL.
func (r ResourceRef) SameResource(other ResourceRef) bool {
	return r.ProviderID == other.ProviderID && r.InstallationID == other.InstallationID && r.Kind == other.Kind && r.ID == other.ID
}
func (a ActingContext) Validate() error {
	return contractcheck.All(contractcheck.ID("actorId", a.ActorID), contractcheck.ID("connectionId", a.ConnectionID), contractcheck.OptionalID("delegatedBy", a.DelegatedBy), contractcheck.OptionalID("originalActorId", a.OriginalActorID), contractcheck.Enum("executionMode", a.ExecutionMode, "interactive", "delegated", "background", "replay"))
}
func (i Invocation) Validate() error {
	return contractcheck.All(contractcheck.ID("invocationId", i.ID), i.Scope.Validate(), i.Actor.Validate(), contractcheck.Timestamp("expiresAt", i.ExpiresAt))
}
func (i Invocation) Matches(s Scope) error {
	if i.Scope != s {
		return fmt.Errorf("invocation scope does not match request fence")
	}
	return i.Validate()
}
func (c Capability) Validate() error {
	if c.Revision < 1 || c.MaxBatchItems < 0 || c.Authorized && !c.Supported {
		return fmt.Errorf("invalid capability support, authorization, or limits")
	}
	return contractcheck.All(required("operation", c.Operation), contractcheck.Enum("concurrency", c.Concurrency, "revision", "serialized", "none"), contractcheck.Enum("offline", c.Offline, "queue", "read-cache", "online-only"))
}
func (c Capabilities) Validate() error {
	if err := required("revision", c.Revision); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, op := range c.Operations {
		if seen[op.Operation] {
			return fmt.Errorf("duplicate capability operation")
		}
		seen[op.Operation] = true
		if err := op.Validate(); err != nil {
			return err
		}
	}
	return contractcheck.All(c.Transport.Validate(), c.Recovery.Validate())
}
func (t Transport) Validate() error {
	return contractcheck.All(contractcheck.Enum("transport", t.Kind, "mcp", "api", "cli", "local"), required("catalogRevision", t.CatalogRevision), contractcheck.Enum("discovery", t.Discovery, "static", "dynamic"))
}
func (p RecoveryPolicy) Validate() error {
	if p.DeduplicationSeconds < 0 || p.ReceiptRetentionSeconds < 0 {
		return fmt.Errorf("negative recovery retention")
	}
	return contractcheck.Enum("idempotency", p.Idempotency, "durable", "windowed", "none")
}
func (b RichBody) Validate() error {
	return contractcheck.All(required("format", b.Format), required("version", b.Version))
}
func (f FieldValue) Validate() error {
	if !json.Valid([]byte(f.JSON)) {
		return fmt.Errorf("field %s must contain valid JSON", f.Name)
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(f.JSON))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	validType := false
	switch f.Type {
	case "string", "date", "datetime":
		_, validType = decoded.(string)
	case "number":
		_, validType = decoded.(json.Number)
	case "boolean":
		_, validType = decoded.(bool)
	case "null":
		validType = decoded == nil
	case "object", "reference":
		_, validType = decoded.(map[string]any)
	case "array":
		_, validType = decoded.([]any)
	}
	if !validType {
		return fmt.Errorf("field %s JSON does not match type %s", f.Name, f.Type)
	}
	return contractcheck.All(required("field name", f.Name), contractcheck.Enum("field type", f.Type, "string", "number", "boolean", "null", "object", "array", "reference", "date", "datetime"))
}
func (f FieldSchema) Validate() error {
	return contractcheck.All(required("field name", f.Name), contractcheck.Enum("field type", f.Type, "string", "number", "boolean", "null", "object", "array", "reference", "date", "datetime"))
}

// Failure validates a typed error response without requiring absent success
// fields. The entire response still obeys the encoded wire size limit.
func Failure(value any, problem *Problem) error {
	return contractcheck.All(contractcheck.Size(value), problem.Validate())
}
func (p PageRequest) Validate() error { return contractcheck.Count("page limit", p.Limit, 1, 200) }
func (p PageInfo) Validate() error {
	if p.Total < 0 {
		return fmt.Errorf("negative total")
	}
	return contractcheck.All(contractcheck.Enum("totalKind", p.TotalKind, "unknown", "estimate", "exact"), contractcheck.Enum("consistency", p.Consistency, "snapshot", "eventual", "live", "cached"), contractcheck.Timestamp("observedAt", p.ObservedAt))
}
func (f Filter) Validate() error {
	return contractcheck.All(required("filter field", f.Field), contractcheck.Enum("filter operator", f.Operator, "eq", "ne", "in", "not-in", "lt", "lte", "gt", "gte", "contains", "exists"))
}
func (s Sort) Validate() error {
	return contractcheck.All(required("sort field", s.Field), contractcheck.Enum("sort direction", s.Direction, "asc", "desc"))
}
func (q Query) Validate() error {
	if (q.NativeQuery == "") != (q.NativeLanguage == "") {
		return fmt.Errorf("native query and language must be supplied together")
	}
	return q.Page.Validate()
}
func (m Mutation) Validate() error {
	if err := contractcheck.All(contractcheck.ID("operationId", m.OperationID), required("sourceBindingRevision", m.SourceBindingRevision), required("origin", m.Origin), contractcheck.Timestamp("createdAt", m.CreatedAt), contractcheck.Unique("dependency", m.DependsOn)); err != nil {
		return err
	}
	for _, id := range m.DependsOn {
		if id == m.OperationID {
			return fmt.Errorf("operation cannot depend on itself")
		}
		if err := contractcheck.ID("dependency", id); err != nil {
			return err
		}
	}
	return nil
}
func (m Mutation) Matches(s Scope) error {
	if m.SourceBindingRevision != s.BindingRevision {
		return fmt.Errorf("stale source binding revision")
	}
	return m.Validate()
}
func (p Problem) Validate() error {
	if p.RetryAfterSeconds < 0 {
		return fmt.Errorf("negative retry delay")
	}
	return contractcheck.All(contractcheck.Enum("problem code", p.Code, "authentication-required", "denied", "not-found", "unsupported", "validation", "conflict", "rate-limited", "unavailable", "stale-binding", "partial", "unknown-outcome"), contractcheck.Enum("retry", p.Retry, "never", "after-delay", "reauthorize", "reconcile", "recover-operation"), contractcheck.Text("message", p.Message, 4096, true))
}
func validReceipt(state string) error {
	return contractcheck.Enum("receipt state", state, "saved-local", "pending", "accepted", "rejected", "conflicted", "partial", "unknown")
}
func (r ItemReceipt) Validate() error {
	if r.State == "accepted" && r.Problem != nil {
		return fmt.Errorf("accepted item cannot also fail")
	}
	return contractcheck.All(required("itemId", r.ItemID), validReceipt(r.State))
}
func (r Receipt) Validate() error {
	if r.RemoteAccepted != (r.State == "accepted") {
		return fmt.Errorf("remoteAccepted must be true exactly for an accepted receipt")
	}
	if r.State == "unknown" && r.RecoveryToken == "" {
		return fmt.Errorf("unknown outcome requires recovery token")
	}
	if r.State == "conflicted" && r.Conflict == nil {
		return fmt.Errorf("conflicted receipt must preserve both sides")
	}
	if r.State != "conflicted" && r.Conflict != nil {
		return fmt.Errorf("conflict payload requires conflicted receipt")
	}
	if r.State == "accepted" && r.Problem != nil {
		return fmt.Errorf("accepted operation cannot also fail")
	}
	if r.State == "partial" && len(r.Items) == 0 {
		return fmt.Errorf("partial outcome requires item receipts")
	}
	return contractcheck.All(contractcheck.ID("operationId", r.OperationID), validReceipt(r.State), contractcheck.Timestamp("observedAt", r.ObservedAt))
}
func (r Receipt) ValidateFor(m Mutation) error {
	if r.OperationID != m.OperationID {
		return fmt.Errorf("receipt belongs to another operation")
	}
	return contractcheck.All(r.Validate(), Validate(r))
}
func (c Conflict) Validate() error {
	if c.PayloadRef == "" && ((c.LocalBody == nil && len(c.LocalFields) == 0) || (c.RemoteBody == nil && len(c.RemoteFields) == 0)) {
		return fmt.Errorf("conflict requires preserved content or payload reference")
	}
	return contractcheck.All(required("baseRevision", c.BaseRevision), required("remoteRevision", c.RemoteRevision), contractcheck.Count("resolution options", len(c.ResolutionOptions), 1, 16))
}
func (o OfflineOperation) Validate() error {
	if o.Mutation.OperationID != o.Receipt.OperationID {
		return fmt.Errorf("offline receipt operation mismatch")
	}
	if (o.SessionID == "") != (o.NativeID == "") {
		return fmt.Errorf("native mapping requires both session and native identity")
	}
	if OnlineOnly(o.Operation) {
		return fmt.Errorf("operation cannot be queued offline")
	}
	return contractcheck.All(o.Scope.Validate(), o.Actor.Validate(), o.Receipt.ValidateFor(o.Mutation), o.Mutation.Matches(o.Scope), required("operation", o.Operation), contractcheck.ID("payloadRef", o.PayloadRef), required("payloadChecksum", o.PayloadChecksum), contractcheck.Timestamp("persistedAt", o.PersistedAt))
}
func (c Change) Validate() error {
	return contractcheck.All(c.Resource.Validate(), required("revision", c.Revision), contractcheck.ID("actorId", c.ActorID), required("origin", c.Origin), contractcheck.OptionalID("operationId", c.OperationID), contractcheck.Enum("change kind", c.Kind, "created", "updated", "deleted", "restored", "permissions", "migrated"), contractcheck.Timestamp("occurredAt", c.OccurredAt))
}
func (t Transfer) Validate() error {
	if t.Size < 0 {
		return fmt.Errorf("negative transfer size")
	}
	if err := contractcheck.All(contractcheck.ID("transferId", t.ID), t.Resource.Validate(), contractcheck.Enum("direction", t.Direction, "upload", "download"), contractcheck.Enum("transfer method", t.Method, "mcp-resource", "mcp-chunk", "authorized-https"), contractcheck.Timestamp("expiresAt", t.ExpiresAt), required("mediaType", t.MediaType), required("checksum", t.Checksum), required("provenance", t.Provenance)); err != nil {
		return err
	}
	u, err := url.Parse(t.URI)
	if err != nil || u.Scheme == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || t.Method == "authorized-https" && (u.Scheme != "https" || u.Host == "") {
		return fmt.Errorf("invalid transfer URI")
	}
	return nil
}

func (e MigrationEntity) Validate() error {
	if err := contractcheck.All(required("sourceRecordId", e.SourceRecordID), e.Source.Validate(), required("revision", e.Revision), required("checksum", e.Checksum)); err != nil {
		return err
	}
	if e.Destination != nil {
		if err := e.Destination.Validate(); err != nil {
			return err
		}
	}
	if e.Record != nil {
		if !e.Record.Resource.SameResource(e.Source) || e.Record.Checksum != e.Checksum {
			return fmt.Errorf("migration record does not match source and checksum")
		}
		return e.Record.Validate()
	}
	return nil
}

func (m Migration) Validate() error {
	if m.Source.ProjectID != m.Destination.ProjectID || m.Source.EnvironmentID != m.Destination.EnvironmentID || m.Source.TenantID != m.Destination.TenantID {
		return fmt.Errorf("migration must retain logical project, environment, and tenant identity")
	}
	if m.State == "verified" && (!m.Lossless || !m.WritesPaused) {
		return fmt.Errorf("verified migration requires lossless data and paused writes")
	}
	seen := map[string]bool{}
	for _, entity := range m.Entities {
		if err := entity.Validate(); err != nil {
			return err
		}
		if seen[entity.SourceRecordID] {
			return fmt.Errorf("duplicate migration source record")
		}
		seen[entity.SourceRecordID] = true
		if m.State == "verified" && (entity.Destination == nil || len(entity.Problems) != 0 || entity.Record == nil || !entity.Record.Verified) {
			return fmt.Errorf("verified migration requires mapped entities and verified source records without failures")
		}
	}
	return contractcheck.All(contractcheck.ID("migrationId", m.ID), m.Source.Validate(), m.Destination.Validate(), contractcheck.Enum("migration state", m.State, "planned", "exporting", "importing", "verifying", "verified", "failed", "aborted"), required("snapshotRevision", m.SnapshotRevision), required("inventoryChecksum", m.InventoryChecksum))
}
func (a Attachment) Validate() error {
	if a.Size < 0 {
		return fmt.Errorf("negative attachment size")
	}
	return contractcheck.All(a.Ref.Validate(), required("name", a.Name), required("mediaType", a.MediaType), required("revision", a.Revision), required("checksum", a.Checksum))
}
func (c Comment) Validate() error {
	return contractcheck.All(c.Ref.Validate(), c.Body.Validate(), contractcheck.ID("authorId", c.AuthorID), contractcheck.Timestamp("createdAt", c.CreatedAt), required("revision", c.Revision))
}
func (r Relation) Validate() error {
	if (r.Kind == "hierarchy" || r.Kind == "dependency") && r.Source.SameResource(r.Target) {
		return fmt.Errorf("hierarchy and dependency cannot reference themselves")
	}
	return contractcheck.All(contractcheck.Enum("relation kind", r.Kind, "hierarchy", "link", "dependency", "knowledge", "membership"), r.Source.Validate(), r.Target.Validate(), required("relation type", r.Type))
}
func (g Grant) Validate() error {
	return contractcheck.All(required("principalId", g.PrincipalID), contractcheck.Enum("principal kind", g.PrincipalKind, "user", "group", "role", "public"), required("operation", g.Operation), contractcheck.Enum("grant effect", g.Effect, "allow", "deny"))
}
func required(name, value string) error { return contractcheck.Text(name, value, 1024, true) }
func optionalURL(value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("invalid canonical URL")
	}
	return nil
}

// Validate checks the complete bounded wire graph, including nested semantic
// validators. Root validators call this function; root itself is skipped to
// avoid recursion. This is wire validation, never authorization.
func Validate(value any) error {
	if err := contractcheck.Size(value); err != nil {
		return err
	}
	return walk(reflect.ValueOf(value), true)
}
func walk(v reflect.Value, root bool) error {
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return walk(v.Elem(), root)
	}
	if !root && v.CanInterface() {
		if check, ok := v.Interface().(interface{ Validate() error }); ok {
			if err := check.Validate(); err != nil {
				return err
			}
		}
	}
	switch v.Kind() {
	case reflect.Struct:
		for n := 0; n < v.NumField(); n++ {
			if err := walk(v.Field(n), false); err != nil {
				return fmt.Errorf("%s: %w", v.Type().Field(n).Name, err)
			}
		}
	case reflect.Slice:
		if v.Len() > 256 {
			return fmt.Errorf("collection exceeds 256 items")
		}
		for n := 0; n < v.Len(); n++ {
			if err := walk(v.Index(n), false); err != nil {
				return err
			}
		}
	case reflect.String:
		return contractcheck.Text("string", v.String(), 32768, false)
	}
	return nil
}

// RequireOperation checks support, current authorization and revision fences.
// offline admits a queued mutation, not a cached read: read-cache never grants
// write authority. Hosts check cached-read policy separately and must still
// authenticate caller, binding, connection, and provider generation.
func RequireOperation(scope Scope, caps Capabilities, operation string, offline bool) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.CapabilityRevision != caps.Revision {
		return fmt.Errorf("stale capability revision")
	}
	if offline && OnlineOnly(operation) {
		return fmt.Errorf("operation requires online authority")
	}
	for _, c := range caps.Operations {
		if c.Operation == operation {
			if !c.Supported || !c.Authorized {
				return fmt.Errorf("operation unavailable: %s", operation)
			}
			if offline && c.Offline != "queue" {
				return fmt.Errorf("operation cannot be queued offline")
			}
			return c.Validate()
		}
	}
	return fmt.Errorf("unsupported operation: %s", operation)
}

// OnlineOnly protects authority-changing operations even when a provider
// accidentally advertises them as queueable.
func OnlineOnly(operation string) bool {
	switch operation {
	case "claim.acquire", "claim.renew", "agent.complete", "permissions.set", "admin.permissions", "public-link.enable", "public-link.disable":
		return true
	}
	return false
}

// NamespaceField makes provider-specific extensions explicit without accepting
// raw remote request payloads in the common operation model.
func NamespaceField(provider, name string) string {
	return strings.TrimSpace(provider) + ":" + strings.TrimSpace(name)
}
