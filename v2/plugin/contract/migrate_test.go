package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// The migration corpus under v2/migrations is the proof of MigrateV1, the same
// way v2/fixtures is the proof of VerifyDir. Every case is a v1 plugin
// directory, an input.json carrying the decisions a migration cannot read off
// the manifest, and either migrated.json (the exact bytes) or an expected.json
// naming the error. A binding in another language is conformant when it
// produces the same bytes.

const migrationsRoot = "v2/migrations"

type migrationExpectation struct {
	Note  string `json:"note"`
	Error string `json:"error"`
	Notes []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Path     string `json:"path"`
	} `json:"notes"`
}

func TestMigrationCorpus(t *testing.T) {
	entries, err := os.ReadDir(migrationsRoot)
	if err != nil {
		t.Fatal(err)
	}
	walked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		walked++
		t.Run(entry.Name(), func(t *testing.T) {
			dir := filepath.Join(migrationsRoot, entry.Name())
			raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			in := readMigrateInput(t, dir)
			in.Dir = dir
			exp := readMigrationExpectation(t, dir)

			out, notes, err := MigrateV1(raw, in)
			if exp.Error != "" {
				if err == nil {
					t.Fatalf("want an error containing %q, got a migration:\n%s", exp.Error, out)
				}
				if !strings.Contains(err.Error(), exp.Error) {
					t.Fatalf("error %q does not contain %q", err, exp.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("MigrateV1: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(dir, "migrated.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, want) {
				t.Fatalf("migrated bytes differ\n got:\n%s\nwant:\n%s", out, want)
			}
			gotNotes := make([]string, 0, len(notes))
			for _, d := range notes {
				gotNotes = append(gotNotes, d.Code+" "+string(d.Severity)+" "+d.Path)
			}
			wantNotes := make([]string, 0, len(exp.Notes))
			for _, d := range exp.Notes {
				wantNotes = append(wantNotes, d.Code+" "+d.Severity+" "+d.Path)
			}
			slices.Sort(wantNotes)
			if !slices.Equal(gotNotes, wantNotes) {
				t.Fatalf("notes differ\n got: %s\nwant: %s", gotNotes, wantNotes)
			}
			// The notes ARE the verifier's findings, so a case with none must
			// verify. Anything else would let migrate report success on a
			// manifest the next gate refuses.
			if len(exp.Notes) == 0 {
				if report := Verify(out, Options{}); !report.OK {
					t.Fatalf("a migration with no notes must verify: %s", describe(report))
				}
			}
		})
	}
	if walked < 6 {
		t.Fatalf("walked only %d migration cases; the corpus is not being read", walked)
	}
}

// TestMigrationIsDeterministic is AC 1: the same fixture migrated twice is
// byte-identical, including key order, across separately decoded documents.
func TestMigrationIsDeterministic(t *testing.T) {
	dir := filepath.Join(migrationsRoot, "spawn-string-publisher")
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	in := readMigrateInput(t, dir)
	in.Dir = dir
	first, _, err := MigrateV1(raw, in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, _, err := MigrateV1(slices.Clone(raw), in)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, again)
		}
	}
}

// TestMigrationPreservesUntouchedKeyOrder: a document whose keys are in an
// order no tidy rewrite would choose keeps that order, because a migration
// diff a reviewer cannot read is a migration nobody reviews.
func TestMigrationPreservesUntouchedKeyOrder(t *testing.T) {
	raw := []byte(`{"targets":["gateway"],"zzz":1,"role":"system","id":"keeper","aaa":2,"version":"1.0.0","kind":"builtin"}`)
	out, _, err := MigrateV1(raw, MigrateInput{FirstParty: true, DisplayName: "Keeper", Description: "Keeps things."})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.Token()
	for dec.More() {
		key, _ := dec.Token()
		var skip json.RawMessage
		dec.Decode(&skip)
		order = append(order, key.(string))
	}
	want := []string{"contract", "targets", "zzz", "role", "id", "aaa", "version", "identity", "kind", "publisher"}
	if !slices.Equal(order, want) {
		t.Fatalf("key order is %v, want %v", order, want)
	}
}

// TestKindInferenceFallsBackToTheTree covers the two rules the committed
// corpus cannot carry: a directory holding plugin.go is a builtin, and one
// serving ui/index.html with no executable is a ui package. A committed
// fixture named plugin.go would be compiled as Go source.
func TestKindInferenceFallsBackToTheTree(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"plugin.go is a builtin", map[string]string{"plugin.go": "package plugins\n"}, plugin.KindBuiltin},
		{"ui/index.html is a ui package", map[string]string{"ui/index.html": "<!doctype html>"}, plugin.KindUI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				path := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, body)
			}
			out, _, err := MigrateV1([]byte(`{"id":"x","version":"1"}`), MigrateInput{
				Dir: dir, FirstParty: true, DisplayName: "X", Description: "Does x.",
			})
			if err != nil {
				t.Fatal(err)
			}
			var m plugin.Manifest
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatal(err)
			}
			if m.Kind != tc.want {
				t.Fatalf("kind is %q, want %q\n%s", m.Kind, tc.want, out)
			}
		})
	}
}

// TestDisplayNameFallingBackToTheIdWarns: the id is an address, not a name, so
// the fallback verifies with BDP2212 rather than passing silently.
func TestDisplayNameFallingBackToTheIdWarns(t *testing.T) {
	out, notes, err := MigrateV1([]byte(`{"id":"keeper","version":"1","kind":"builtin"}`),
		MigrateInput{FirstParty: true, Description: "Keeps things."})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`"displayName": "keeper"`)) {
		t.Fatalf("displayName did not fall back to the id:\n%s", out)
	}
	if !slices.ContainsFunc(notes, func(d plugin.Diagnostic) bool { return d.Code == "BDP2212" }) {
		t.Fatalf("want BDP2212 among the notes, got %v", notes)
	}
}

// TestAMissingDescriptionIsLeftEmpty: migrate never invents the sentence an
// operator reads. Empty is refused at verify (BDP2213), which is the point.
func TestAMissingDescriptionIsLeftEmpty(t *testing.T) {
	out, notes, err := MigrateV1([]byte(`{"id":"keeper","version":"1","kind":"builtin"}`),
		MigrateInput{FirstParty: true, DisplayName: "Keeper"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`"description": ""`)) {
		t.Fatalf("description was invented:\n%s", out)
	}
	if !slices.ContainsFunc(notes, func(d plugin.Diagnostic) bool {
		return d.Code == "BDP2213" && d.Severity == plugin.SeverityError
	}) {
		t.Fatalf("want BDP2213 as an error note, got %v", notes)
	}
}

func readMigrateInput(t *testing.T, dir string) MigrateInput {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "input.json"))
	if os.IsNotExist(err) {
		return MigrateInput{}
	}
	if err != nil {
		t.Fatal(err)
	}
	var in MigrateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatalf("%s/input.json: %v", dir, err)
	}
	return in
}

func readMigrationExpectation(t *testing.T, dir string) migrationExpectation {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exp migrationExpectation
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatalf("%s/expected.json: %v", dir, err)
	}
	return exp
}
