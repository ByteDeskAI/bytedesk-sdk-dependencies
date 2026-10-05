// Package projectknowledge defines the independently selected knowledge slot
// of Project Management. Hosts resolve providers; consumers share this facade.
package projectknowledge

import (
	"fmt"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
	pm "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/projectmanagement"
)

const ContractRevision = 1

type Space struct {
	Ref         ResourceRef  `json:"ref" bd:"subject"`
	Name        string       `json:"name" bd:"subject"`
	Description *RichBody    `json:"description,omitempty" bd:"subject"`
	Revision    string       `json:"revision" bd:"public"`
	Fields      []FieldValue `json:"fields,omitempty" bd:"subject"`
}

// Content preserves source bodies and draft/published identity. A hierarchy is
// represented by references rather than recursive, unbounded response trees.
type Content struct {
	Ref        ResourceRef   `json:"ref" bd:"subject"`
	Space      ResourceRef   `json:"space" bd:"subject"`
	Kind       string        `json:"kind" bd:"public"`
	Title      string        `json:"title" bd:"subject"`
	Body       *RichBody     `json:"body,omitempty" bd:"subject"`
	State      string        `json:"state" bd:"public"`
	Revision   string        `json:"revision" bd:"public"`
	VersionID  string        `json:"versionId" bd:"public"`
	Parent     *ResourceRef  `json:"parent,omitempty" bd:"subject"`
	Ancestors  []ResourceRef `json:"ancestors,omitempty" bd:"subject"`
	Depth      int           `json:"depth" bd:"public"`
	OrderKey   string        `json:"orderKey,omitempty" bd:"public"`
	Labels     []string      `json:"labels,omitempty" bd:"subject"`
	Properties []FieldValue  `json:"properties,omitempty" bd:"subject"`
	Template   *ResourceRef  `json:"template,omitempty" bd:"subject"`
	AuthorID   string        `json:"authorId" bd:"subject"`
	CreatedAt  string        `json:"createdAt" bd:"public"`
	UpdatedAt  string        `json:"updatedAt" bd:"public"`
}
type Version struct {
	ID        string      `json:"id" bd:"public"`
	Resource  ResourceRef `json:"resource" bd:"subject"`
	Revision  string      `json:"revision" bd:"public"`
	AuthorID  string      `json:"authorId" bd:"subject"`
	CreatedAt string      `json:"createdAt" bd:"public"`
	Message   string      `json:"message,omitempty" bd:"subject"`
	Body      *RichBody   `json:"body,omitempty" bd:"subject"`
}
type VersionDiff struct {
	FromID string `json:"fromId" bd:"public"`
	ToID   string `json:"toId" bd:"public"`
	Format string `json:"format" bd:"public"`
	Source string `json:"source" bd:"subject"`
	Lossy  bool   `json:"lossy" bd:"public"`
}
type HierarchyMove struct {
	Parent             ResourceRef  `json:"parent" bd:"subject"`
	Before             *ResourceRef `json:"before,omitempty" bd:"subject"`
	After              *ResourceRef `json:"after,omitempty" bd:"subject"`
	IncludeDescendants bool         `json:"includeDescendants" bd:"public"`
}
type Reaction struct {
	ID      string `json:"id" bd:"subject"`
	ActorID string `json:"actorId" bd:"subject"`
	Value   string `json:"value" bd:"public"`
}
type ReactionInput struct {
	Value string `json:"value" bd:"public"`
}

// InlineTask is a document checkbox, not a managed work item or completion grant.
type InlineTask struct {
	ID          string       `json:"id" bd:"subject"`
	Text        string       `json:"text" bd:"subject"`
	State       string       `json:"state" bd:"public"`
	AssigneeID  string       `json:"assigneeId,omitempty" bd:"subject"`
	ManagedTask *ResourceRef `json:"managedTask,omitempty" bd:"subject"`
}
type Access struct {
	EffectiveOperations []string     `json:"effectiveOperations" bd:"public"`
	Grants              []Grant      `json:"grants,omitempty" bd:"subject"`
	InheritedFrom       *ResourceRef `json:"inheritedFrom,omitempty" bd:"subject"`
	PublicLinkEnabled   bool         `json:"publicLinkEnabled" bd:"public"`
	Revision            string       `json:"revision" bd:"public"`
}
type Export struct {
	Mode               string    `json:"mode" bd:"public"`
	Format             string    `json:"format" bd:"public"`
	IncludeVersions    bool      `json:"includeVersions" bd:"public"`
	IncludeAttachments bool      `json:"includeAttachments" bd:"public"`
	IncludePermissions bool      `json:"includePermissions" bd:"public"`
	Transfer           *Transfer `json:"transfer,omitempty" bd:"subject"`
	Lossless           bool      `json:"lossless" bd:"public"`
}

