package plugin

import (
	"encoding/json"
	"testing"
)

func TestNavigationShapeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		item  NavItem
		valid bool
	}{
		{"legacy", NavItem{ID: "files", Label: "Files", Href: "/files"}, true},
		{"group", NavItem{ID: "remote", Label: "Remote access", Kind: NavKindGroup}, true},
		{"section", NavItem{ID: "work", Label: "Work", Kind: NavKindSection}, true},
		{"link-parent", NavItem{ID: "logs", Label: "Logs", Href: "/logs", Parent: &NavReference{Owner: "system", ID: "system"}}, true},
		{"empty-link", NavItem{ID: "files", Label: "Files"}, false},
		{"unknown-kind", NavItem{ID: "files", Label: "Files", Kind: "tree"}, false},
		{"folder-url", NavItem{ID: "remote", Label: "Remote", Kind: NavKindGroup, Href: "/remote"}, false},
		{"section-parent", NavItem{ID: "work", Label: "Work", Kind: NavKindSection, Parent: &NavReference{Owner: "core", ID: "other"}}, false},
		{"unqualified-parent", NavItem{ID: "logs", Label: "Logs", Href: "/logs", Parent: &NavReference{ID: "system"}}, false},
		{"invalid-placement", NavItem{ID: "files", Label: "Files", Href: "/files", Placement: "overlay"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateNavigation([]NavItem{tc.item}); (err == nil) != tc.valid {
				t.Fatalf("ValidateNavigation = %v, valid=%v", err, tc.valid)
			}
		})
	}
}

func TestNavigationSnapshotKeepsDeclaredAndResolvedReferencesSeparate(t *testing.T) {
	declared := &NavReference{Owner: "offline", ID: "parent"}
	resolved := &NavReference{Owner: "core", ID: "root"}
	in := NavigationSnapshot{Items: []NavigationNode{{Owner: "child", Item: NavItem{ID: "page", Label: "Page", Href: "/page", Parent: declared}, Parent: resolved}}, Diagnostics: []NavigationDiagnostic{}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out NavigationSnapshot
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if *out.Items[0].Item.Parent != *declared || *out.Items[0].Parent != *resolved {
		t.Fatalf("lost declared or effective parent: %s", raw)
	}
	if (NavItem{}).EffectiveKind() != NavKindLink {
		t.Fatal("legacy items must remain links")
	}
}
