package projectknowledge

import (
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"testing"
)

func fixture() WriteRequest {
	return WriteRequest{Scope: Scope{ProjectID: "project", EnvironmentID: "dev", TenantID: "tenant", BindingID: "binding", BindingRevision: "rev", ProviderGeneration: "generation", CapabilityRevision: "caps"}, Mutation: Mutation{OperationID: "operation", SourceBindingRevision: "rev", Origin: "ui", CreatedAt: "2026-10-05T12:00:00Z"}, Action: "content.update", Resource: &ResourceRef{ProviderID: "knowledge", InstallationID: "install", Kind: "page", ID: "native-id"}, Body: &RichBody{Format: "storage", Version: "1", Source: "<p>native source</p>"}}
}

func TestRestoreCreatesVersionAgainstExpectedCurrentRevision(t *testing.T) {
	r := fixture()
	r.Action = "version.restore"
	r.Body = nil
	r.VersionID = "old-version"
	if r.Validate() == nil {
		t.Fatal("restored without expected current revision")
	}
	r.Mutation.ExpectedRevision = "current-version"
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Action = "draft.publish"
	r.Mutation.ExpectedRevision = ""
	if r.Validate() == nil {
		t.Fatal("published stale draft")
	}
}

func TestKnowledgeActionUnionAndFacadeAdmission(t *testing.T) {
	r := fixture()
	r.PrincipalID = "another-author"
	if r.Validate() == nil {
		t.Fatal("content update carried another acting principal")
	}
	if len(DeclaredCommands([]bus.Pattern{"cmd.gateway.>"})) != 0 {
		t.Fatal("broad wildcard gained knowledge authority")
	}
	if got := DeclaredCommands([]bus.Pattern{"cmd.gateway.project-knowledge.v1.>"}); len(got) != 9 {
		t.Fatalf("missing knowledge commands: %v", got)
	}
}

func TestRenderedExportDoesNotImplyLosslessArchive(t *testing.T) {
	r := fixture()
	r.Action = "export"
	r.Body = nil
	r.Export = &Export{Mode: "lossless-archive", Format: "native"}
	if r.Validate() == nil {
		t.Fatal("archive omitted versions/bytes/permissions")
	}
	r.Export.IncludeVersions = true
	r.Export.IncludeAttachments = true
	r.Export.IncludePermissions = true
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Export = &Export{Mode: "rendered", Format: "pdf"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Export.Lossless {
		t.Fatal("rendered export acquired lossless guarantee")
	}
}

func TestContentMoveAndInlineTasksHaveDistinctAuthority(t *testing.T) {
	r := fixture()
	r.Action = "content.move"
	r.Body = nil
	if r.Validate() == nil {
		t.Fatal("move without target parent")
	}
	parent, sibling := *r.Resource, *r.Resource
	parent.ID = "parent"
	sibling.ID = "sibling"
	r.Move = &HierarchyMove{Parent: parent, Before: &sibling, After: &sibling}
	if r.Validate() == nil {
		t.Fatal("ambiguous relative order")
	}
	r.Move.After = nil
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r = fixture()
	r.Action = "inline-task.update"
	r.Body = nil
	r.InlineTask = &InlineTask{ID: "checkbox", Text: "review", State: "complete"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.InlineTask.ManagedTask != nil {
		t.Fatal("document checkbox became managed task")
	}
}
