package plugin

import "strings"

// RequiredIDs returns requires[].id (trimmed, non-empty).
func (m Manifest) RequiredIDs() []string {
	out := make([]string, 0, len(m.Requires))
	for _, req := range m.Requires {
		id := strings.TrimSpace(req.ID)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// MissingRequired returns required ids not present in installed.
func MissingRequired(m Manifest, installed map[string]struct{}) []string {
	var missing []string
	for _, id := range m.RequiredIDs() {
		if _, ok := installed[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// GraphFrom maps each manifest id to the peer ids it requires.
func GraphFrom(manifests []Manifest) map[string][]string {
	g := make(map[string][]string, len(manifests))
	for _, m := range manifests {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		g[id] = append([]string(nil), m.RequiredIDs()...)
	}
	return g
}

// Cycle returns a cycle (ids) if the requires graph has one, else nil.
func Cycle(graph map[string][]string) []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(graph))
	var stack []string
	var found []string
	var visit func(string) bool
	visit = func(n string) bool {
		color[n] = gray
		stack = append(stack, n)
		for _, nxt := range graph[n] {
			switch color[nxt] {
			case gray:
				// cycle: nxt ... n -> nxt
				for i, id := range stack {
					if id == nxt {
						found = append([]string(nil), stack[i:]...)
						found = append(found, nxt)
						return true
					}
				}
				found = []string{nxt, n, nxt}
				return true
			case white:
				if visit(nxt) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	for n := range graph {
		if color[n] == white {
			if visit(n) {
				return found
			}
		}
	}
	return nil
}
