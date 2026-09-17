package pack

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/attest"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin/contract"
)

// A .bdx is one self-describing file: a plain (uncompressed) tar holding
//
//	plugin.json        a copy of the manifest, so a reader can list, gate or
//	                   display a package without extracting the payload
//	attestation.json   the envelope: payload digest and whatever signatures
//	                   the package has collected
//	package.tar.gz     the payload — the plugin directory, exactly as the host
//	                   will extract it
//
// The signatures cover package.tar.gz alone, which is why the payload is a
// single member rather than the package's files loose in the outer tar: the
// bytes that are signed are the bytes that exist, with no repacking between
// signing and verifying.
//
// The outer tar is uncompressed on purpose. The payload is already gzipped,
// compressing it twice buys nothing, and leaving the container plain means the
// two small members can be read with two seeks and no decompressor.
const (
	// Ext is the package extension, including the dot.
	Ext = ".bdx"

	memberManifest    = "plugin.json"
	memberAttestation = "attestation.json"
	memberPayload     = "package.tar.gz"

	// maxMetadataBytes caps the two small members a reader loads into memory
	// before anything about the package has been verified. A manifest is a
	// few KB and an envelope a few hundred bytes; this is three orders of
	// magnitude of headroom and still refuses a container that claims a 2GB
	// plugin.json.
	maxMetadataBytes = 4 << 20
)

// ErrContract is returned by WriteBDX when the directory does not verify. The
// Report on the result carries every diagnostic; the error exists so a caller
// that only checks err does not write an unverified package by accident.
var ErrContract = errors.New("pack: the plugin does not pass the contract")

// BDXResult is a written package.
type BDXResult struct {
	Path    string
	ID      string
	Version string

	// PayloadSHA is the hex sha256 of package.tar.gz — the digest the
	// attestation carries and the signatures cover.
	PayloadSHA string

	// GrantsDigest fingerprints what an operator consents to. See Result.
	GrantsDigest string

	// Report is the verification that gated the write, kept so a caller can
	// print warnings for a package that was written anyway.
	Report contract.Report
}

// WriteBDX verifies dir and, if it passes, writes <id>-<version>.bdx into
// outDir. The package is unsigned: the envelope carries the payload digest and
// no signatures, which is what `bytedesk plugin sign` fills in later.
//
// Verification comes first and is not optional. A package that cannot be
// verified by its author is one every later gate refuses, and the author is
// the only one of the four who can still fix it cheaply.
func WriteBDX(dir, outDir string, opts contract.Options) (BDXResult, error) {
	var res BDXResult
	report, err := contract.VerifyDir(dir, opts)
	res.Report = report
	if err != nil {
		return res, err
	}
	if !report.OK {
		return res, ErrContract
	}
	m := report.Manifest
	if m == nil {
		return res, ErrContract
	}
	res.ID, res.Version, res.GrantsDigest = m.ID, m.Version, plugin.GrantsDigest(*m)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return res, err
	}
	stage, err := os.MkdirTemp("", "plugin-bdx-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(stage)

	payload := filepath.Join(stage, memberPayload)
	if err := writePayload(dir, payload, m.ID); err != nil {
		return res, err
	}
	res.PayloadSHA, err = fileSHA(payload)
	if err != nil {
		return res, err
	}
	envelope, err := json.MarshalIndent(attest.Envelope{V: attest.EnvelopeVersion, SHA256: res.PayloadSHA}, "", "  ")
	if err != nil {
		return res, err
	}
	manifest, err := os.ReadFile(filepath.Join(dir, memberManifest))
	if err != nil {
		return res, err
	}

	res.Path = filepath.Join(outDir, m.ID+"-"+m.Version+Ext)
	// The copy is written whole and renamed, so a reader never sees a
	// half-written package with a valid-looking name.
	tmp := res.Path + ".partial"
	if err := writeContainer(tmp, manifest, envelope, payload); err != nil {
		os.Remove(tmp)
		return res, err
	}
	if err := os.Rename(tmp, res.Path); err != nil {
		os.Remove(tmp)
		return res, err
	}
	return res, nil
}

// writeContainer writes the outer tar: metadata first so a reader gets both
// small members before the payload, then the payload streamed from disk.
func writeContainer(path string, manifest, envelope []byte, payload string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	for _, member := range []struct {
		name string
		body []byte
	}{{memberManifest, manifest}, {memberAttestation, envelope}} {
		if err := tw.WriteHeader(&tar.Header{Name: member.name, Mode: 0o644, Size: int64(len(member.body)), Format: tar.FormatPAX}); err != nil {
			return err
		}
		if _, err := tw.Write(member.body); err != nil {
			return err
		}
	}
	info, err := os.Stat(payload)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: memberPayload, Mode: 0o644, Size: info.Size(), Format: tar.FormatPAX}); err != nil {
		return err
	}
	src, err := os.Open(payload)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := io.Copy(tw, src); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// writePayload tars dir under a single top-level <id>/ directory, exactly as
