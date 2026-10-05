// Package projecttasks defines the task slot of Project Management. UI, CLI,
// MCP and native terminal bridges call this host facade; only the host selects
// and invokes a project.tasks provider. Vendor APIs remain behind that provider.
package projecttasks

import (
	"fmt"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
	pm "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/projectmanagement"
)

const ContractRevision = 1

// Project is the remote project/container binding, never a filesystem path.
type Project struct {
	Ref         ResourceRef  `json:"ref" bd:"subject"`
	Name        string       `json:"name" bd:"subject"`
	Description *RichBody    `json:"description,omitempty" bd:"subject"`
	Revision    string       `json:"revision" bd:"public"`
	Fields      []FieldValue `json:"fields,omitempty" bd:"subject"`
}
type WorkItem struct {
	Ref        ResourceRef  `json:"ref" bd:"subject"`
	Project    ResourceRef  `json:"project" bd:"subject"`
	Type       string       `json:"type" bd:"public"`
	Title      string       `json:"title" bd:"subject"`
	Body       *RichBody    `json:"body,omitempty" bd:"subject"`
	Status     string       `json:"status" bd:"public"`
	Revision   string       `json:"revision" bd:"public"`
	AssigneeID string       `json:"assigneeId,omitempty" bd:"subject"`
	Parent     *ResourceRef `json:"parent,omitempty" bd:"subject"`
	Labels     []string     `json:"labels,omitempty" bd:"subject"`
	Fields     []FieldValue `json:"fields,omitempty" bd:"subject"`
	Agent      *AgentWork   `json:"agent,omitempty" bd:"subject"`
}

// PlanningResource covers board/backlog/iteration/release/component/saved-view
// records. Rank operations use RelativePosition instead of numeric indexes.
type PlanningResource struct {
	Ref      ResourceRef  `json:"ref" bd:"subject"`
	Name     string       `json:"name" bd:"subject"`
	State    string       `json:"state" bd:"public"`
	Revision string       `json:"revision" bd:"public"`
	StartsAt string       `json:"startsAt,omitempty" bd:"public"`
	EndsAt   string       `json:"endsAt,omitempty" bd:"public"`
	Fields   []FieldValue `json:"fields,omitempty" bd:"subject"`
}
type RelativePosition struct {
	Container ResourceRef  `json:"container" bd:"subject"`
	Before    *ResourceRef `json:"before,omitempty" bd:"subject"`
	After     *ResourceRef `json:"after,omitempty" bd:"subject"`
}
type WorkflowAction struct {
	ID           string        `json:"id" bd:"public"`
	Name         string        `json:"name" bd:"public"`
	TargetStatus string        `json:"targetStatus" bd:"public"`
	Fields       []FieldSchema `json:"fields,omitempty" bd:"subject"`
}
type Person struct {
	ID          string `json:"id" bd:"subject"`
	DisplayName string `json:"displayName" bd:"subject"`
	Assignable  bool   `json:"assignable" bd:"public"`
}
type Worklog struct {
	Ref       ResourceRef `json:"ref" bd:"subject"`
	AuthorID  string      `json:"authorId" bd:"subject"`
	StartedAt string      `json:"startedAt" bd:"public"`
	Seconds   int64       `json:"seconds,string" bd:"public"`
	Body      *RichBody   `json:"body,omitempty" bd:"subject"`
	Revision  string      `json:"revision" bd:"public"`
}
type WorklogInput struct {
	StartedAt string    `json:"startedAt" bd:"public"`
	Seconds   int64     `json:"seconds,string" bd:"public"`
	Body      *RichBody `json:"body,omitempty" bd:"subject"`
}
type Evidence struct {
	ID       string `json:"id" bd:"subject"`
	Kind     string `json:"kind" bd:"public"`
	URI      string `json:"uri" bd:"subject"`
	Checksum string `json:"checksum,omitempty" bd:"public"`
	Outcome  string `json:"outcome" bd:"public"`
}
type Criterion struct {
	ID       string     `json:"id" bd:"subject"`
	Text     string     `json:"text" bd:"subject"`
	State    string     `json:"state" bd:"public"`
	Evidence []Evidence `json:"evidence,omitempty" bd:"subject"`
}
type AgentWork struct {
	Goal         *ResourceRef  `json:"goal,omitempty" bd:"subject"`
	Requirements []ResourceRef `json:"requirements,omitempty" bd:"subject"`
	Acceptance   []Criterion   `json:"acceptance,omitempty" bd:"subject"`
	Readiness    string        `json:"readiness" bd:"public"`
	Reasons      []string      `json:"reasons,omitempty" bd:"subject"`
	PoolID       string        `json:"poolId,omitempty" bd:"subject"`
	DispatchID   string        `json:"dispatchId,omitempty" bd:"subject"`
	WorktreeID   string        `json:"worktreeId,omitempty" bd:"subject"`
	ReviewState  string        `json:"reviewState" bd:"public"`
	Claim        *Lease        `json:"claim,omitempty" bd:"subject"`
}

