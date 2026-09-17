package pack

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/attest"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin/contract"
)

// packable writes the smallest directory that passes the contract in package
// mode, plus whatever extra files a test names.
func packable(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := map[string]any{
		"contract": 2,
		"kind":     "ui",
		"id":       "sampler",
		"version":  "1.2.3",
		"identity": map[string]any{
			"displayName": "Sampler",
			"description": "A package for the pack tests.",
		},
		"publisher": map[string]any{"id": "acme", "name": "Acme"},
		"targets":   []string{"gateway"},
		"static":    []any{map[string]any{"dir": "ui", "mount": "ui"}},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugin.json"), string(raw))
	write(t, filepath.Join(dir, "ui", "index.html"), "<!doctype html><title>Sampler</title>\n")
	for name, body := range extra {
		write(t, filepath.Join(dir, filepath.FromSlash(name)), body)
	}
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBDXIsReadableWithoutThePayload is the whole point of the container
// shape: a reader learns what a package is from two small members and never
// touches the payload.
func TestBDXIsReadableWithoutThePayload(t *testing.T) {
	dir := packable(t, nil)
	out := t.TempDir()
	res, err := WriteBDX(dir, out, contract.Options{Host: "gateway"})
	if err != nil {
		t.Fatalf("WriteBDX: %v (%d diagnostics)", err, len(res.Report.Diagnostics))
	}
	if filepath.Base(res.Path) != "sampler-1.2.3.bdx" {
		t.Fatalf("package is named %q", filepath.Base(res.Path))
	}
	got, err := Inspect(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest == nil || got.Manifest.ID != "sampler" || got.Manifest.Identity == nil || got.Manifest.Identity.DisplayName != "Sampler" {
		t.Fatalf("inspect did not read the manifest: %+v", got.Manifest)
	}
	if got.Attestation.V != attest.EnvelopeVersion || got.Attestation.SHA256 != res.PayloadSHA {
		t.Fatalf("envelope %+v does not carry the payload digest %s", got.Attestation, res.PayloadSHA)
	}
	if got.Attestation.Declares() != attest.FromDev {
		t.Fatalf("a freshly packed package claims %q; unsigned is dev", got.Attestation.Declares())
	}
	if got.PayloadSize <= 0 {
		t.Fatal("the payload size is not declared")
	}
	// The digest in the envelope is the digest of the bytes in the container.
	onDisk, err := PayloadSHA(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk != res.PayloadSHA {
		t.Fatalf("payload digest %s, envelope says %s", onDisk, res.PayloadSHA)
	}
}

// TestPackingIsReproducible: the payload digest is what a signature covers, so
// packing the same tree twice has to produce the same digest. Two packs a
// second apart with different file mtimes must agree.
func TestPackingIsReproducible(t *testing.T) {
	dir := packable(t, nil)
	first, err := WriteBDX(dir, t.TempDir(), contract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "plugin.json"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	second, err := WriteBDX(dir, t.TempDir(), contract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.PayloadSHA != second.PayloadSHA {
		t.Fatalf("the same tree packed to %s and then %s", first.PayloadSHA, second.PayloadSHA)
	}
}

// TestWriteBDXRefusesWhatTheContractRefuses: the author gate is the cheapest
// of the four, so nothing unverified gets written at all — not even to be
// fixed later.
func TestWriteBDXRefusesWhatTheContractRefuses(t *testing.T) {
	dir := packable(t, map[string]string{"node_modules/left-pad/index.js": "module.exports = 1\n"})
	out := t.TempDir()
	res, err := WriteBDX(dir, out, contract.Options{})
	if !errors.Is(err, ErrContract) {
		t.Fatalf("want ErrContract, got %v", err)
	}
	if res.Path != "" {
		t.Fatalf("a refused package was still named %q", res.Path)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused package left %d files behind", len(entries))
	}
	if res.Report.OK || len(res.Report.Diagnostics) == 0 {
		t.Fatal("the report that refused the package is not on the result")
	}
}

// TestPayloadIsTheHostsArchive: the inner member is the same single-rooted
// tar.gz hosts already extract, so the container is a wrapper and not a new
// extraction format.
func TestPayloadIsTheHostsArchive(t *testing.T) {
	dir := packable(t, map[string]string{"README.md": "# Sampler\n"})
	res, err := WriteBDX(dir, t.TempDir(), contract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := OpenPayload(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
		if hdr.ModTime.Unix() != 0 {
			t.Errorf("%s carries mtime %s; the payload must not record when it was built", hdr.Name, hdr.ModTime)
		}
		if hdr.Uid != 0 || hdr.Gid != 0 {
			t.Errorf("%s carries uid/gid %d/%d", hdr.Name, hdr.Uid, hdr.Gid)
		}
	}
	for _, want := range []string{"sampler/plugin.json", "sampler/README.md", "sampler/ui/index.html"} {
		if !slices.Contains(names, want) {
			t.Errorf("payload has no %s (got %v)", want, names)
		}
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "sampler/") {
			t.Errorf("%s is outside the single top-level directory", name)
		}
	}
}

// TestInspectRefusesSomethingThatIsNotAPackage: a file that happens to be a
// tar is not a package, and saying so by name beats a nil manifest downstream.
func TestInspectRefusesSomethingThatIsNotAPackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "impostor.bdx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	if err := tw.WriteHeader(&tar.Header{Name: "readme.txt", Size: 2, Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := Inspect(path); err == nil {
		t.Fatal("a tar with no package members was accepted")
	}
}
