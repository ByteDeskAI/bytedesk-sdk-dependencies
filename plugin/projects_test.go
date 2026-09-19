package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func validProjectsManifest() Manifest {
	return Manifest{
		ID:      "web-apps",
		Version: "1.0.0",
		Panels: []PanelSpec{
			{ID: "web-apps", Kind: "page", URL: "/web-apps"},
			{ID: "create-web-app", Kind: "page", URL: "/create-web-app"},
		},
		ProjectViews: []ProjectViewContribution{{
			ID: "web-apps", Label: "Web Apps", Icon: "globe", Order: 30, PanelID: "web-apps",
		}},
		DirectoryContextActions: []DirectoryContextActionContribution{{
			ID: "create-web-app", Label: "Create Web App", WizardPanelID: "create-web-app",
		}},
	}
}

func TestProjectContributionsValidateOwnerLocalPanels(t *testing.T) {
	if err := validProjectsManifest().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Manifest){
		"missing view label":   func(m *Manifest) { m.ProjectViews[0].Label = "" },
		"missing view icon":    func(m *Manifest) { m.ProjectViews[0].Icon = "" },
		"foreign view panel":   func(m *Manifest) { m.ProjectViews[0].PanelID = "other" },
		"duplicate view id":    func(m *Manifest) { m.ProjectViews = append(m.ProjectViews, m.ProjectViews[0]) },
		"missing action label": func(m *Manifest) { m.DirectoryContextActions[0].Label = "" },
		"foreign wizard panel": func(m *Manifest) { m.DirectoryContextActions[0].WizardPanelID = "other" },
		"duplicate action id": func(m *Manifest) {
			m.DirectoryContextActions = append(m.DirectoryContextActions, m.DirectoryContextActions[0])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := validProjectsManifest()
			mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("invalid contribution accepted")
			}
		})
	}
}

func TestDirectoryContextContractsRoundTripWithoutDroppingHostContext(t *testing.T) {
	want := DirectoryContextActionWizardContext{
		ActionID: "create-web-app",
		Context: ProjectDirectoryContext{
			ProjectID: "project-1", CheckoutID: "checkout-1", WorktreeID: "worktree-1",
			ProjectRoot: "/projects/client", WorktreeRoot: "/projects/client/.worktrees/demo",
			DirectoryPath: "/projects/client/.worktrees/demo/apps/portal",
		},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got DirectoryContextActionWizardContext
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got != want || !strings.Contains(string(raw), "directoryPath") {
		t.Fatalf("context changed: got %+v raw=%s", got, raw)
	}
}
