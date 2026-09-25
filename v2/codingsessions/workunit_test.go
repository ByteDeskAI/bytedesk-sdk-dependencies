package codingsessions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

func TestWorkUnitReferenceIsExplicitAndBounded(t *testing.T) {
	for _, id := range []string{"TM-1", "TM-001", "TM-475", "TM-" + strings.Repeat("9", 61)} {
		if err := (WorkUnitReference{TaskID: id}).Validate(); err != nil {
			t.Fatalf("valid task %q: %v", id, err)
		}
	}
	for _, id := range []string{"", "TM-", "TM-0", "TM-000", "tm-1", "TM--1", "TM-1.0", "TM-１", "TM-1\n", " TM-1", "../TM-1", "https://tasks/TM-1", "TM-" + strings.Repeat("9", 62)} {
		if (WorkUnitReference{TaskID: id}).Validate() == nil {
			t.Fatalf("accepted task reference %q", id)
		}
	}
	if (BoundWorkUnit{TaskID: "TM-475"}).Validate() == nil {
		t.Fatal("binding without opaque ID")
	}
	if err := (BoundWorkUnit{TaskID: "TM-475", BindingID: "workunit_immutable"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkUnitOptionalInputAndOutput(t *testing.T) {
	create := CreateRequest{ProjectID: "p", CheckoutRef: "checkout", Preferences: Preferences{RoutingPolicy: PolicyBalanced, PermissionMode: PermissionAsk}, IdempotencyKey: "once"}
	next := NewTaskRequest{SessionID: "s", Preferences: create.Preferences, IdempotencyKey: "next"}
	for _, value := range []interface{ Validate() error }{create, next} {
		if err := value.Validate(); err != nil {
			t.Fatalf("unlinked compatibility: %v", err)
		}
	}
	create.WorkUnit = &WorkUnitReference{TaskID: "TM-475"}
	next.WorkUnit = create.WorkUnit
	for _, value := range []interface{ Validate() error }{create, next} {
		if err := value.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	session := Session{ID: "s", TaskID: "coding-task", ProjectID: "p", State: StatePending, Preferences: create.Preferences, ConfigOptions: []ConfigOption{}, Recovery: "none", CreatedAt: "2026-09-25T00:00:00Z", UpdatedAt: "2026-09-25T00:00:00Z", CheckoutRef: "checkout", WorkUnit: &BoundWorkUnit{TaskID: "TM-475", BindingID: "workunit_immutable"}}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
	session.WorkUnit.BindingID = ""
	if session.Validate() == nil {
		t.Fatal("session accepted unresolved link")
	}
	create.WorkUnit.TaskID = ""
	if create.Validate() == nil || next.Validate() == nil {
		t.Fatal("empty explicit reference silently treated as unlinked")
	}
}

func TestWorkUnitStrictWireRejectsCallerBindingAndStoreAuthority(t *testing.T) {
	base := `{"projectId":"p","checkoutRef":"checkout","preferences":{"routingPolicy":"balanced","permissionMode":"ask"},"idempotencyKey":"once","workUnit":%s}`
	for _, ref := range []string{
		`{"taskId":"TM-475","bindingId":"stolen"}`, `{"taskId":"TM-475","storeRef":"other"}`,
		`{"taskId":"TM-475","root":"/private"}`, `{"taskId":"TM-475","url":"http://localhost:9999"}`,
		`{"taskId":"TM-475","taskId":"TM-476"}`, `{"TaskID":"TM-475"}`, `{"taskId":null}`, `{"taskId":"TM-000"}`,
		`null`,
	} {
		raw := strings.Replace(base, "%s", ref, 1)
		if _, err := plugin.DecodeValidated[CreateRequest]([]byte(raw)); err == nil {
			t.Fatalf("accepted request authority %s", ref)
		}
	}
	for _, ref := range []string{`{"taskId":"TM-475"}`, `{"taskId":"TM-001"}`} {
		if _, err := plugin.DecodeValidated[CreateRequest]([]byte(strings.Replace(base, "%s", ref, 1))); err != nil {
			t.Fatal(err)
		}
	}
	var next NewTaskRequest
	if err := json.Unmarshal([]byte(`{"sessionId":"s","preferences":{"routingPolicy":"balanced","permissionMode":"ask"},"idempotencyKey":"next"}`), &next); err != nil || next.WorkUnit != nil {
		t.Fatal("omitted next-task link not absent")
	}
}

func TestWorkUnitJSONOmissionObjectAndNullMatchBrowserContract(t *testing.T) {
	create := CreateRequest{ProjectID: "p", CheckoutRef: "checkout", Preferences: Preferences{RoutingPolicy: PolicyBalanced, PermissionMode: PermissionAsk}, IdempotencyKey: "once"}
	next := NewTaskRequest{SessionID: "s", Preferences: create.Preferences, IdempotencyKey: "next"}
	session := Session{ID: "s", TaskID: "coding-task", ProjectID: "p", State: StatePending, Preferences: create.Preferences, ConfigOptions: []ConfigOption{}, Recovery: "none", CreatedAt: "2026-09-25T00:00:00Z", UpdatedAt: "2026-09-25T00:00:00Z", CheckoutRef: "checkout"}
	for _, test := range []struct {
		name   string
		value  any
		decode func([]byte) error
		object string
	}{
		{"create", create, func(raw []byte) error { _, err := plugin.DecodeValidated[CreateRequest](raw); return err }, `{"taskId":"TM-001"}`},
		{"new-task", next, func(raw []byte) error { _, err := plugin.DecodeValidated[NewTaskRequest](raw); return err }, `{"taskId":"TM-001"}`},
		{"session", session, func(raw []byte) error { _, err := plugin.DecodeValidated[Session](raw); return err }, `{"taskId":"TM-001","bindingId":"binding_1"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.decode(raw); err != nil {
				t.Fatalf("omitted: %v", err)
			}
			prefix := strings.TrimSuffix(string(raw), "}")
			if err := test.decode([]byte(prefix + `,"workUnit":` + test.object + `}`)); err != nil {
				t.Fatalf("object: %v", err)
			}
			for _, tail := range []string{`,"workUnit":null}`, `,"WorkUnit":` + test.object + `}`, `,"unknown":true}`, `,"workUnit":` + test.object + `,"workUnit":` + test.object + `}`} {
				if err := test.decode([]byte(prefix + tail)); err == nil {
					t.Fatalf("custom decoder bypassed strict guard: %s", tail)
				}
			}
		})
	}
}