// AgentWorkInput contains writable agent metadata. Lease authority is absent:
// only the online claim operation can return a server-issued Lease. Providers
// still check current review, evidence, and workflow policy before applying it.
type AgentWorkInput struct {
	Goal         *ResourceRef  `json:"goal,omitempty" bd:"subject"`
	Requirements []ResourceRef `json:"requirements,omitempty" bd:"subject"`
	Acceptance   []Criterion   `json:"acceptance,omitempty" bd:"subject"`
	Readiness    string        `json:"readiness" bd:"public"`
	Reasons      []string      `json:"reasons,omitempty" bd:"subject"`
	PoolID       string        `json:"poolId,omitempty" bd:"subject"`
	DispatchID   string        `json:"dispatchId,omitempty" bd:"subject"`
	WorktreeID   string        `json:"worktreeId,omitempty" bd:"subject"`
	ReviewState  string        `json:"reviewState" bd:"public"`
}

// Lease is server-issued exclusive authority. An offline cache cannot renew it.
type Lease struct {
	ID        string      `json:"id" bd:"subject"`
	HolderID  string      `json:"holderId" bd:"subject"`
	Fence     string      `json:"fence" bd:"public"`
	ExpiresAt string      `json:"expiresAt" bd:"public"`
	Resource  ResourceRef `json:"resource" bd:"subject"`
}
type NativeMapping struct {
	SessionID          string      `json:"sessionId" bd:"subject"`
	NativeID           string      `json:"nativeId" bd:"subject"`
	Resource           ResourceRef `json:"resource" bd:"subject"`
	BindingRevision    string      `json:"bindingRevision" bd:"public"`
	ProviderGeneration string      `json:"providerGeneration" bd:"public"`
	LastOperationID    string      `json:"lastOperationId" bd:"subject"`
	Direction          string      `json:"direction" bd:"public"`
}

type SchemaRequest struct {
	Scope        Scope        `json:"scope" bd:"subject"`
	Resource     *ResourceRef `json:"resource,omitempty" bd:"subject"`
	Operation    string       `json:"operation" bd:"public"`
	ResourceKind string       `json:"resourceKind" bd:"public"`
	Page         PageRequest  `json:"page" bd:"subject"`
}
type SchemaResult struct {
	Fields  []FieldSchema    `json:"fields" bd:"subject"`
	Types   []string         `json:"types,omitempty" bd:"public"`
	Actions []WorkflowAction `json:"actions,omitempty" bd:"subject"`
	People  []Person         `json:"people,omitempty" bd:"subject"`
	Page    PageInfo         `json:"page" bd:"subject"`
	Problem *Problem         `json:"problem,omitempty" bd:"subject"`
}
type QueryRequest struct {
	Scope Scope  `json:"scope" bd:"subject"`
	Kind  string `json:"kind" bd:"public"`
	Query Query  `json:"query" bd:"subject"`
}
type QueryResult struct {
	Items    []WorkItem         `json:"items,omitempty" bd:"subject"`
	Projects []Project          `json:"projects,omitempty" bd:"subject"`
	Planning []PlanningResource `json:"planning,omitempty" bd:"subject"`
	Page     PageInfo           `json:"page" bd:"subject"`
	Problem  *Problem           `json:"problem,omitempty" bd:"subject"`
}
type ReadRequest struct {
	Scope    Scope       `json:"scope" bd:"subject"`
	Resource ResourceRef `json:"resource" bd:"subject"`
	Aspect   string      `json:"aspect" bd:"public"`
	Page     PageRequest `json:"page" bd:"subject"`
}
type ReadResult struct {
	Item        *WorkItem         `json:"item,omitempty" bd:"subject"`
	Project     *Project          `json:"project,omitempty" bd:"subject"`
	Planning    *PlanningResource `json:"planning,omitempty" bd:"subject"`
	Relations   []Relation        `json:"relations,omitempty" bd:"subject"`
	Comments    []Comment         `json:"comments,omitempty" bd:"subject"`
	Attachments []Attachment      `json:"attachments,omitempty" bd:"subject"`
	History     []Change          `json:"history,omitempty" bd:"subject"`
	Watchers    []Person          `json:"watchers,omitempty" bd:"subject"`
	Worklogs    []Worklog         `json:"worklogs,omitempty" bd:"subject"`
	Actions     []WorkflowAction  `json:"actions,omitempty" bd:"subject"`
	Grants      []Grant           `json:"grants,omitempty" bd:"subject"`
	Page        PageInfo          `json:"page" bd:"subject"`
	Problem     *Problem          `json:"problem,omitempty" bd:"subject"`
}

