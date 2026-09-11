package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"regexp"
	"slices"
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
	// path reads from the test's cwd; out is the same file from the repo root,
	// so the printed command is copy-pasteable where the generator is run.
	for _, tc := range []struct{ emit, pkg, path, out string }{
		{"js", "plugin", "../../typescript/validators.js", "typescript/validators.js"},
		{"go", "plugin", "../../plugin/payload_gen.go", "plugin/payload_gen.go"},
		{"go", "consumer", "consumer/payload_gen.go", "cmd/plugin-typescript/consumer/payload_gen.go"},
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
				t.Fatalf("%s drifted; run go run ./cmd/plugin-typescript -emit=%s -package=%s -out %s", tc.out, tc.emit, tc.pkg, tc.out)
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
// for: every operation that ships in a descriptor has an entry carrying its
// canonical AST, at the same revision and hash, and nothing in the sidecar has
// been dropped from the descriptors.
//
// It compares by operation NAME, not by hash, because payload_gen.go NAMES its
// constants — CommandMessageSend, ContractRevision — instead of inlining their
// values. What that file ships can therefore change without a single byte of it
// changing: bump ContractRevision in contract.go, or repoint a Command*
// constant, and the descriptors silently carry a revision or name the recorded
// hash was never computed over. A hash-keyed comparison passes straight through
// both. That is why the revision is checked explicitly even though schema-id
// already folds it into the digest — the digest only protects a value the file
// actually pins.
//
// The regexes below read constants and descriptors as source text. Reformatting
// either file so they stop matching does not fail silently: an unmatched
// descriptor is a sidecar entry nothing ships, and matching nothing at all trips
// the empty check.
func TestEveryShippedMessagingDescriptorMatchesItsSidecarEntry(t *testing.T) {
	const regen = "go run ./cmd/plugin-typescript -emit=json -out typescript/schemas.json"

	// payload_gen.go names its operation and revision constants, so resolve
	// them from the declaration a reader would consult.
	contract, err := os.ReadFile("../../messaging/contract.go")
	if err != nil {
		t.Fatal(err)
	}
	consts := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"([^"]*)"`).FindAllStringSubmatch(string(contract), -1) {
		consts[m[1]] = m[2]
	}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*(\d+)$`).FindAllStringSubmatch(string(contract), -1) {
		consts[m[1]] = m[2]
	}

	src, err := os.ReadFile("../../messaging/payload_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	type descriptor struct{ rev, hash string }
	shipped := map[string]descriptor{}
	for _, m := range regexp.MustCompile(`plugin\.NewDescriptor\(\s*(\w+),\s*(\w+),\s*"([a-f0-9]{64})"\s*\)`).FindAllStringSubmatch(string(src), -1) {
		name, ok := consts[m[1]]
		if !ok {
			t.Errorf("a descriptor names %s, which messaging/contract.go does not declare; this guard cannot resolve what it ships", m[1])
			continue
		}
		rev, ok := consts[m[2]]
		if !ok {
			t.Errorf("%s ships revision %s, which messaging/contract.go does not declare; this guard cannot resolve what it ships", name, m[2])
			continue
		}
		shipped[name] = descriptor{rev: rev, hash: m[3]}
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
	recorded := map[string]map[string]string{} // name -> revision -> hash
	for name, revs := range doc.Operations {
		if !strings.Contains(name, ".messaging.") {
			continue
		}
		for rev, entry := range revs {
			if len(entry.Canonical) == 0 {
				t.Errorf("%s rev %s has a hash with no canonical AST beside it", name, rev)
			}
			if recorded[name] == nil {
				recorded[name] = map[string]string{}
			}
			recorded[name][rev] = entry.Hash
		}
	}

	for name, got := range shipped {
		revs, ok := recorded[name]
		if !ok {
			t.Errorf("%s ships in messaging/payload_gen.go with no sidecar entry.\n"+
				"Register the operation in the messaging target, then: %s", name, regen)
			continue
		}
		want, ok := revs[got.rev]
		if !ok {
			t.Errorf("%s ships revision %s; the sidecar records revision(s) %s.\n"+
				"Align the messaging target's revision, then: %s",
				name, got.rev, strings.Join(slices.Sorted(maps.Keys(revs)), ", "), regen)
			continue
		}
		if got.hash != want {
			t.Errorf("%s rev %s ships hash %s; the sidecar records %s.\n"+
				"Regenerate: %s", name, got.rev, got.hash, want, regen)
		}
	}
	for name := range recorded {
		if _, ok := shipped[name]; !ok {
			t.Errorf("the sidecar carries %s but no messaging descriptor ships it.\n"+
				"Drop it from the messaging target, then: %s", name, regen)
		}
	}
}

// The .d.ts and the validators are one module surface: repo B ships them as
// contracts.d.ts beside contracts.js. A guard that exists at runtime with no
// declaration is a TS2305 on an import that works perfectly, which is a
// genuinely confusing failure for a plugin author. Declarations are not part of
// any schema, so this asserts surface parity only — no hash moves with it.
func TestDeclarationsCoverEveryEmittedValidator(t *testing.T) {
	names := func(pattern string, src []byte) map[string]bool {
		out := map[string]bool{}
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(string(src), -1) {
			out[m[1]] = true
		}
		return out
	}
	dts, err := generate("dts", "plugin")
	if err != nil {
		t.Fatal(err)
	}
	js, err := generate("js", "plugin")
	if err != nil {
		t.Fatal(err)
	}
	declared := names(`(?m)^export declare function (is\w+)\(`, dts)
	exported := names(`(?m)^export function (is\w+)\(`, js)
	if len(exported) == 0 {
		t.Fatal("found no validators in the js emission; this guard is not looking where it thinks it is")
	}
	for name := range exported {
		if !declared[name] {
			t.Errorf("validators.js exports %s with no declaration in contracts.d.ts (TS2305 for consumers)", name)
		}
	}
	for name := range declared {
		if !exported[name] {
			t.Errorf("contracts.d.ts declares %s but no validator ships it", name)
		}
	}
}
