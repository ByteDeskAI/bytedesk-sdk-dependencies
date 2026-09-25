package plugin

import "fmt"

const (
	NavKindLink                 = "link"
	NavKindGroup                = "group"
	NavKindSection              = "section"
	NavPlacementMain            = "main"
	NavPlacementPinned          = "pinned"
	NavPlacementFooter          = "footer"
	NavigationChildrenInterface = "navigation.children.v1"
)

// NavReference names a declaration, never grants permission to see or attach to it.
// IDs remain local to their manifest owner; labels and URLs are not identities.
type NavReference struct {
	Owner string `json:"owner" bd:"public"`
	ID    string `json:"id" bd:"public"`
}

// NavigationNode is a host-resolved, authorized projection of a declaration.
// Parent and Section are the effective references after unavailable ancestors
// have been removed. Item retains the declaration for owner attribution.
// A flat representation supports arbitrary depth without recursive wire values.
type NavigationNode struct {
	Owner   string        `json:"owner" bd:"public"`
	Item    NavItem       `json:"item" bd:"public"`
	Parent  *NavReference `json:"parent,omitempty" bd:"public"`
	Section *NavReference `json:"section,omitempty" bd:"public"`
}

type NavigationDiagnostic struct {
	Owner string `json:"owner" bd:"public"`
	ID    string `json:"id" bd:"public"`
	Code  string `json:"code" bd:"public"`
}

// NavigationSnapshot is published with the authorized contribution projection
// and its runtime epoch/revision. It is not a replacement for endpoint access checks.
type NavigationSnapshot struct {
	Items       []NavigationNode       `json:"items" bd:"public"`
	Diagnostics []NavigationDiagnostic `json:"diagnostics" bd:"public"`
}

// EffectiveKind preserves the meaning of existing flat navigation declarations.
func (n NavItem) EffectiveKind() string {
	if n.Kind == "" {
		return NavKindLink
	}
	return n.Kind
}

// ValidateNavigation checks declaration shape only. The host owns admission,
// cross-owner extension resolution, availability, cycles and effective placement.
func ValidateNavigation(items []NavItem) error {
	for _, n := range items {
		if n.ID == "" || n.Label == "" {
			return fmt.Errorf("navigation: id and label required")
		}
		switch n.EffectiveKind() {
		case NavKindLink:
			if n.Href == "" {
				return fmt.Errorf("navigation %q: link requires href", n.ID)
			}
		case NavKindGroup, NavKindSection:
			if n.Href != "" || n.NewTab {
				return fmt.Errorf("navigation %q: structural node cannot navigate", n.ID)
			}
		default:
			return fmt.Errorf("navigation %q: unknown kind %q", n.ID, n.Kind)
		}
		if n.Placement != "" && n.Placement != NavPlacementMain && n.Placement != NavPlacementPinned && n.Placement != NavPlacementFooter {
			return fmt.Errorf("navigation %q: unknown placement %q", n.ID, n.Placement)
		}
		for _, ref := range []*NavReference{n.Parent, n.Section} {
			if ref != nil && (ref.Owner == "" || ref.ID == "") {
				return fmt.Errorf("navigation %q: references require owner and id", n.ID)
			}
		}
		if n.EffectiveKind() == NavKindSection && (n.Parent != nil || n.Section != nil || n.Placement != "" && n.Placement != NavPlacementMain) {
			return fmt.Errorf("navigation %q: section must be a main root", n.ID)
		}
	}
	return nil
}