// WriteRequest is a typed action union. Only fields required by Action may be
// sent; omitted patch fields are preserved, Clear names fields to remove.
// Workflow transitions and governed completion have separate actions.
type WriteRequest struct {
	Scope            Scope             `json:"scope" bd:"subject"`
	Mutation         Mutation          `json:"mutation" bd:"subject"`
	Action           string            `json:"action" bd:"public"`
	Resource         *ResourceRef      `json:"resource,omitempty" bd:"subject"`
	Items            []ResourceRef     `json:"items,omitempty" bd:"subject"`
	Title            string            `json:"title,omitempty" bd:"subject"`
	Body             *RichBody         `json:"body,omitempty" bd:"subject"`
	Fields           []FieldValue      `json:"fields,omitempty" bd:"subject"`
	Clear            []string          `json:"clear,omitempty" bd:"public"`
	AssigneeID       string            `json:"assigneeId,omitempty" bd:"subject"`
	PrincipalID      string            `json:"principalId,omitempty" bd:"subject"`
	ActionID         string            `json:"actionId,omitempty" bd:"public"`
	Relation         *Relation         `json:"relation,omitempty" bd:"subject"`
	Position         *RelativePosition `json:"position,omitempty" bd:"subject"`
	Comment          *CommentInput     `json:"comment,omitempty" bd:"subject"`
	Worklog          *WorklogInput     `json:"worklog,omitempty" bd:"subject"`
	Planning         *PlanningResource `json:"planning,omitempty" bd:"subject"`
	Agent            *AgentWorkInput   `json:"agent,omitempty" bd:"subject"`
	Native           *NativeMapping    `json:"native,omitempty" bd:"subject"`
	Grants           []Grant           `json:"grants,omitempty" bd:"subject"`
	LeaseFence       string            `json:"leaseFence,omitempty" bd:"public"`
	ResourceKind     string            `json:"resourceKind,omitempty" bd:"public"`
	BulkAction       string            `json:"bulkAction,omitempty" bd:"public"`
	FieldDefinitions []FieldSchema     `json:"fieldDefinitions,omitempty" bd:"subject"`
	WorkflowActions  []WorkflowAction  `json:"workflowActions,omitempty" bd:"subject"`
}
type WriteResult struct {
	Receipt Receipt `json:"receipt" bd:"subject"`
}
type ClaimRequest struct {
	Scope           Scope       `json:"scope" bd:"subject"`
	Mutation        Mutation    `json:"mutation" bd:"subject"`
	Resource        ResourceRef `json:"resource" bd:"subject"`
	Action          string      `json:"action" bd:"public"`
	LeaseID         string      `json:"leaseId,omitempty" bd:"subject"`
	LeaseFence      string      `json:"leaseFence,omitempty" bd:"public"`
	DurationSeconds int         `json:"durationSeconds" bd:"public"`
}
type ClaimResult struct {
	Lease   *Lease  `json:"lease,omitempty" bd:"subject"`
	Receipt Receipt `json:"receipt" bd:"subject"`
}

