package plugin

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type presentationVectors struct {
	AcceptRequests []PresentationRequest         `json:"acceptRequests"`
	RejectRequests []struct{ Name, JSON string } `json:"rejectRequests"`
	AcceptResults  []PresentationResult          `json:"acceptResults"`
	RejectResults  []struct{ Name, JSON string } `json:"rejectResults"`
}

func loadPresentationVectors(t *testing.T) presentationVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/terminal_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var v presentationVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTerminalPresentationSharedVectors(t *testing.T) {
	v := loadPresentationVectors(t)
	request := v.AcceptRequests[0]
	for _, want := range v.AcceptRequests {
		raw, _ := json.Marshal(want)
		if got, err := DecodePresentationRequest(bytes.NewReader(raw)); err != nil || len(got.Terminals) != len(want.Terminals) {
			t.Errorf("accepted request rejected: %v", err)
		}
	}
	for _, tc := range v.RejectRequests {
		t.Run("request "+tc.Name, func(t *testing.T) {
			if _, err := DecodePresentationRequest(strings.NewReader(tc.JSON)); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	for _, want := range v.AcceptResults {
		raw, _ := json.Marshal(want)
		if _, err := DecodePresentationResult(bytes.NewReader(raw), request, request.Terminals); err != nil {
			t.Errorf("accepted result rejected: %v", err)
		}
	}
	for _, tc := range v.RejectResults {
		t.Run("result "+tc.Name, func(t *testing.T) {
			if _, err := DecodePresentationResult(strings.NewReader(tc.JSON), request, request.Terminals); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestTerminalPresentationLeaseAndCurrentPrincipal(t *testing.T) {
	v := loadPresentationVectors(t)
	request, result := v.AcceptRequests[0], v.AcceptResults[0]
	bad := result
	bad.Lease.RequestID = "late"
	if err := ValidatePresentationResult(request, request.Terminals, bad); err == nil {
		t.Fatal("late lease accepted")
	}
	if err := ValidatePresentationResult(request, request.Terminals[:1], result); err == nil {
		t.Fatal("revoked terminal accepted")
	}
	changed := append([]PresentationTerminal(nil), request.Terminals...)
	tmux := *changed[1].Context.Tmux
	changed[1].Context.Tmux = &tmux
	changed[1].Context.Tmux.PanePID = "5"
	if err := ValidatePresentationResult(request, changed, result); err == nil {
		t.Fatal("changed incarnation accepted")
	}
	duplicate := result
	duplicate.Items = append(duplicate.Items, duplicate.Items[0])
	if err := ValidatePresentationResult(request, request.Terminals, duplicate); err == nil {
		t.Fatal("duplicate result accepted")
	}
}

func TestTerminalPresentationBounds(t *testing.T) {
	v := loadPresentationVectors(t)
	request, result := v.AcceptRequests[0], v.AcceptResults[0]
	result.Items[0].GroupPath = []PresentationGroup{{ID: "same", Label: "A"}}
	result.Items[1].GroupPath = []PresentationGroup{{ID: "same", Label: "B"}}
	if err := ValidatePresentationResult(request, request.Terminals, result); err == nil {
		t.Fatal("inconsistent prefix accepted")
	}
	result = v.AcceptResults[0]
	result.Items[0].Badges = make([]PresentationBadge, TerminalPresentationMaxBadges+1)
	if err := ValidatePresentationResult(request, request.Terminals, result); err == nil {
		t.Fatal("badge overflow accepted")
	}
	oversize := strings.NewReader(`{"lease":{},"terminals":[],"padding":"` + strings.Repeat("x", TerminalPresentationMaxBytes) + `"}`)
	if _, err := DecodePresentationRequest(oversize); err == nil {
		t.Fatal("oversized request accepted")
	}
	request = v.AcceptRequests[0]
	request.Terminals = make([]PresentationTerminal, TerminalPresentationMaxTerminals+1)
	for i := range request.Terminals {
		request.Terminals[i] = PresentationTerminal{TerminalID: string(rune(0x1000 + i)), Context: TerminalBindingContext{Kind: TerminalBindingNone}}
	}
	if err := ValidatePresentationRequest(request); err == nil {
		t.Fatal("terminal count overflow accepted")
	}
	result = v.AcceptResults[0]
	result.Items[0].GroupPath = make([]PresentationGroup, TerminalPresentationMaxGroups+1)
	if err := ValidatePresentationResult(v.AcceptRequests[0], v.AcceptRequests[0].Terminals, result); err == nil {
		t.Fatal("group depth overflow accepted")
	}
	for _, maxAge := range []uint32{0, 30001} {
		result = v.AcceptResults[0]
		result.MaxAgeMS = maxAge
		if err := ValidatePresentationResult(v.AcceptRequests[0], v.AcceptRequests[0].Terminals, result); err == nil {
			t.Fatalf("maxAgeMs %d accepted", maxAge)
		}
	}
}

func TestTerminalPresentationOptionalAgentIdentity(t *testing.T) {
	v := loadPresentationVectors(t)
	request, result := v.AcceptRequests[0], v.AcceptResults[0]
	result.Items[0].AgentID = "codex-qi"
	result.Items[0].DisplayName = "Codex Qi"
	if err := ValidatePresentationResult(request, request.Terminals, result); err != nil {
		t.Fatalf("valid agent identity rejected: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*PresentationItem)
	}{
		{"agent id controls", func(item *PresentationItem) { item.AgentID = "bad\nidentity" }},
		{"display name controls", func(item *PresentationItem) { item.DisplayName = "bad\ndisplay name" }},
		{"agent id too long", func(item *PresentationItem) { item.AgentID = strings.Repeat("a", 129) }},
		{"display name too long", func(item *PresentationItem) { item.DisplayName = strings.Repeat("d", 161) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := v.AcceptResults[0]
			tc.mutate(&invalid.Items[0])
			if err := ValidatePresentationResult(request, request.Terminals, invalid); err == nil {
				t.Fatal("invalid agent identity accepted")
			}
		})
	}
}

func TestTerminalPresentationIdentifiers(t *testing.T) {
	if TerminalPresentationPoint != "host.terminal.presentation" || TerminalPresentationInterface != "terminal.presentation.v1" || TerminalPresentationCommand != "terminal.presentation.project.v1" || TerminalPresentationBindingRead != "terminal.presentation.binding.read.v1" {
		t.Fatal("canonical identifiers drifted")
	}
}
