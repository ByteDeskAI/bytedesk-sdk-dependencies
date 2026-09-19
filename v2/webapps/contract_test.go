package webapps

import (
	"reflect"
	"strings"
	"testing"
)

func validTarget() Target {
	return Target{ProjectID: "project-1", CheckoutID: "checkout-1", AppID: "app-1", ConfigRevision: "revision-1", RunID: "run-1"}
}

func validMessage() Message {
	return Message{ID: "message-1", Role: "assistant", Kind: "markdown", Markdown: "Ready", CreatedAt: "2026-09-19T12:00:00Z", Attachments: []Attachment{}}
}

func TestCommandIdentityMatchesWebAppsPanel(t *testing.T) {
	want := []string{
		"cmd.web-apps.v1.list", "cmd.web-apps.v1.create", "cmd.web-apps.v1.creation-eligibility",
		"cmd.web-apps.v1.conversation.send", "cmd.web-apps.v1.conversation.approve", "cmd.web-apps.v1.conversation.answer",
		"cmd.web-apps.v1.run.stop", "cmd.web-apps.v1.services.start", "cmd.web-apps.v1.services.stop", "cmd.web-apps.v1.services.logs",
		"cmd.web-apps.v1.preview.resolve", "cmd.web-apps.v1.preview.navigate", "cmd.web-apps.v1.preview.open-external",
	}
	got := []string{CommandList, CommandCreate, CommandCreationEligibility, CommandConversationSend, CommandConversationApprove, CommandConversationAnswer, CommandRunStop, CommandServicesStart, CommandServicesStop, CommandServicesLogs, CommandPreviewResolve, CommandPreviewNavigate, CommandPreviewOpenExternal}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v", got)
	}
	descriptors := []string{List.Name(), Create.Name(), CheckCreationEligibility.Name(), SendConversationMessage.Name(), ApproveConversationRequest.Name(), AnswerConversationRequest.Name(), StopRun.Name(), StartServices.Name(), StopServices.Name(), ReadServiceLogs.Name(), ResolvePreview.Name(), NavigatePreview.Name(), OpenPreviewExternal.Name()}
	if !reflect.DeepEqual(descriptors, want) {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	if Changed.Name() != EventChanged || Events.Name() != "WEB_APPS_EVENTS" {
		t.Fatalf("event descriptors = %q %q", Changed.Name(), Events.Name())
	}
}

func TestTargetPinsRunAndConfiguration(t *testing.T) {
	if err := validTarget().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Target){
		func(v *Target) { v.ProjectID = "" }, func(v *Target) { v.CheckoutID = "" },
		func(v *Target) { v.AppID = "" }, func(v *Target) { v.ConfigRevision = "" },
		func(v *Target) { v.RunID = "bad path" },
	} {
		v := validTarget()
		mutate(&v)
		if err := v.Validate(); err == nil {
			t.Fatalf("invalid target accepted: %+v", v)
		}
	}
}

func TestConversationAndEventsAreBoundedAndStructured(t *testing.T) {
	req := ConversationSendRequest{Target: validTarget(), Content: "Build it", Mode: "build", ProviderID: "codex", Attachments: []Attachment{}}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	req.Content = strings.Repeat("x", MaxTextBytes+1)
	if err := req.Validate(); err == nil {
		t.Fatal("oversized message accepted")
	}
	event := RuntimeEvent{Cursor: "cursor-1", Kind: "message", Target: validTarget(), CreatedAt: "2026-09-19T12:00:00Z", Message: ptr(validMessage())}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	event.Run = &Run{ID: "run-1", State: "running", Mode: "build", ProviderID: "codex", ConfigRevision: "revision-1", Activity: []ToolActivity{}}
	if err := event.Validate(); err == nil {
		t.Fatal("event with two payloads accepted")
	}
}

func TestRuntimeStateDoesNotExposeHostAuthority(t *testing.T) {
	for _, value := range []any{Target{}, Run{}, ServiceStatus{}, Preview{}, App{}} {
		typ := reflect.TypeOf(value)
		for _, forbidden := range []string{"Root", "CWD", "Command", "Argv", "Process", "Handle", "Password", "Secret", "Environment"} {
			if _, found := typ.FieldByName(forbidden); found {
				t.Fatalf("%s exposes forbidden field %s", typ.Name(), forbidden)
			}
		}
	}
}

func ptr[T any](value T) *T { return &value }