type SchemaRequest struct {
	Scope       Scope        `json:"scope" bd:"subject"`
	Resource    *ResourceRef `json:"resource,omitempty" bd:"subject"`
	Operation   string       `json:"operation" bd:"public"`
	ContentKind string       `json:"contentKind" bd:"public"`
}
type SchemaResult struct {
	Fields  []FieldSchema `json:"fields" bd:"subject"`
	Kinds   []string      `json:"kinds" bd:"public"`
	Formats []string      `json:"formats" bd:"public"`
	States  []string      `json:"states" bd:"public"`
	Problem *Problem      `json:"problem,omitempty" bd:"subject"`
}
type QueryRequest struct {
	Scope     Scope        `json:"scope" bd:"subject"`
	Kind      string       `json:"kind" bd:"public"`
	Container *ResourceRef `json:"container,omitempty" bd:"subject"`
	Query     Query        `json:"query" bd:"subject"`
}
type QueryResult struct {
	Spaces   []Space   `json:"spaces,omitempty" bd:"subject"`
	Contents []Content `json:"contents,omitempty" bd:"subject"`
	Page     PageInfo  `json:"page" bd:"subject"`
	Problem  *Problem  `json:"problem,omitempty" bd:"subject"`
}
type ReadRequest struct {
	Scope            Scope       `json:"scope" bd:"subject"`
	Resource         ResourceRef `json:"resource" bd:"subject"`
	Aspect           string      `json:"aspect" bd:"public"`
	VersionID        string      `json:"versionId,omitempty" bd:"public"`
	CompareVersionID string      `json:"compareVersionId,omitempty" bd:"public"`
	Page             PageRequest `json:"page" bd:"subject"`
}
type ReadResult struct {
	Space       *Space       `json:"space,omitempty" bd:"subject"`
	Content     *Content     `json:"content,omitempty" bd:"subject"`
	Versions    []Version    `json:"versions,omitempty" bd:"subject"`
	Diff        *VersionDiff `json:"diff,omitempty" bd:"subject"`
	Relations   []Relation   `json:"relations,omitempty" bd:"subject"`
	Comments    []Comment    `json:"comments,omitempty" bd:"subject"`
	Reactions   []Reaction   `json:"reactions,omitempty" bd:"subject"`
	WatcherIDs  []string     `json:"watcherIds,omitempty" bd:"subject"`
	InlineTasks []InlineTask `json:"inlineTasks,omitempty" bd:"subject"`
	Attachments []Attachment `json:"attachments,omitempty" bd:"subject"`
	Access      *Access      `json:"access,omitempty" bd:"subject"`
	History     []Change     `json:"history,omitempty" bd:"subject"`
	Page        PageInfo     `json:"page" bd:"subject"`
	Problem     *Problem     `json:"problem,omitempty" bd:"subject"`
}
type WriteRequest struct {
	Scope       Scope          `json:"scope" bd:"subject"`
	Mutation    Mutation       `json:"mutation" bd:"subject"`
	Action      string         `json:"action" bd:"public"`
	Resource    *ResourceRef   `json:"resource,omitempty" bd:"subject"`
	Title       string         `json:"title,omitempty" bd:"subject"`
	Body        *RichBody      `json:"body,omitempty" bd:"subject"`
	ContentKind string         `json:"contentKind,omitempty" bd:"public"`
	Fields      []FieldValue   `json:"fields,omitempty" bd:"subject"`
	Clear       []string       `json:"clear,omitempty" bd:"public"`
	VersionID   string         `json:"versionId,omitempty" bd:"public"`
	Move        *HierarchyMove `json:"move,omitempty" bd:"subject"`
	Comment     *CommentInput  `json:"comment,omitempty" bd:"subject"`
	Reaction    *ReactionInput `json:"reaction,omitempty" bd:"subject"`
	InlineTask  *InlineTask    `json:"inlineTask,omitempty" bd:"subject"`
	Labels      []string       `json:"labels,omitempty" bd:"subject"`
	Template    *ResourceRef   `json:"template,omitempty" bd:"subject"`
	Grants      []Grant        `json:"grants,omitempty" bd:"subject"`
	State       string         `json:"state,omitempty" bd:"public"`
	PrincipalID string         `json:"principalId,omitempty" bd:"subject"`
	Export      *Export        `json:"export,omitempty" bd:"subject"`
}
type WriteResult struct {
	Receipt Receipt `json:"receipt" bd:"subject"`
	Export  *Export `json:"export,omitempty" bd:"subject"`
}

