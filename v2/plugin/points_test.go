package plugin

import "testing"

// TestKnownPointsIsClosedAndCopied checks the registry is what it claims: a
// fixed vocabulary a caller cannot extend.
func TestKnownPointsIsClosedAndCopied(t *testing.T) {
	points := KnownPoints()
	if len(points) == 0 {
		t.Fatal("KnownPoints is empty")
	}
	seen := map[Point]bool{}
	for _, p := range points {
		if seen[p] {
			t.Errorf("duplicate point %q", p)
		}
		seen[p] = true
		if !IsKnownPoint(string(p)) {
			t.Errorf("IsKnownPoint(%q) = false for a registered point", p)
		}
	}
	points[0] = "acme.not-a-point"
	if IsKnownPoint("acme.not-a-point") {
		t.Error("mutating the returned slice extended the closed registry")
	}
	if KnownPoints()[0] == "acme.not-a-point" {
		t.Error("KnownPoints returns its backing array, not a copy")
	}
}

// TestHostOwnedPointsAreNotExtensible is the absence that matters.
//
// host.auth.method and host.system.action are the host's own. A plugin that
// could supply an authentication method could substitute itself for the
// gateway's authentication; one that could supply a system action would run it
// under the host's authority. Neither is an extension point, so a manifest
// naming one must FAIL validation rather than be quietly ignored — and it fails
// because IsKnownPoint says false here.
func TestHostOwnedPointsAreNotExtensible(t *testing.T) {
	for _, name := range []string{"host.auth.method", "host.system.action"} {
		if IsKnownPoint(name) {
			t.Errorf("IsKnownPoint(%q) = true; the host owns it and it must not be extensible", name)
		}
		if p, ok := SuggestPoint(name); ok {
			t.Errorf("SuggestPoint(%q) = %q; suggesting a spelling implies the point exists", name, p)
		}
	}
}

func TestSuggestPointResolvesNearMisses(t *testing.T) {
	cases := []struct {
		in    string
		want  Point
		found bool
	}{
		{"host.settings.section", PointSettingsSection, true},
		{"settings.section", PointSettingsSection, true},       // bare suffix of a host point
		{"session.backend", PointSessionBackend, true},         // same mistake, same fix
		{"host.settings.sections", PointSettingsSection, true}, // one insertion
		{"host.setings.section", PointSettingsSection, true},   // one deletion
		{"mcp.tool", PointMCPTool, true},
		{"mcp.tools", PointMCPTool, true},
		{"mcp.tolo", PointMCPTool, false}, // two edits away: no guess
		{"files.S3", PointFilesS3, true},  // case is normalised
		{"  acp.provider  ", PointACPProvider, true},
		{"terminal.presentation", PointTerminalPresentation, true},
		{"", "", false},
		{"totally.unrelated", "", false},
	}
	for _, tc := range cases {
		got, ok := SuggestPoint(tc.in)
		if ok != tc.found || (ok && got != tc.want) {
			t.Errorf("SuggestPoint(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.found)
		}
	}
}

// TestSuggestPointNeverWidensAdmission: a suggestion is a message, not an
// admission. Every name SuggestPoint resolves must resolve TO a known point,
// and a near miss must still be unknown.
func TestSuggestPointNeverWidensAdmission(t *testing.T) {
	for _, near := range []string{"settings.section", "mcp.tools", "host.setings.section"} {
		if IsKnownPoint(near) {
			t.Errorf("IsKnownPoint(%q) = true; a near miss must still fail validation", near)
		}
		p, ok := SuggestPoint(near)
		if !ok {
			t.Fatalf("SuggestPoint(%q) found nothing", near)
		}
		if !IsKnownPoint(string(p)) {
			t.Errorf("SuggestPoint(%q) = %q, which is not itself a known point", near, p)
		}
	}
}
