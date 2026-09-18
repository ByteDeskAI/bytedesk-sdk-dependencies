package contract

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// The corpus under v2/fixtures is THE proof of the contract. Every fixture is
// a plugin directory plus expected.json; the test copies it to a scratch
// directory (so links.json can be materialised and nothing on disk is a
// symlink the embed would refuse), runs VerifyDir with the fixture's options
// and compares the sorted (code, severity, path) tuples exactly. A binding in
// another language is conformant when it passes the same walk.

type expectedFile struct {
	Options     Options `json:"options"`
	Diagnostics []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Path     string `json:"path"`
		Message  string `json:"message,omitempty"`
	} `json:"diagnostics"`
	SkipOn []string `json:"skipOn"`
}

const fixturesRoot = "v2/fixtures"

func TestConformanceCorpus(t *testing.T) {
	walked := 0
	for _, group := range []string{"valid", "invalid"} {
		entries, err := os.ReadDir(filepath.Join(fixturesRoot, group))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			walked++
			name := group + "/" + entry.Name()
			t.Run(name, func(t *testing.T) {
				src := filepath.Join(fixturesRoot, group, entry.Name())
				exp := readExpected(t, src)
				if slices.Contains(exp.SkipOn, runtime.GOOS) {
					t.Skipf("fixture is not judged on %s", runtime.GOOS)
				}
				dir := materialise(t, src)
				report, err := VerifyDir(dir, exp.Options)
				if err != nil {
					t.Fatalf("VerifyDir: %v", err)
				}
				got := make([]string, 0, len(report.Diagnostics))
				for _, d := range report.Diagnostics {
					got = append(got, d.Code+" "+string(d.Severity)+" "+d.Path)
				}
				want := make([]string, 0, len(exp.Diagnostics))
				for _, d := range exp.Diagnostics {
					want = append(want, d.Code+" "+d.Severity+" "+d.Path)
				}
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Fatalf("diagnostics differ\n got: %s\nwant: %s\nfull: %s", got, want, describe(report))
				}
				for _, d := range exp.Diagnostics {
					if d.Message == "" {
						continue
					}
					if !slices.ContainsFunc(report.Diagnostics, func(r plugin.Diagnostic) bool {
						return r.Code == d.Code && r.Path == d.Path && r.Message == d.Message
					}) {
						t.Errorf("no %s at %q carries message %q", d.Code, d.Path, d.Message)
					}
				}
				if wantOK := group == "valid"; report.OK != wantOK {
					t.Fatalf("ok=%v, a %s fixture wants %v", report.OK, group, wantOK)
				}
				// Sorted output is the wire contract, not a courtesy.
				if !slices.IsSortedFunc(report.Diagnostics, func(a, b plugin.Diagnostic) int {
					return strings.Compare(a.Path+"\x00"+a.Code, b.Path+"\x00"+b.Code)
				}) {
					t.Fatal("report is not sorted by (path, code)")
				}
			})
		}
	}
	if walked < 50 {
		t.Fatalf("walked only %d fixtures; the corpus is not being read", walked)
	}
}

