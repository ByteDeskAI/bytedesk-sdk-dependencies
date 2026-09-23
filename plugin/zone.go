package plugin

import (
	"fmt"
	"sort"
	"strings"
)

// BaseID is the publisher's required base for this point. RequiredBase wins
// when it is set. Otherwise Interface is the base. Empty means the point names
// no base.
func (p ExtensionPoint) BaseID() string {
	if base := strings.TrimSpace(p.RequiredBase); base != "" {
		return base
	}
	return strings.TrimSpace(p.Interface)
}

// AdmitImplementer reports whether provider may register at point.
//
// A sealed point refuses every implementer. A UI zone (Zone set) or a point
// that names RequiredBase admits only a provider whose Base equals BaseID.
// Zero implementers is success for the caller: this function is one provider.
// A historical point that is not a zone and does not name RequiredBase keeps
// the previous rule and admits a provider that does not name a base.
func AdmitImplementer(point ExtensionPoint, provider Provider) error {
	name := strings.TrimSpace(point.Name)
	if name == "" {
		name = strings.TrimSpace(provider.Point)
	}
	if point.Sealed {
		return fmt.Errorf("extension point %s is sealed", name)
	}
	if strings.TrimSpace(point.Zone) == "" && strings.TrimSpace(point.RequiredBase) == "" {
		return nil
	}
	want := point.BaseID()
	got := strings.TrimSpace(provider.Base)
	if want == "" || got != want {
		return fmt.Errorf("extension point %s requires base %q, got %q", name, want, got)
	}
	return nil
}

// AssignPublisherColors gives each publisher one color. Publishers are ordered
// by id. A hint is kept when it is a palette entry nobody earlier in that
// order already took. A collision or an unknown hint takes the next free
// palette color. The returned map is keyed by publisher id.
func AssignPublisherColors(publishers []Publisher, palette []string) map[string]string {
	ids := make([]string, 0, len(publishers))
	hint := make(map[string]string, len(publishers))
	for _, p := range publishers {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		if _, seen := hint[id]; seen {
			continue
		}
		ids = append(ids, id)
		hint[id] = strings.TrimSpace(p.Color)
	}
	sort.Strings(ids)
	used := make(map[string]bool, len(ids))
	out := make(map[string]string, len(ids))
	next := 0
	takeFree := func() string {
		for next < len(palette) {
			color := palette[next]
			next++
			if color == "" || used[color] {
				continue
			}
			used[color] = true
			return color
		}
		return ""
	}
	for _, id := range ids {
		color := hint[id]
		if color != "" && !used[color] && paletteContains(palette, color) {
			used[color] = true
			out[id] = color
			continue
		}
		out[id] = takeFree()
	}
	return out
}

func paletteContains(palette []string, color string) bool {
	for _, entry := range palette {
		if entry == color {
			return true
		}
	}
	return false
}