func (s Space) Validate() error {
	return contractcheck.All(required("name", s.Name), required("revision", s.Revision))
}
func (c Content) Validate() error {
	if c.Depth < 0 {
		return fmt.Errorf("negative hierarchy depth")
	}
	return contractcheck.All(required("kind", c.Kind), required("title", c.Title), contractcheck.Enum("content state", c.State, "draft", "published", "archived", "deleted"), required("revision", c.Revision), required("versionId", c.VersionID), contractcheck.ID("authorId", c.AuthorID), contractcheck.Timestamp("createdAt", c.CreatedAt), contractcheck.Timestamp("updatedAt", c.UpdatedAt))
}
func (v Version) Validate() error {
	return contractcheck.All(required("versionId", v.ID), required("revision", v.Revision), contractcheck.ID("authorId", v.AuthorID), contractcheck.Timestamp("createdAt", v.CreatedAt))
}
func (m HierarchyMove) Validate() error {
	if m.Before != nil && m.After != nil {
		return fmt.Errorf("move cannot specify both before and after")
	}
	return nil
}
func (t InlineTask) Validate() error {
	return contractcheck.All(required("inline task id", t.ID), contractcheck.Enum("inline task state", t.State, "open", "complete"))
}
func (e Export) Validate() error {
	if e.Mode == "lossless-archive" && (!e.IncludeVersions || !e.IncludeAttachments || !e.IncludePermissions) {
		return fmt.Errorf("lossless archive must include versions, attachments, and permissions")
	}
	return contractcheck.All(contractcheck.Enum("export mode", e.Mode, "rendered", "lossless-archive"), required("export format", e.Format))
}
func (r SchemaRequest) Validate() error {
	return contractcheck.All(required("operation", r.Operation), required("contentKind", r.ContentKind), pm.Validate(r))
}
func (r SchemaResult) Validate() error {
	if r.Problem != nil {
		return pm.Failure(r, r.Problem)
	}
	return pm.Validate(r)
}
func (r QueryRequest) Validate() error {
	return contractcheck.All(contractcheck.Enum("query kind", r.Kind, "spaces", "content", "children", "ancestors", "descendants", "templates", "labels"), pm.Validate(r))
}
func (r QueryResult) Validate() error {
	if r.Problem != nil {
		return pm.Failure(r, r.Problem)
	}
	return pm.Validate(r)
}
func (r ReadRequest) Validate() error {
	if r.Aspect == "diff" && (r.VersionID == "" || r.CompareVersionID == "") {
		return fmt.Errorf("diff requires two version ids")
	}
	return contractcheck.All(contractcheck.Enum("read aspect", r.Aspect, "record", "versions", "version", "diff", "relations", "comments", "reactions", "watchers", "inline-tasks", "attachments", "access", "history"), pm.Validate(r))
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
	if err := contractcheck.All(r.Mutation.Matches(r.Scope), contractcheck.Enum("knowledge action", r.Action, "space.ensure", "space.create", "space.update", "content.create", "content.update", "content.archive", "content.restore", "content.delete", "draft.publish", "content.move", "content.copy", "version.restore", "comment.create", "comment.update", "comment.delete", "comment.resolve", "comment.reopen", "reaction.add", "reaction.remove", "watcher.add", "watcher.remove", "inline-task.update", "labels.set", "properties.set", "template.apply", "template.create", "template.update", "template.delete", "state.set", "permissions.set", "public-link.enable", "public-link.disable", "export"), pm.Validate(r)); err != nil {
		return err
	}
	if r.Action != "space.create" && r.Action != "space.ensure" && r.Resource == nil {
		return fmt.Errorf("action requires resource")
	}
	if r.Action == "content.move" || r.Action == "content.copy" {
		if r.Move == nil {
			return fmt.Errorf("move/copy requires destination")
		}
		if r.Move.Parent.SameResource(*r.Resource) || r.Move.Before != nil && r.Move.Before.SameResource(*r.Resource) || r.Move.After != nil && r.Move.After.SameResource(*r.Resource) {
			return fmt.Errorf("content cannot be its own parent or sibling position")
		}
	}
	if r.Action == "version.restore" && (r.VersionID == "" || r.Mutation.ExpectedRevision == "") {
		return fmt.Errorf("version restore creates a new version and requires source version and expected current revision")
	}
	if r.Action == "draft.publish" && r.Mutation.ExpectedRevision == "" {
		return fmt.Errorf("publish requires expected revision")
	}
	if r.Action == "export" && r.Export == nil {
		return fmt.Errorf("export requires explicit mode and format")
	}
	if r.Action == "inline-task.update" && r.InlineTask == nil {
		return fmt.Errorf("inline-task update requires task")
	}
	if (r.Action == "comment.create" || r.Action == "comment.update") && r.Comment == nil {
		return fmt.Errorf("comment action requires body")
	}
	if r.Action == "content.create" && (r.ContentKind == "" || r.Title == "") {
		return fmt.Errorf("content creation requires kind and title")
	}
	return nil
}
func (r WriteResult) Validate() error   { return pm.Validate(r) }
func required(name, value string) error { return contractcheck.Text(name, value, 4096, true) }

func (r WriteResult) ValidateFor(q WriteRequest) error {
	return contractcheck.All(r.Receipt.ValidateFor(q.Mutation), r.Validate())
}