// TestEveryDiagnosticCodeHasAFixture makes the table and the corpus agree in
// both directions: a code nothing produces is dead, and a code a fixture
// expects that the table does not define is a typo the walk above would
// otherwise report as a mismatch far from its cause.
func TestEveryDiagnosticCodeHasAFixture(t *testing.T) {
	// Codes proven by a unit test instead of a fixture, with the test named.
	exempt := map[string]string{
		"BDP3004": "TestLimitsAreEnforced (a fixture over the limit would be too large to commit)",
		"BDP6020": "the gateway's direct-upload route (TM-407): reaching this code needs an attestation the manifest alone never carries, so no VerifyDir fixture can produce it — plugin.NewDiagnostic(\"BDP6020\", ...) is exercised by the gateway's own tests instead",
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(fixturesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "expected.json" {
			return err
		}
		exp := readExpected(t, filepath.Dir(path))
		for _, diag := range exp.Diagnostics {
			seen[diag.Code] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	table := plugin.DiagnosticCodes()
	for _, code := range table {
		if !seen[code] && exempt[code] == "" {
			t.Errorf("%s is defined in diagnostics.json but no fixture expects it", code)
		}
	}
	for code := range seen {
		if !slices.Contains(table, code) {
			t.Errorf("a fixture expects %s, which diagnostics.json does not define", code)
		}
	}
	if len(seen) < 80 {
		t.Fatalf("only %d distinct codes appear in fixtures; the corpus is thinner than the table", len(seen))
	}
}

// TestLimitsAreEnforced covers BDP3004 without committing a package over the
// limit: 5001 empty files, and a sparse file one byte over the byte ceiling.
func TestLimitsAreEnforced(t *testing.T) {
	limits := layoutSpec().Limits
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plugin.json"), `{"id":"big","version":"1.0.0"}`)
	writeFile(t, filepath.Join(dir, "README.md"), "#")
	f, err := os.Create(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(limits.MaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < limits.MaxFiles; i++ {
		writeFile(t, filepath.Join(dir, "docs", "d"+itoa(i)+".md"), "")
	}
	report, err := VerifyDir(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	for _, d := range report.Diagnostics {
		if d.Code == "BDP3004" {
			hits++
		}
	}
	if hits != 2 {
		t.Fatalf("want BDP3004 twice (files and bytes), got %s", describe(report))
	}
}

// TestVerifyBytesIsTheManifestHalfOfVerifyDir: the same manifest bands, no
// tree, so a Store publish gate can run on the bytes it has.
func TestVerifyBytesIsTheManifestHalfOfVerifyDir(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixturesRoot, "valid", "reference", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	report := Verify(raw, Options{Host: plugin.TargetVault})
	if report.OK || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "BDP6001" {
		t.Fatalf("want exactly BDP6001 for the wrong host, got %s", describe(report))
	}
	if report.Manifest == nil || report.Manifest.ID != "tmux-manager" {
		t.Fatal("the decoded manifest is not on the report")
	}
	if report := Verify(raw, Options{}); !report.OK {
		t.Fatalf("the reference manifest must verify host-neutral: %s", describe(report))
	}
}

// TestReportIsTheWireShape pins what a consumer parses: contract, root, ok and
// a diagnostics ARRAY, empty rather than null when there is nothing to say.
func TestReportIsTheWireShape(t *testing.T) {
	b, err := json.Marshal(Verify([]byte(`{"id":"x","version":"1.0.0","identity":{"displayName":"X","description":"A plugin."}}`), Options{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"contract":2`, `"ok":true`, `"diagnostics":[]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("report %s lacks %s", b, want)
		}
	}
	if strings.Contains(string(b), `"root"`) {
		t.Errorf("Verify has no root; the report should omit it: %s", b)
	}
	var back Report
	if err := json.Unmarshal(b, &back); err != nil || back.Contract != Contract {
		t.Fatalf("report does not round-trip: %v", err)
	}
}

// TestValidateStillReturnsTheFirstError is the signature promise: the
// error-returning entry points did not change meaning when the collector
// arrived. errors.As reaches the Diagnostic, and the first finding wins.
func TestValidateStillReturnsTheFirstError(t *testing.T) {
	_, err := plugin.ParseManifest([]byte(`{"version":"1.0.0","needs":["postgres"]}`))
	var d plugin.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("ParseManifest returned %T, want a plugin.Diagnostic", err)
	}
	if d.Code != "BDP2001" {
		t.Fatalf("first error is %s, want BDP2001 (id required runs before needs)", d.Code)
	}
	if !strings.Contains(err.Error(), "plugin id required") {
		t.Fatalf("wording changed: %q", err.Error())
	}
}

// TestAssetsServeTheContract: the three data files a binding reads are there.
func TestAssetsServeTheContract(t *testing.T) {
	for _, name := range []string{"plugin.schema.json", "layout.json", "diagnostics.json", "fixtures/valid/reference/plugin.json"} {
		if _, err := fs.Stat(Assets(), name); err != nil {
			t.Errorf("Assets() lacks %s: %v", name, err)
		}
	}
}

func readExpected(t *testing.T, dir string) expectedFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exp expectedFile
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatalf("%s/expected.json: %v", dir, err)
	}
	return exp
}

// materialise copies a fixture into a scratch directory, preserving file
// modes, and creates the symlinks links.json asks for.
func materialise(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." || rel == "expected.json" || rel == "links.json" {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(src, "links.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return dst
	}
	if err != nil {
		t.Fatal(err)
	}
	var links map[string]string
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatalf("links.json: %v", err)
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dst, name)); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("cannot create symlinks here: %v", err)
			}
			t.Fatal(err)
		}
	}
	return dst
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func describe(r Report) string {
	b, _ := json.MarshalIndent(r.Diagnostics, "", "  ")
	return string(b)
}

func itoa(i int) string { return strconv.Itoa(i) }

