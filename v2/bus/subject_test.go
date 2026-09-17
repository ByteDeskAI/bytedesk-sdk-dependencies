package bus

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSubject(t *testing.T) {
	for _, tc := range []struct {
		in  string
		ok  bool
		why string
	}{
		{in: "event.files.changed", ok: true},
		{in: "a", ok: true},
		{in: "cmd.files.v1.list", ok: true},
		{in: "tick.ops-health.sweep", ok: true, why: "a hyphen inside a token is ordinary"},
		{in: "kv_bucket.key", ok: true, why: "an underscore inside a token is ordinary"},
		{in: "$SYS.ACCOUNT.x", ok: true, why: "reserved tokens PARSE; the manifest validator is what refuses them"},
		{in: "_INBOX.files.abc", ok: true, why: "the inbox family has to be nameable to be routed"},

		{in: "", why: "empty"},
		{in: "a..b", why: "empty token"},
		{in: ".a", why: "empty leading token"},
		{in: "a.", why: "empty trailing token"},
		{in: "a.*.c", why: "a wildcard is not a concrete subject"},
		{in: "a.>", why: "a tail wildcard is not a concrete subject"},
		{in: "A.b", why: "uppercase"},
		{in: "-a.b", why: "a token may not start with a hyphen"},
		{in: "_a.b", why: "a token may not start with an underscore"},
		{in: "a.b!c", why: "punctuation"},
		{in: strings.Repeat("a.", MaxTokens) + "a", why: "17 tokens"},
		{in: strings.Repeat("a", MaxLen+1), why: "256 bytes"},
	} {
		_, err := ParseSubject(tc.in)
		if tc.ok && err != nil {
			t.Errorf("ParseSubject(%q) = %v, want ok (%s)", tc.in, err, tc.why)
		}
		if !tc.ok {
			if err == nil {
				t.Errorf("ParseSubject(%q) = ok, want refusal (%s)", tc.in, tc.why)
				continue
			}
			if !errors.Is(err, ErrSubject) {
				t.Errorf("ParseSubject(%q) error does not match ErrSubject: %v", tc.in, err)
			}
		}
	}
}

func TestParsePattern(t *testing.T) {
	for _, tc := range []struct {
		in  string
		ok  bool
		why string
	}{
		{in: "event.files.>", ok: true},
		{in: "event.*.changed", ok: true},
		{in: "*", ok: true},
		{in: ">", ok: true},
		{in: "*.*.>", ok: true},
		{in: "cmd.files.v1.list", ok: true, why: "every subject is a pattern"},

		{in: "a.>.b", why: `">" is only valid last`},
		{in: ">.a", why: `">" is only valid last`},
		{in: "a.b*", why: `"*" is a whole token, not a prefix`},
		{in: "a.*b", why: `"*" is a whole token, not a prefix`},
		{in: "", why: "empty"},
	} {
		_, err := ParsePattern(tc.in)
		if tc.ok != (err == nil) {
			t.Errorf("ParsePattern(%q): err=%v, want ok=%v (%s)", tc.in, err, tc.ok, tc.why)
		}
	}
}

func TestPatternMatches(t *testing.T) {
	for _, tc := range []struct {
		pattern Pattern
		subject Subject
		want    bool
		why     string
	}{
		{"a.b.c", "a.b.c", true, ""},
		{"a.b.c", "a.b.d", false, ""},

		{"a.*.c", "a.b.c", true, `"*" is one token`},
		{"a.*.c", "a.x.c", true, ""},
		{"a.*.c", "a.b.x.c", false, `"*" spans exactly one token, never two`},
		{"a.*.c", "a.c", false, `"*" is not optional`},
		{"a.*", "a.b", true, ""},
		{"a.*", "a.b.c", false, ""},

		{"a.>", "a.b", true, `">" is one or more`},
		{"a.>", "a.b.c.d", true, ""},
		{"a.>", "a", false, `">" is one or MORE: it does not match the empty tail`},
		{">", "a", true, ""},
		{">", "a.b", true, ""},

		{"event.files.>", "event.files.changed", true, ""},
		{"event.files.>", "event.other.changed", false, ""},
		{"$SYS.>", "$SYS.ACCOUNT.PLUGIN.CONNECT", true, "the deny set has to match what it denies"},
	} {
		if got := tc.pattern.Matches(tc.subject); got != tc.want {
			t.Errorf("Pattern(%q).Matches(%q) = %v, want %v %s", tc.pattern, tc.subject, got, tc.want, tc.why)
		}
	}
}

