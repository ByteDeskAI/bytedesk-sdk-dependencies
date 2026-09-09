package plugin

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestDocumentPathGrammar(t *testing.T) {
	for _, path := range []string{"/sessions", "/sessions/:tabId", "/files/*path", "/settings/:section/*rest", "/a.b/~user"} {
		if err := ValidateDocumentPath(path); err != nil {
			t.Errorf("valid %q: %v", path, err)
		}
	}
	for _, path := range []string{"", "/", "sessions", " /sessions", "/sessions/", "//sessions", "/a//b", "/a/.", "/a/..", "/a%20b", "/a?b", "/a#b", "/a\\b", "/a/:", "/a/:9x", "/a/:x/:x", "/a/:x/*x", "/a/*", "/a/*rest/more", "/a/pre:x", "/a/*rest*", "/é"} {
		if err := ValidateDocumentPath(path); err == nil {
			t.Errorf("accepted malformed %q", path)
		}
	}
}

func TestDocumentOverlapMatchesEnumeratedLanguage(t *testing.T) {
	patterns := []string{"/a", "/b", "/:x", "/*tail", "/a/:x", "/a/*tail", "/a/b", "/:x/b", "/:x/*tail", "/a/:x/b", "/a/:x/*tail", "/a/b/:x", "/a/b/*tail", "/a/b/x/:id", "/a/b/x/*tail"}
	// Exhaustively enumerate a representative finite alphabet containing all
	// literals and an unrelated value. Depth exceeds every fixed prefix.
	witness := make([][]bool, len(patterns))
	for i := range witness {
		witness[i] = make([]bool, len(patterns))
	}
	var walk func([]string)
	walk = func(parts []string) {
		if len(parts) != 0 {
			path := "/" + strings.Join(parts, "/")
			var matched []int
			for i, pattern := range patterns {
				if _, ok := MatchDocumentPath(pattern, path); ok {
					matched = append(matched, i)
				}
			}
			for _, i := range matched {
				for _, j := range matched {
					witness[i][j] = true
				}
			}
		}
		if len(parts) == 5 {
			return
		}
		for _, value := range []string{"a", "b", "x", "z"} {
			walk(append(parts, value))
		}
	}
	walk(nil)
	for i, a := range patterns {
		for j, b := range patterns {
			got, err := DocumentPathsOverlap(a, b)
			if err != nil || got != witness[i][j] {
				t.Errorf("%s vs %s: overlap %v, %v; enumerated witness %v", a, b, got, err, witness[i][j])
			}
		}
	}
}

func TestDocumentPathSharedVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/document_paths.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Matches []struct {
			Pattern string            `json:"pattern"`
			Path    string            `json:"path"`
			Params  map[string]string `json:"params"`
		} `json:"matches"`
		Overlaps []struct {
			A       string `json:"a"`
			B       string `json:"b"`
			Overlap bool   `json:"overlap"`
		} `json:"overlaps"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, tc := range vectors.Matches {
		got, ok := MatchDocumentPath(tc.Pattern, tc.Path)
		if ok != (tc.Params != nil) || !reflect.DeepEqual(got, tc.Params) {
			t.Errorf("shared vector %q %q = %#v, %v; want %#v", tc.Pattern, tc.Path, got, ok, tc.Params)
		}
	}
	for _, tc := range vectors.Overlaps {
		got, err := DocumentPathsOverlap(tc.A, tc.B)
		if err != nil || got != tc.Overlap {
			t.Errorf("shared overlap %q %q = %v, %v; want %v", tc.A, tc.B, got, err, tc.Overlap)
		}
	}
}

func TestDocumentPathMatching(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		params        map[string]string
	}{
		{"/sessions", "/sessions", map[string]string{}},
		{"/sessions/:tabId", "/sessions/abc", map[string]string{"tabId": "abc"}},
		{"/sessions/:tabId", "/sessions/a%20b", map[string]string{"tabId": "a b"}},
		{"/sessions/:tabId", "/sessions/%E2%9C%93", map[string]string{"tabId": "✓"}},
		{"/sessions/:tabId", "/sessions/%252F", map[string]string{"tabId": "%2F"}},
		{"/files/*path", "/files/a/b.txt", map[string]string{"path": "a/b.txt"}},
		{"/files/*path", "/files/a", map[string]string{"path": "a"}},
		{"/settings/:section/*rest", "/settings/theme/dark/editor", map[string]string{"section": "theme", "rest": "dark/editor"}},
	} {
		got, ok := MatchDocumentPath(tc.pattern, tc.path)
		if !ok || !reflect.DeepEqual(got, tc.params) {
			t.Errorf("%q at %q = %#v, %v; want %#v", tc.pattern, tc.path, got, ok, tc.params)
		}
	}
	for _, path := range []string{"/sessions", "/sessions/", "/sessions/a/b", "/Sessions/a", "/sessions/%2f", "/sessions/%5C", "/sessions/%00", "/sessions/%7f", "/sessions/%FF", "/sessions/%", "/sessions/.", "/sessions/%2e%2e", "/sessions/a?b", "/sessions/a#b", "https://host/sessions/a"} {
		if _, ok := MatchDocumentPath("/sessions/:tabId", path); ok {
			t.Errorf("matched unsafe or nonmatching path %q", path)
		}
	}
	for _, path := range []string{"/files", "/files/", "/files/a//b", "/files/a/../b", "/files/a/%2f/b"} {
		if _, ok := MatchDocumentPath("/files/*path", path); ok {
			t.Errorf("catch-all matched %q", path)
		}
	}
	if _, ok := MatchDocumentPath("/settings/:section/*rest", "/settings/theme"); ok {
		t.Fatal("catch-all matched zero segments")
	}
}

func TestDocumentPathOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"/sessions", "/sessions", true},
		{"/sessions", "/sessions/:id", false},
		{"/sessions/new", "/sessions/:id", true},
		{"/sessions/:id", "/sessions/:tab", true},
		{"/files", "/files/*path", false},
		{"/files/:id", "/files/*path", true},
		{"/files/a/b", "/files/*path", true},
		{"/files/*path", "/files/:dir/*rest", true},
		{"/a/:id/x", "/a/b/y", false},
		{"/a/:id/*rest", "/a/b", false},
		{"/a/:id/*rest", "/a/b/c", true},
		{"/a/*rest", "/b/*rest", false},
		{"/a/*x", "/a/*y", true},
		{"/a/b/*x", "/a/b", false},
	} {
		for _, pair := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
			got, err := DocumentPathsOverlap(pair[0], pair[1])
			if err != nil || got != tc.want {
				t.Errorf("overlap %q, %q = %v, %v; want %v", pair[0], pair[1], got, err, tc.want)
			}
		}
	}
	if _, err := DocumentPathsOverlap("/a", "/bad/"); err == nil {
		t.Fatal("invalid pattern treated as a nonconflicting claim")
	}
}

func TestManifestDocumentClaims(t *testing.T) {
	base := func() Manifest {
		return Manifest{ID: "files", Version: "1", Protocol: &ProtocolRequirements{Major: ProtocolMajor, Required: []string{FeatureDocumentPaths}}, Panels: []PanelSpec{
			{ID: "files", URL: "/ui", DocumentPaths: []string{"/files", "/files/*path"}},
		}}
	}
	if err := base().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Manifest){
		"missing negotiation": func(m *Manifest) { m.Protocol = nil },
		"missing feature":     func(m *Manifest) { m.Protocol.Required = nil },
		"root":                func(m *Manifest) { m.Panels[0].DocumentPaths = []string{"/"} },
		"trailing slash":      func(m *Manifest) { m.Panels[0].DocumentPaths = []string{"/files/"} },
		"overlap same panel":  func(m *Manifest) { m.Panels[0].DocumentPaths = []string{"/files/:id", "/files/*path"} },
		"duplicate":           func(m *Manifest) { m.Panels[0].DocumentPaths = []string{"/files", "/files"} },
		"overlap different panel": func(m *Manifest) {
			m.Panels = append(m.Panels, PanelSpec{ID: "other", URL: "/other", DocumentPaths: []string{"/files/new"}})
		},
		"ambiguous panel id": func(m *Manifest) { m.Panels = append(m.Panels, PanelSpec{ID: "files", URL: "/other"}) },
		"missing panel id":   func(m *Manifest) { m.Panels[0].ID = "" },
		"missing document":   func(m *Manifest) { m.Panels[0].URL = "" },
	} {
		t.Run(name, func(t *testing.T) {
			m := base()
			mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("authoring accepted invalid claims")
			}
			if err := m.ValidateDiscover(); err == nil {
				t.Fatal("discovery accepted invalid claims")
			}
		})
	}
}
