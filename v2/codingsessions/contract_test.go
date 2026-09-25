package codingsessions

import "testing"

func TestCandidateSelectionRequiresAdvertisedEnforceableOptions(t *testing.T) {
	p := Provider{ID: "coder", Generation: "g1", Name: "Coder", Availability: "available", ObservedAt: "2026-09-25T00:00:00Z", PermissionModes: []string{PermissionAsk}, Models: []Model{{ID: "m1", Name: "Model"}}, ConfigOptions: []ConfigOption{{ID: "vendor-thought", Name: "Effort", Category: "thought_level", Type: "select", Choices: []ConfigChoice{{Value: "vendor-auto", Name: "Automatic"}}}}}
	o := Overrides{ProviderID: "coder", ModelID: "m1", ConfigValues: []ConfigValue{{ID: "vendor-thought", Value: "vendor-auto"}}}
	if err := o.ValidateFor(p, PermissionAsk); err != nil {
		t.Fatal(err)
	}
	if o.ValidateFor(p, PermissionFullAccess) == nil {
		t.Fatal("unenforceable permission accepted")
	}
	o.ConfigValues[0].Value = "medium"
	if o.ValidateFor(p, PermissionAsk) == nil {
		t.Fatal("invented effort accepted")
	}
}
func TestOrderedEventsAndDiscriminatedPayloads(t *testing.T) {
	e := SessionEvent{SessionID: "s", TaskID: "t", Sequence: 1, At: "2026-09-25T00:00:00Z", Kind: "message", Message: &Message{Role: "assistant", Content: TextInput{Inline: "working"}}}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Failure = &Failure{Code: "oops", Message: "failed"}
	if e.Validate() == nil {
		t.Fatal("ambiguous event accepted")
	}
	e.Failure = nil
	v := EventsResult{Events: []SessionEvent{e, e}, LastSequence: 1}
	if v.Validate() == nil {
		t.Fatal("duplicate sequence accepted")
	}
	v.Events = v.Events[:1]
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestActiveSessionRequiresStickyRoute(t *testing.T) {
	s := Session{ID: "s", TaskID: "t", ProjectID: "p", State: StateActive, Preferences: Preferences{RoutingPolicy: PolicyBalanced, PermissionMode: PermissionAsk}, Recovery: "none", CreatedAt: "2026-09-25T00:00:00Z", UpdatedAt: "2026-09-25T00:00:00Z", CheckoutRef: "committed-head"}
	if s.Validate() == nil {
		t.Fatal("active session without route accepted")
	}
	s.Route = &Route{ProviderID: "coder", ProviderGeneration: "g1", ModelID: "m1", Policy: PolicyBalanced, Source: "fallback", Reason: "decision provider unavailable"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.State = StateEnded
	s.ActivePromptID = "p"
	if s.Validate() == nil {
		t.Fatal("ended session active prompt accepted")
	}
}