// TestPatternCovers is grant containment: p covers q when every subject q can
// match is one p can match. It is deliberately conservative, because a grant
// that over-covers hands out authority nobody approved.
func TestPatternCovers(t *testing.T) {
	for _, tc := range []struct {
		grant Pattern
		asked Pattern
		want  bool
		why   string
	}{
		{"a.b.c", "a.b.c", true, "identical"},
		{"a.b.c", "a.b.d", false, ""},

		{"a.>", "a.b", true, ""},
		{"a.>", "a.b.c", true, ""},
		{"a.>", "a.*", true, `"*" is one token and ">" is one or more`},
		{"a.>", "a.>", true, ""},
		{"a.>", "a", false, `">" needs at least one token`},
		{"a.>", "b.>", false, ""},

		{"a.*", "a.b", true, ""},
		{"a.*", "a.*", true, ""},
		{"a.*", "a.>", false, `">" reaches token counts "*" cannot`},
		{"a.*", "a.b.c", false, ""},

		{"*.b", "a.b", true, ""},
		{"*.>", "a.b.c", true, ""},
		{">", "anything.at.all", true, ""},
		{">", ">", true, ""},

		{"a.b", "a.>", false, "a literal never covers a family"},
		{"a.b", "a.*", false, ""},
	} {
		if got := tc.grant.Covers(tc.asked); got != tc.want {
			t.Errorf("Pattern(%q).Covers(%q) = %v, want %v %s", tc.grant, tc.asked, got, tc.want, tc.why)
		}
	}
}

// TestCoversImpliesMatches is the property that ties the two together: if a
// grant covers a pattern, every subject that pattern matches must also be
// matched by the grant. A mismatch here is a grant that admits a subscription
// and then refuses its traffic, or worse, the other way round.
func TestCoversImpliesMatches(t *testing.T) {
	grants := []Pattern{"a.>", "a.*", "a.b.c", "*.b.>", ">", "event.files.>"}
	asked := []Pattern{"a.b", "a.b.c", "a.*", "a.>", "event.files.changed", "x.b.c.d"}
	subjects := []Subject{"a", "a.b", "a.b.c", "a.b.c.d", "x.b.c.d", "event.files.changed", "event.other.x"}

	for _, g := range grants {
		for _, q := range asked {
			if !g.Covers(q) {
				continue
			}
			for _, s := range subjects {
				if q.Matches(s) && !g.Matches(s) {
					t.Errorf("grant %q covers %q but does not match %q, which %q does", g, q, s, q)
				}
			}
		}
	}
}

func TestCoveredByAnyAndMatchedByAny(t *testing.T) {
	grants := []Pattern{"event.files.>", "cmd.files.v1.*"}
	if !CoveredByAny(grants, "event.files.changed") {
		t.Error("CoveredByAny missed a covered pattern")
	}
	if CoveredByAny(grants, "event.other.changed") {
		t.Error("CoveredByAny admitted an uncovered pattern")
	}
	if !MatchedByAny(grants, "cmd.files.v1.list") {
		t.Error("MatchedByAny missed a matched subject")
	}
	if MatchedByAny(grants, "cmd.files.v2.list") {
		t.Error("MatchedByAny admitted an unmatched subject")
	}
	if CoveredByAny(nil, "a") || MatchedByAny(nil, "a") {
		t.Error("an empty grant list grants nothing")
	}
}

func TestIsReservedToken(t *testing.T) {
	for _, tok := range []string{"$SYS", "$JS", "_INBOX"} {
		if !IsReservedToken(tok) {
			t.Errorf("%q must be reserved", tok)
		}
	}
	for _, tok := range []string{"event", "files", "inbox", "sys"} {
		if IsReservedToken(tok) {
			t.Errorf("%q must not be reserved", tok)
		}
	}
}

func TestHasWildcard(t *testing.T) {
	for p, want := range map[Pattern]bool{
		"a.b":   false,
		"a.*":   true,
		"a.>":   true,
		"*":     true,
		"a.b.c": false,
	} {
		if got := p.HasWildcard(); got != want {
			t.Errorf("Pattern(%q).HasWildcard() = %v, want %v", p, got, want)
		}
	}
}
