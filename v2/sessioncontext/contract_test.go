package sessioncontext

import (
	"reflect"
	"strings"
	"testing"
)

func validContext() Context {
	return Context{
		ID: "context-1", Purpose: PurposeTaskDashboard, Revision: "revision-1",
		ExpiresAt: "2026-09-19T00:00:00Z", State: StateReady,
		Actions: []AllowedAction{{ID: "start", Label: "Start dashboard", Enabled: true}},
	}
}

func TestContractIdentity(t *testing.T) {
	if ContractRevision != 1 {
		t.Fatalf("revision = %d", ContractRevision)
	}
	want := []string{"cmd.host.session-context.v1.open", "cmd.host.session-context.v1.refresh", "cmd.host.session-context.v1.action"}
	if got := []string{CommandOpen, CommandRefresh, CommandAction}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v", got)
	}
}

func TestContextAcceptsDerivedStateOnly(t *testing.T) {
	if err := (OpenRequest{Purpose: PurposeTaskDashboard, TargetID: "terminal-1"}).Validate(); err != nil {
		t.Fatalf("valid open: %v", err)
	}
	if err := validContext().Validate(); err != nil {
		t.Fatalf("valid context: %v", err)
	}
	for _, invalid := range []Context{
		{ID: "context-1", Purpose: PurposeTaskDashboard, Revision: "r", ExpiresAt: "bad", State: StateReady, Actions: []AllowedAction{}},
		{ID: "context-1", Purpose: PurposeTaskDashboard, Revision: "r", ExpiresAt: "2026-09-19T00:00:00Z", State: "unknown", Actions: []AllowedAction{}},
		{ID: "context-1", Purpose: PurposeTaskDashboard, Revision: "r", ExpiresAt: "2026-09-19T00:00:00Z", State: StateReady, Actions: nil},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid context accepted: %#v", invalid)
		}
	}
	for _, name := range []string{"CWD", "Root", "Port", "Proxy", "URL", "Server", "Path", "Lease", "Principal"} {
		if _, found := reflect.TypeOf(Context{}).FieldByName(name); found {
			t.Fatalf("Context exposes forbidden field %s", name)
		}
	}
}

func TestActionRequestIsBoundedAndClosed(t *testing.T) {
	valid := ActionRequest{ContextID: "context-1", Revision: "revision-1", ActionID: "start", IdempotencyKey: "request-1", Inputs: []ActionInput{}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid action: %v", err)
	}
	invalid := valid
	invalid.Inputs = []ActionInput{{Name: "field", Value: "one"}, {Name: "field", Value: "two"}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("duplicate input accepted")
	}
	invalid = valid
	invalid.Inputs = []ActionInput{{Name: "field", Value: strings.Repeat("x", 1025)}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("oversized input accepted")
	}
}