func (p Project) Validate() error {
	return contractcheck.All(required("name", p.Name), required("revision", p.Revision))
}
func (w WorkItem) Validate() error {
	return contractcheck.All(required("type", w.Type), required("title", w.Title), required("status", w.Status), required("revision", w.Revision))
}
func (p RelativePosition) Validate() error {
	if (p.Before == nil) == (p.After == nil) {
		return fmt.Errorf("rank requires exactly one before or after reference")
	}
	return nil
}
func (w Worklog) Validate() error {
	if w.Seconds <= 0 {
		return fmt.Errorf("worklog seconds must be positive")
	}
	return contractcheck.All(contractcheck.ID("authorId", w.AuthorID), contractcheck.Timestamp("startedAt", w.StartedAt), required("revision", w.Revision))
}
func (w WorklogInput) Validate() error {
	if w.Seconds <= 0 {
		return fmt.Errorf("worklog seconds must be positive")
	}
	return contractcheck.Timestamp("startedAt", w.StartedAt)
}
func (l Lease) Validate() error {
	return contractcheck.All(contractcheck.ID("leaseId", l.ID), contractcheck.ID("holderId", l.HolderID), required("lease fence", l.Fence), contractcheck.Timestamp("expiresAt", l.ExpiresAt))
}
func (n NativeMapping) Validate() error {
	return contractcheck.All(contractcheck.ID("sessionId", n.SessionID), required("nativeId", n.NativeID), required("bindingRevision", n.BindingRevision), required("providerGeneration", n.ProviderGeneration), contractcheck.ID("lastOperationId", n.LastOperationID), contractcheck.Enum("native direction", n.Direction, "native-to-provider", "provider-to-native"))
}
func (a AgentWork) Validate() error {
	return contractcheck.All(contractcheck.Enum("readiness", a.Readiness, "ready", "blocked", "unknown"), contractcheck.Enum("reviewState", a.ReviewState, "not-requested", "pending", "approved", "rejected"))
}
func (a AgentWorkInput) Validate() error {
	return contractcheck.All(contractcheck.Enum("readiness", a.Readiness, "ready", "blocked", "unknown"), contractcheck.Enum("reviewState", a.ReviewState, "not-requested", "pending", "approved", "rejected"))
}
func (r SchemaRequest) Validate() error {
	return contractcheck.All(required("operation", r.Operation), required("resourceKind", r.ResourceKind), pm.Validate(r))
}
func (r SchemaResult) Validate() error {
	if r.Problem != nil {
		return pm.Failure(r, r.Problem)
	}
	return pm.Validate(r)
}
func (r QueryRequest) Validate() error {
	return contractcheck.All(contractcheck.Enum("query kind", r.Kind, "projects", "items", "boards", "backlogs", "iterations", "releases", "components", "saved-views", "goals", "requirements"), pm.Validate(r))
}
func (r QueryResult) Validate() error {
	if r.Problem != nil {
		return pm.Failure(r, r.Problem)
	}
	return pm.Validate(r)
}
func (r ReadRequest) Validate() error {
	return contractcheck.All(contractcheck.Enum("read aspect", r.Aspect, "record", "relations", "comments", "attachments", "history", "watchers", "worklogs", "workflow", "permissions", "readiness"), pm.Validate(r))
}
func (r ReadResult) Validate() error {
	if r.Problem != nil {
		return pm.Failure(r, r.Problem)
	}
	return pm.Validate(r)
}
func (r WriteRequest) Validate() error {
	if err := r.validateActionFields(); err != nil {
		return err
	}
	if err := contractcheck.All(r.Mutation.Matches(r.Scope), contractcheck.Enum("task action", r.Action, "project.ensure", "project.create", "project.update", "item.create", "item.patch", "item.replace", "item.assign", "item.clone", "item.archive", "item.restore", "item.delete", "item.bulk", "workflow.transition", "relation.add", "relation.remove", "comment.create", "comment.update", "comment.delete", "watcher.add", "watcher.remove", "worklog.create", "worklog.update", "worklog.delete", "planning.create", "planning.update", "planning.delete", "planning.transition", "planning.membership", "planning.rank", "agent.update", "agent.dispatch", "agent.review", "agent.complete", "native.sync", "admin.fields", "admin.workflow", "admin.screens", "admin.permissions"), pm.Validate(r)); err != nil {
		return err
	}
	if r.Action != "project.create" && r.Action != "project.ensure" && r.Resource == nil {
		return fmt.Errorf("action requires resource")
	}
	if r.Action == "workflow.transition" || r.Action == "planning.transition" {
		if r.ActionID == "" {
			return fmt.Errorf("transition requires discovered action id")
		}
	}
	if r.Action == "planning.rank" && r.Position == nil {
		return fmt.Errorf("rank requires relative position")
	}
	if (r.Action == "relation.add" || r.Action == "relation.remove") && r.Relation == nil {
		return fmt.Errorf("relation action requires relation")
	}
	if r.Action == "agent.complete" && (r.LeaseFence == "" || r.Agent == nil || r.Agent.ReviewState != "approved") {
		return fmt.Errorf("completion requires lease fence and approved review")
	}
	if r.Action == "native.sync" {
		if r.Native == nil || !r.Native.Resource.SameResource(*r.Resource) || r.Native.BindingRevision != r.Scope.BindingRevision || r.Native.ProviderGeneration != r.Scope.ProviderGeneration || r.Native.LastOperationID != r.Mutation.OperationID {
			return fmt.Errorf("native mapping does not match binding and operation")
		}
	}
	if r.Action == "item.create" && r.ResourceKind == "" {
		return fmt.Errorf("item creation requires resource kind")
	}
	if r.Action == "item.bulk" {
		if err := contractcheck.All(contractcheck.Count("bulk items", len(r.Items), 1, 200), contractcheck.Enum("bulk action", r.BulkAction, "patch", "assign", "archive", "restore", "delete", "transition")); err != nil {
			return err
		}
		if r.BulkAction == "transition" && r.ActionID == "" {
			return fmt.Errorf("bulk transition requires action id")
		}
	}
	if (r.Action == "comment.create" || r.Action == "comment.update") && r.Comment == nil {
		return fmt.Errorf("comment action requires body")
	}
	if (r.Action == "worklog.create" || r.Action == "worklog.update") && r.Worklog == nil {
		return fmt.Errorf("worklog action requires worklog")
	}
	for _, f := range r.Fields {
		if f.Name == "status" {
			return fmt.Errorf("status changes require workflow.transition")
		}
	}
	for _, field := range r.Clear {
		if field == "status" {
			return fmt.Errorf("status changes require workflow.transition")
		}
	}
	return nil
}
func (r WriteResult) Validate() error { return pm.Validate(r) }
func (r ClaimRequest) Validate() error {
	if r.DurationSeconds < 0 || r.DurationSeconds > 86400 {
		return fmt.Errorf("invalid claim duration")
	}
	if r.Action != "acquire" && (r.LeaseID == "" || r.LeaseFence == "") {
		return fmt.Errorf("renew/release requires server-issued lease id and fence")
	}
	if r.Action != "release" && (r.DurationSeconds < 1 || r.DurationSeconds > 86400) {
		return fmt.Errorf("claim duration must be 1 to 86400 seconds")
	}
	return contractcheck.All(contractcheck.Enum("claim action", r.Action, "acquire", "renew", "release"), r.Mutation.Matches(r.Scope), pm.Validate(r))
}
func (r ClaimResult) Validate() error {
	if r.Lease != nil && !r.Receipt.RemoteAccepted {
		return fmt.Errorf("lease requires remote acceptance")
	}
	return pm.Validate(r)
}
func required(name, value string) error { return contractcheck.Text(name, value, 4096, true) }

func (r WriteResult) ValidateFor(q WriteRequest) error {
	return contractcheck.All(r.Receipt.ValidateFor(q.Mutation), r.Validate())
}

func (r ClaimResult) ValidateFor(q ClaimRequest) error {
	if r.Receipt.RemoteAccepted && q.Action != "release" && r.Lease == nil {
		return fmt.Errorf("accepted claim requires lease")
	}
	if r.Lease != nil && !r.Lease.Resource.SameResource(q.Resource) {
		return fmt.Errorf("lease resource mismatch")
	}
	return contractcheck.All(r.Receipt.ValidateFor(q.Mutation), r.Validate())
}
