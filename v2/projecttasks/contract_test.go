package projecttasks

import (
	"encoding/json"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
	"strings"
	"testing"
)

func TestProviderEnvelopeSharesTheWireLimit(t *testing.T) {
	r := fixture()
	r.Fields = nil
	r.Body = &RichBody{Format: "markdown", Version: "1", Source: strings.Repeat("x", 32000), Rendered: strings.Repeat("x", 32000)}
	base, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Title = strings.Repeat("x", 65400-len(base))
	if err := r.Validate(); err != nil {
		t.Fatal("request fixture must fit:", err)
	}
	p := ProviderWriteRequest{Request: r, Invocation: Invocation{ID: "invocation", Scope: r.Scope, Actor: ActingContext{ActorID: "actor", ConnectionID: "connection", ExecutionMode: "interactive"}, ExpiresAt: "2026-10-05T12:01:00Z"}}
	if p.Validate() == nil {
		t.Fatal("provider envelope exceeded the wire limit")
	}
}

func fixture() WriteRequest {
	return WriteRequest{Scope: Scope{ProjectID: "project", EnvironmentID: "dev", TenantID: "tenant", BindingID: "binding", BindingRevision: "rev", ProviderGeneration: "generation", CapabilityRevision: "caps"}, Mutation: Mutation{OperationID: "operation", SourceBindingRevision: "rev", Origin: "terminal", CreatedAt: "2026-10-05T12:00:00Z"}, Action: "item.patch", Resource: &ResourceRef{ProviderID: "tasks", InstallationID: "install", Kind: "item", ID: "native-id"}, Fields: []FieldValue{{Name: "priority", Type: "string", JSON: `"high"`}}}
}

func TestWorkflowStatusCannotBePatched(t *testing.T) {
	r := fixture()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Fields[0].Name = "status"
	if r.Validate() == nil {
		t.Fatal("status bypassed workflow")
	}
	r = fixture()
	r.Action = "workflow.transition"
	if r.Validate() == nil {
		t.Fatal("transition without discovered action")
	}
	r.ActionID = "transition-1"
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestActionUnionAndExactFacadeAdmission(t *testing.T) {
	r := fixture()
	r.Grants = []Grant{{PrincipalID: "someone", PrincipalKind: "user", Operation: "delete", Effect: "allow"}}
	if r.Validate() == nil {
		t.Fatal("item patch carried administrative permission change")
	}
	if len(DeclaredCommands([]bus.Pattern{"cmd.gateway.>"})) != 0 {
		t.Fatal("broad wildcard gained project authority")
	}
	if got := DeclaredCommands([]bus.Pattern{bus.Pattern(CommandRead), bus.Pattern(CommandRead)}); len(got) != 1 {
		t.Fatal("exact command missing or duplicated")
	}
	if got := DeclaredCommands([]bus.Pattern{"cmd.gateway.project-tasks.v1.>"}); len(got) != 10 {
		t.Fatalf("missing task commands: %v", got)
	}
}

func TestNativeMappingPinsOperationBindingAndGeneration(t *testing.T) {
	r := fixture()
	r.Action = "native.sync"
	r.Native = &NativeMapping{SessionID: "session", NativeID: "todo-7", Resource: *r.Resource, BindingRevision: "rev", ProviderGeneration: "generation", LastOperationID: "operation", Direction: "native-to-provider"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*NativeMapping){"binding": func(n *NativeMapping) { n.BindingRevision = "old" }, "generation": func(n *NativeMapping) { n.ProviderGeneration = "old" }, "operation": func(n *NativeMapping) { n.LastOperationID = "old" }} {
		t.Run(name, func(t *testing.T) {
			q := r
			mapping := *r.Native
			q.Native = &mapping
			change(q.Native)
			if q.Validate() == nil {
				t.Fatal("native mapping escaped fence")
			}
		})
	}
}

func TestClaimsAndCompletionRequireServerAuthority(t *testing.T) {
	w := fixture()
	q := ClaimRequest{Scope: w.Scope, Mutation: w.Mutation, Resource: *w.Resource, Action: "renew", DurationSeconds: 300}
	if q.Validate() == nil {
		t.Fatal("lease renewal without server fence")
	}
	q.LeaseID = "lease"
	q.LeaseFence = "server-fence"
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	r := ClaimResult{Receipt: Receipt{OperationID: "operation", State: "accepted", RemoteAccepted: true, ObservedAt: "2026-10-05T12:00:00Z"}}
	if r.ValidateFor(q) == nil {
		t.Fatal("accepted claim without lease")
	}
	r.Lease = &Lease{ID: "lease", HolderID: "actor", Fence: "server-fence", ExpiresAt: "2026-10-05T12:05:00Z", Resource: q.Resource}
	if err := r.ValidateFor(q); err != nil {
		t.Fatal(err)
	}
	r.Receipt.State = "saved-local"
	r.Receipt.RemoteAccepted = false
	if r.Validate() == nil {
		t.Fatal("local persistence granted lease")
	}
	w.Action = "agent.complete"
	w.Fields = nil
	if w.Validate() == nil {
		t.Fatal("ungoverned completion")
	}
	w.LeaseFence = "server-fence"
	w.Agent = &AgentWorkInput{Readiness: "ready", ReviewState: "approved"}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentWritesCannotSupplyServerIssuedLease(t *testing.T) {
	r := fixture()
	r.Action = "agent.update"
	r.Fields = nil
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	agent := map[string]any{"readiness": "ready", "reviewState": "not-requested"}
	wire["agent"] = agent
	valid, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.DecodeValidated[WriteRequest](valid); err != nil {
		t.Fatal("ordinary agent metadata rejected:", err)
	}
	agent["claim"] = Lease{ID: "fabricated", HolderID: "actor", Fence: "fabricated", ExpiresAt: "2027-10-05T12:00:00Z", Resource: *r.Resource}
	for _, action := range []string{"agent.update", "agent.dispatch", "agent.review", "agent.complete"} {
		wire["action"] = action
		if action == "agent.complete" {
			agent["reviewState"] = "approved"
			wire["leaseFence"] = "fabricated"
		}
		raw, err = json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plugin.DecodeValidated[WriteRequest](raw); err == nil {
			t.Fatalf("%s accepted a caller-authored lease", action)
		}
	}
}

func TestCommentCreationCannotImpersonateHistoricalAuthor(t *testing.T) {
	r := fixture()
	r.Action = "comment.create"
	r.Fields = nil
	r.Comment = &CommentInput{Body: RichBody{Format: "markdown", Version: "1", Source: "hello"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	// CommentInput exposes body/parent/anchor only. Authenticated authorship is
	// intentionally absent from the writable schema and remains host-owned.
}