// the legacy archive did, so a host's existing extractor reads it unchanged.
//
// Every header field that records WHEN or WHO rather than WHAT is zeroed, and
// the mode is flattened to one of two values. Packing the same tree twice must
// produce the same bytes: the payload digest is what a signature covers, and a
// digest that moves with the clock would make every rebuild look like tampering.
func writePayload(dir, out, id string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	// WalkDir visits lexically, so member order is fixed by the tree alone.
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.Contains(rel, "..") {
			return fmt.Errorf("pack: refusing path %s", rel)
		}
		// The legacy packer skipped these three; the contract now refuses a
		// package that ships them, so this is belt and braces for a caller
		// that packs with a relaxed mode.
		switch filepath.Base(path) {
		case ".git", "SHA256SUMS", ".unsigned":
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !d.IsDir() {
			// Symlinks and devices are dropped rather than followed. The
			// host's extractor drops them too; agreeing here means what was
			// verified is what is packed.
			return nil
		}
		hdr := &tar.Header{
			Name:   filepath.ToSlash(filepath.Join(id, rel)),
			Mode:   0o644,
			Format: tar.FormatPAX,
		}
		switch {
		case d.IsDir():
			hdr.Typeflag, hdr.Mode, hdr.Name = tar.TypeDir, 0o755, hdr.Name+"/"
		default:
			hdr.Typeflag, hdr.Size = tar.TypeReg, info.Size()
			if info.Mode()&0o111 != 0 {
				hdr.Mode = 0o755
			}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// BDX is what a reader learns from a package without extracting it.
type BDX struct {
	Path        string
	Manifest    *plugin.Manifest
	Attestation attest.Envelope

	// PayloadSize is the compressed size of package.tar.gz as the container
	// declares it. It is a header value, not a measurement: a caller sizing a
	// download can use it, a caller enforcing a limit must check the bytes.
	PayloadSize int64
}

// Inspect reads a package's manifest and attestation and stops. It never
// decompresses the payload, so listing a directory of packages costs two small
// reads each, and a corrupt or hostile payload cannot affect a reader that only
// wanted to know what the package claims to be.
//
// Nothing here is verified: the manifest is the copy the packer put in the
// container, and the envelope's signatures are not checked. A gate calls
// VerifyDir on the extracted tree and attest.Verify on the envelope.
func Inspect(path string) (BDX, error) {
	out := BDX{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, fmt.Errorf("pack: %s is not a readable %s container: %w", filepath.Base(path), Ext, err)
		}
		switch hdr.Name {
		case memberManifest, memberAttestation:
			if hdr.Size > maxMetadataBytes {
				return out, fmt.Errorf("pack: %s is %d bytes, over the %d-byte limit", hdr.Name, hdr.Size, int64(maxMetadataBytes))
			}
			raw, err := io.ReadAll(io.LimitReader(tr, maxMetadataBytes))
			if err != nil {
				return out, err
			}
			seen[hdr.Name] = true
			if hdr.Name == memberManifest {
				var m plugin.Manifest
				if err := json.Unmarshal(raw, &m); err != nil {
					return out, fmt.Errorf("pack: %s in %s: %w", memberManifest, filepath.Base(path), err)
				}
				out.Manifest = &m
				continue
			}
			if err := json.Unmarshal(raw, &out.Attestation); err != nil {
				return out, fmt.Errorf("pack: %s in %s: %w", memberAttestation, filepath.Base(path), err)
			}
		case memberPayload:
			seen[hdr.Name] = true
			out.PayloadSize = hdr.Size
			// The payload is the last member and the only large one; there
			// is nothing after it worth reading.
			return out, missingMembers(path, seen)
		}
	}
	return out, missingMembers(path, seen)
}

func missingMembers(path string, seen map[string]bool) error {
	var missing []string
	for _, name := range []string{memberManifest, memberAttestation, memberPayload} {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("pack: %s is not a %s package: missing %s", filepath.Base(path), Ext, strings.Join(missing, ", "))
}

// OpenPayload returns package.tar.gz as a stream, for a host that is about to
// extract it with its own guarded extractor. This package deliberately does
// not extract: the limits, the zip-slip guards and the symlink policy belong
// to the host that owns the filesystem, and a second implementation here would
// be a second set of guards to keep in step.
//
// The caller closes the returned reader.
func OpenPayload(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		if hdr.Name == memberPayload {
			return payloadReader{Reader: io.LimitReader(tr, hdr.Size), closer: f}, nil
		}
	}
	f.Close()
	return nil, fmt.Errorf("pack: %s has no %s", filepath.Base(path), memberPayload)
}

type payloadReader struct {
	io.Reader
	closer io.Closer
}

func (p payloadReader) Close() error { return p.closer.Close() }

// PayloadSHA is the digest of a package's payload as it exists on disk, for a
// caller checking a package against its own envelope.
func PayloadSHA(path string) (string, error) {
	rc, err := OpenPayload(path)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
