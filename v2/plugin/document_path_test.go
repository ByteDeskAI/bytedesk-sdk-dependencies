package plugin

import "testing"

// This file closes a gap flagged during review of PR TM-362: the
// document-path parser (ValidateDocumentPath, MatchDocumentPath,
// DocumentPathsOverlap) is a traversal-prevention parser reachable both at
// manifest-validate time and, per its own doc comment, at live routing time
// against a browser's location.pathname -- and had zero unit tests anywhere
// in this module. A regression here (an off-by-one in the catch-all-must-
// be-last check, or a broken ".."-after-decode check) would not have failed
// any test in that PR.

func TestValidateDocumentPathGrammar(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		valid   bool
		why     string
	}{
		{"/projects/:id", true, "a literal segment then a named parameter"},
		{"/files/*rest", true, "a terminal catch-all"},
		{"/a/b/c", true, "three literal segments"},
		{"/", false, "the bare root is refused explicitly"},
		{"", false, "empty pattern has no leading slash"},
		{"no-leading-slash", false, "must start with /"},
		{"/a//b", false, "an empty segment (doubled slash)"},
		{"/a/./b", false, "a literal dot segment"},
		{"/a/../b", false, "a literal dot-dot segment -- traversal in the DECLARATION itself"},
		{"/a/", false, "a trailing slash produces a trailing empty segment"},
		{"/:", false, "a named parameter with an empty name"},
		{"/:1abc", false, "a parameter name must start with a letter, not a digit"},
		{"/:name/:name", false, "a parameter name reused within one pattern"},
		{"/*", false, "a catch-all with an empty name"},
		{"/*rest/more", false, "a catch-all must be the LAST segment"},
		{"/a/*rest", true, "a catch-all as the last segment is fine"},
		{"/a!b", false, "! is outside the literal segment alphabet"},
		{"/a b", false, "a literal space is outside the alphabet"},
		{"/a.b_c~d-e", true, "literal segments may contain . _ ~ -"},
	} {
		err := ValidateDocumentPath(tc.pattern)
		if tc.valid && err != nil {
			t.Errorf("%q: want valid (%s), got %v", tc.pattern, tc.why, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("%q: want refused (%s), got valid", tc.pattern, tc.why)
		}
	}
}

func TestMatchDocumentPathRejectsTraversalAfterDecoding(t *testing.T) {
	pattern := "/files/:name"
	for _, tc := range []struct {
		path  string
		match bool
		why   string
	}{
		{"/files/report.pdf", true, "an ordinary filename"},
		{"/files/..", false, "a literal .. segment"},
		{"/files/%2e%2e", false, "URL-encoded .. must be caught AFTER exactly one decode"},
		{"/files/%2e%2e%2f%2e%2e%2fetc%2fpasswd", false, "an encoded segment that decodes to something containing a slash is refused, not silently traversed"},
		{"/files/a%2fb", false, "a decoded segment containing / would let one path segment smuggle two"},
		{"/files/a%5cb", false, "a decoded segment containing \\ is refused the same way as /"},
		{"/files/%00", false, "a decoded control character (NUL) is refused"},
		{"/files/%0a", false, "a decoded control character (newline) is refused"},
		{"/files/report.pdf?x=1", false, "MatchDocumentPath takes an escaped PATH, not a URL; a query string is refused outright"},
		{"/files/report.pdf#frag", false, "a fragment is refused outright"},
		{"files/report.pdf", false, "missing leading slash"},
		{"/files/%ff", false, "a byte that is not valid UTF-8 once decoded is refused"},
	} {
		_, ok := MatchDocumentPath(pattern, tc.path)
		if ok != tc.match {
			t.Errorf("MatchDocumentPath(%q, %q) = %v, want %v (%s)", pattern, tc.path, ok, tc.match, tc.why)
		}
	}
}

func TestMatchDocumentPathExtractsParametersAndCatchAll(t *testing.T) {
	params, ok := MatchDocumentPath("/projects/:id/files/*rest", "/projects/proj-1/files/a/b/c.txt")
	if !ok {
		t.Fatal("a well-formed match was refused")
	}
	if params["id"] != "proj-1" {
		t.Errorf("id=%q, want proj-1", params["id"])
	}
	if params["rest"] != "a/b/c.txt" {
		t.Errorf("rest=%q, want a/b/c.txt", params["rest"])
	}

	if _, ok := MatchDocumentPath("/projects/:id", "/projects/a/b"); ok {
		t.Error("a pattern with no catch-all matched a path with an extra segment")
	}
	if _, ok := MatchDocumentPath("/projects/:id/name", "/projects/a"); ok {
		t.Error("a shorter path matched a longer, non-catch-all pattern")
	}
	if _, ok := MatchDocumentPath("/projects/fixed", "/projects/other"); ok {
		t.Error("a literal segment matched a different literal value")
	}
}

func TestDocumentPathsOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b    string
		overlap bool
		why     string
	}{
		{"/projects/:id", "/projects/:name", true, "two named-parameter patterns at the same position always overlap"},
		{"/projects/fixed", "/projects/:id", true, "a literal is covered by a parameter at the same position"},
		{"/projects/a", "/projects/b", false, "two different literals at the same position never overlap"},
		{"/projects/*rest", "/projects/anything/at/all", true, "a catch-all overlaps anything it can reach"},
		{"/projects/:id", "/projects/:id/extra", false, "different lengths with no catch-all on the shorter side cannot overlap"},
		{"/a/:x/c", "/a/:y/c", true, "literal tails matching with a parameter in between"},
		{"/a/:x/c", "/a/:y/d", false, "literal tails that differ refuse the overlap"},
	} {
		got, err := DocumentPathsOverlap(tc.a, tc.b)
		if err != nil {
			t.Fatalf("DocumentPathsOverlap(%q, %q): %v", tc.a, tc.b, err)
		}
		if got != tc.overlap {
			t.Errorf("DocumentPathsOverlap(%q, %q) = %v, want %v (%s)", tc.a, tc.b, got, tc.overlap, tc.why)
		}
	}
	if _, err := DocumentPathsOverlap("not-a-pattern", "/a"); err == nil {
		t.Error("an invalid first pattern did not return an error")
	}
	if _, err := DocumentPathsOverlap("/a", "not-a-pattern"); err == nil {
		t.Error("an invalid second pattern did not return an error")
	}
}
