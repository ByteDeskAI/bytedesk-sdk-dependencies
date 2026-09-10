package main

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestBrowserContractsMatchCanonicalGoModel(t *testing.T) {
	want, err := os.ReadFile("../../typescript/contracts.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	got, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("browser contracts drifted; run go run ./cmd/plugin-typescript -out typescript/contracts.d.ts")
	}
}

func TestGeneratedArtifactsMatchCheckedInCopies(t *testing.T) {
	for _, tc := range []struct{ emit, pkg, path string }{
		{"js", "plugin", "../../typescript/validators.js"},
		{"go", "plugin", "../../plugin/payload_gen.go"},
		{"go", "consumer", "consumer/payload_gen.go"},
	} {
		t.Run(tc.emit+"/"+tc.pkg, func(t *testing.T) {
			want, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := generate(tc.emit, tc.pkg)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s drifted; run go run ./cmd/plugin-typescript -emit=%s -package=%s -out %s", tc.path, tc.emit, tc.pkg, tc.path)
			}
		})
	}
}

// The sidecar is byte-compared like the generated code, so a hash change can
// never land without the canonical AST that explains it.
func TestSchemaSidecarMatchesCheckedInCopy(t *testing.T) {
	want, err := os.ReadFile("../../typescript/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := generate("json", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("schemas.json drifted; run go run ./cmd/plugin-typescript -emit=json -out typescript/schemas.json")
	}
}

// The messaging package's descriptors are hand-maintained by that session,
// while the sidecar is generated from this package's target table. Nothing in
// the compiler couples the two, so this asserts the property the sidecar exists
// for: every hash that ships in a descriptor has an entry carrying its
// canonical AST, and nothing in the sidecar has been dropped from the
// descriptors. Without it, an operation added over there is simply missing from
// the artifact and its hash has nothing to diff against on mismatch.
func TestEveryShippedMessagingDescriptorHasASidecarEntry(t *testing.T) {
	src, err := os.ReadFile("../../messaging/payload_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	shipped := map[string]bool{}
	for _, m := range regexp.MustCompile(`NewDescriptor\([^)]*"([a-f0-9]{64})"\)`).FindAllStringSubmatch(string(src), -1) {
		shipped[m[1]] = true
	}
	if len(shipped) == 0 {
		t.Fatal("found no descriptors in messaging/payload_gen.go; this guard is not looking where it thinks it is")
	}

	raw, err := os.ReadFile("../../typescript/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Operations map[string]map[string]struct {
			Hash      string   `json:"hash"`
			Canonical []string `json:"canonical"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	recorded := map[string]string{}
	for name, revs := range doc.Operations {
		if !strings.Contains(name, ".messaging.") {
			continue
		}
		for rev, entry := range revs {
			if len(entry.Canonical) == 0 {
				t.Errorf("%s rev %s has a hash with no canonical AST beside it", name, rev)
			}
			recorded[entry.Hash] = name + " rev " + rev
		}
	}
	for hash := range shipped {
		if _, ok := recorded[hash]; !ok {
			t.Errorf("descriptor hash %s ships in messaging/payload_gen.go with no sidecar entry.\n"+
				"Register the operation in the messaging target, then: go run ./cmd/plugin-typescript -emit=json -out typescript/schemas.json", hash)
		}
	}
	for hash, where := range recorded {
		if !shipped[hash] {
			t.Errorf("sidecar carries %s (%s) but no messaging descriptor ships it", hash, where)
		}
	}
}