// TestTheContractFloorIsOneFlip: raising Options.MinAccepted to ContractV2 is
// the single change that closes the v1 dual-loading window, so it is tested
// over the whole corpus rather than one fixture. Every valid manifest that
// does not declare contract 2 — which is most of them, because the field is
// optional — is then refused on BDP1007 ALONE, and the one that declares it is
// untouched. A floor of 0 changes nothing, which is what keeps the window open.
func TestTheContractFloorIsOneFlip(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(fixturesRoot, "valid"))
	if err != nil {
		t.Fatal(err)
	}
	refused, accepted := 0, 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		src := filepath.Join(fixturesRoot, "valid", entry.Name())
		exp := readExpected(t, src)
		if slices.Contains(exp.SkipOn, runtime.GOOS) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, "plugin.json"))
		if err != nil {
			t.Fatal(err)
		}
		var declared struct {
			Contract int `json:"contract"`
		}
		if err := json.Unmarshal(raw, &declared); err != nil {
			t.Fatal(err)
		}
		opts := exp.Options
		opts.MinAccepted = ContractV2
		report, err := VerifyDir(materialise(t, src), opts)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case declared.Contract >= ContractV2:
			accepted++
			if !report.OK {
				t.Errorf("%s declares contract %d and must still pass: %s", entry.Name(), declared.Contract, describe(report))
			}
		default:
			refused++
			if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "BDP1007" || report.Diagnostics[0].Path != "contract" {
				t.Errorf("%s is a v1 document and must be refused on BDP1007 alone: %s", entry.Name(), describe(report))
			}
		}
	}
	if refused == 0 || accepted == 0 {
		t.Fatalf("the corpus proved only one side of the floor: %d refused, %d accepted", refused, accepted)
	}
}

// TestAliasReservationsComeFromTheHost pins the layering TM-421 corrected. The
// same manifest, claiming /setup, is fine for a host that does not serve /setup
// and refused by one that does — and no static list inside this package decides
// which. Before TM-421 the SDK carried a guessed list that missed /setup
// entirely, so this manifest passed everywhere; the first assertion is what
// that mistake looked like, and the second is the fix.
func TestAliasReservationsComeFromTheHost(t *testing.T) {
	dir := materialise(t, filepath.Join(fixturesRoot, "invalid", "6011-alias-reserved-by-the-host"))

	unreserved, err := VerifyDir(dir, Options{Host: plugin.TargetGateway})
	if err != nil {
		t.Fatal(err)
	}
	if !unreserved.OK {
		t.Fatalf("a host that reserves nothing must accept /setup: %s", describe(unreserved))
	}

	reserved, err := VerifyDir(dir, Options{Host: plugin.TargetGateway, HostReservations: []string{"/setup"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reserved.Diagnostics) != 1 || reserved.Diagnostics[0].Code != "BDP6011" {
		t.Fatalf("want exactly BDP6011: %s", describe(reserved))
	}
	if !strings.Contains(reserved.Diagnostics[0].Message, plugin.TargetGateway) {
		t.Errorf("the refusal does not name the host that reserved the route: %q", reserved.Diagnostics[0].Message)
	}

	// The prefix is a prefix: /setup reserves /setup/advanced too, and nothing
	// else. A reservation that matched on substring would refuse /setupwizard.
	for alias, want := range map[string]bool{"/setup/advanced": true, "/setupwizard": false} {
		raw := []byte(`{"contract":2,"kind":"ui","id":"x","version":"1.0.0","identity":{"displayName":"X","description":"A plugin."},"publisher":{"id":"acme","name":"Acme"},"targets":["gateway"],"aliases":["` + alias + `"]}`)
		got := Verify(raw, Options{Host: plugin.TargetGateway, HostReservations: []string{"/setup"}})
		if refused := !got.OK; refused != want {
			t.Errorf("alias %q refused=%v, want %v: %s", alias, refused, want, describe(got))
		}
	}
}

// TestNoDiagnosticRendersAFormattingArtifact catches a whole class of message
// bug at once: a template whose verbs and arguments disagree renders Go's
// "%!" marker straight into the text an author reads. BDP2002 shipped exactly
// that — its template takes no verb and the call site passed a message that
// duplicated the template — and nothing failed, because the corpus compares
// (code, severity, path) and only checks message text where a fixture pins it.
//
// Found by the Toolbox binding (TM-404), which could not reproduce the
// artifact and had to report it rather than imitate it.
func TestNoDiagnosticRendersAFormattingArtifact(t *testing.T) {
	checked := 0
	for _, group := range []string{"valid", "invalid"} {
		entries, err := os.ReadDir(filepath.Join(fixturesRoot, group))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			src := filepath.Join(fixturesRoot, group, entry.Name())
			exp := readExpected(t, src)
			if slices.Contains(exp.SkipOn, runtime.GOOS) {
				continue
			}
			report, err := VerifyDir(materialise(t, src), exp.Options)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range report.Diagnostics {
				checked++
				if strings.Contains(d.Message, "%!") {
					t.Errorf("%s/%s: %s renders a formatting artifact: %q",
						group, entry.Name(), d.Code, d.Message)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no diagnostic messages were examined; this test would pass vacuously")
	}
}
