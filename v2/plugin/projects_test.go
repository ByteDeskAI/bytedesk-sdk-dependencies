package plugin

import (
	"encoding/json"
	"testing"
)

func TestProjectContributionsValidateOwnerLocalPanels(t *testing.T) {
	m := fullManifest()
	if err := Validate(m); err != nil {
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
			m := fullManifest()
			mutate(&m)
			if err := Validate(m); err == nil {
				t.Fatal("invalid contribution accepted")
			}
		})
	}
}

func TestDirectoryContextWizardRoundTrip(t *testing.T) {
	want := DirectoryContextActionWizardContext{
		ActionID: "create-web-app",
		Context:  ProjectDirectoryContext{ProjectID: "p", CheckoutID: "c", WorktreeID: "w", ProjectRoot: "/p", WorktreeRoot: "/p/w", DirectoryPath: "/p/w/app"},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got DirectoryContextActionWizardContext
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("context changed: got %+v", got)
	}
}
