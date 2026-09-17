package pack

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

func fixture(t *testing.T) (dir string, m plugin.Manifest) {
	t.Helper()
	dir = t.TempDir()
	m = plugin.Manifest{
		Contract: plugin.ProtocolMajor,
		Kind:     plugin.KindBuiltin,
		ID:       "files",
		Version:  "1.2.3",
		Identity: &plugin.ManifestIdentity{DisplayName: "Files", Description: "Browse and share files."},
		Permissions: plugin.Permissions{
			Publish: []bus.Pattern{"event.other.>"},
		},
		Needs: []string{"durable", "kv"},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, m
}

// TestDirCarriesTheGrantsDigest is the one thing v2 added to this package, and
// it is the reason the digest is computed HERE: a digest printed by something
// other than the tool that built the archive is a digest of something else.
func TestDirCarriesTheGrantsDigest(t *testing.T) {
	dir, m := fixture(t)
	got, err := Dir(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "files" || got.Version != "1.2.3" || !got.Unsigned {
		t.Fatalf("Result = %+v", got)
	}
	want := plugin.GrantsDigest(m)
	if got.GrantsDigest != want {
		t.Fatalf("GrantsDigest = %q, want %q", got.GrantsDigest, want)
	}
	if got.GrantsDigest == "" {
		t.Fatal("an empty digest would key every consent entry to the same value")
	}
}

// TestDigestMovesWhenTheAskedForGrantsChange: the archive's digest is what an
// operator's approval is keyed by, so widening the ask must produce a different
// archive fingerprint rather than inheriting the earlier approval.
func TestDigestMovesWhenTheAskedForGrantsChange(t *testing.T) {
	dir, m := fixture(t)
	before, err := Dir(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	m.Permissions.Publish = append(m.Permissions.Publish, bus.Pattern("event.everything.>"))
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Dir(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if before.GrantsDigest == after.GrantsDigest {
		t.Fatal("widening the publish list did not move the digest; the operator would not be re-asked")
	}
}

// TestArchiveLayout pins what a host unpacks: one top-level directory named for
// the plugin, the manifest inside it, a SHA256SUMS and the .unsigned marker.
func TestArchiveLayout(t *testing.T) {
	dir, _ := fixture(t)
	out := t.TempDir()
	got, err := Dir(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Archive) != "files-1.2.3.tar.gz" {
		t.Fatalf("archive name = %q", filepath.Base(got.Archive))
	}

	f, err := os.Open(got.Archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seen[h.Name] = true
		if !strings.HasPrefix(h.Name, "files/") {
			t.Errorf("entry %q escapes the plugin's own directory", h.Name)
		}
	}
	for _, want := range []string{"files/plugin.json", "files/README.md", "files/SHA256SUMS", "files/.unsigned"} {
		if !seen[want] {
			t.Errorf("archive is missing %s", want)
		}
	}
}

// TestDirRefusesAnInvalidManifest: packing is the last gate before an archive
// is handed to a host, so a manifest that would fail validation there must fail
// here instead.
func TestDirRefusesAnInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"id":"files","version":"1.0.0","permissions":{"publish":["event.files.>"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(dir, t.TempDir()); err == nil {
		t.Fatal("packed a manifest that lists its own implicit namespace")
	}
}
