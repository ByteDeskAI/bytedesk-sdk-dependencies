package plugin

import "testing"

func TestRequirementVersionMatching(t *testing.T) {
	cases := []struct {
		name, constraint, actual string
		match, fail              bool
	}{
		{"legacy empty", "", "historical-build", true, false},
		{"exact", "1.2.3", "1.2.3", true, false},
		{"exact mismatch", "1.2.3", "1.2.4", false, false},
		{"range", ">=1.2.0 <2.0.0", "1.9.2", true, false},
		{"caret major", "^1.2.0", "2.0.0", false, false},
		{"tilde minor", "~1.2.0", "1.3.0", false, false},
		{"or", "^1.0.0 || ^3.0.0", "3.1.0", true, false},
		{"wildcard", "1.2.x", "1.2.9", true, false},
		{"prerelease excluded", ">=1.2.0 <2.0.0", "1.3.0-beta.1", false, false},
		{"prerelease explicit", ">=1.3.0-0 <2.0.0", "1.3.0-beta.1", true, false},
		{"short numeric", ">=1.0.0", "1", true, false},
		{"prefix", "^1.2.0", "v1.2.3", true, false},
		{"invalid constraint", "nonsense", "1.2.3", false, true},
		{"invalid version", "^1.0.0", "historical-build", false, true},
		{"missing version", ">=1.0.0", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := (Requirement{ID: "peer", Version: c.constraint}).MatchesVersion(c.actual)
			if got != c.match || (err != nil) != c.fail {
				t.Fatalf("match=%v err=%v; want match=%v error=%v", got, err, c.match, c.fail)
			}
		})
	}
}

func TestManifestRejectsMalformedRequirementVersion(t *testing.T) {
	m := Manifest{ID: "consumer", Version: "1.0.0", Requires: []Requirement{{ID: "peer", Version: "not-a-range"}}}
	if err := m.Validate(); err == nil {
		t.Fatal("authoring accepted malformed constraint")
	}
	if err := m.ValidateDiscover(); err == nil {
		t.Fatal("discovery accepted malformed constraint")
	}
	m.Requires[0].Version = "^1.0.0"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}
