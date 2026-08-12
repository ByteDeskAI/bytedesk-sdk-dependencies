// Package pack builds the common plugin tar.gz layout used by every host.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
)

// Result is the written archive.
type Result struct {
	Archive  string
	ID       string
	Version  string
	Unsigned bool
}

// Dir copies dir into a path-safe <id>-<version>.tar.gz under outDir.
// Callers should already have run host-specific ValidateDir (targets).
func Dir(dir, outDir string) (Result, error) {
	var zero Result
	m, err := plugin.LoadDir(dir)
	if err != nil {
		return zero, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return zero, err
	}
	stage, err := os.MkdirTemp("", "plugin-pack-")
	if err != nil {
		return zero, err
	}
	defer os.RemoveAll(stage)
	root := filepath.Join(stage, m.ID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return zero, err
	}
	if err := copyTree(dir, root); err != nil {
		return zero, err
	}
	if err := writeSHA256SUMS(root); err != nil {
		return zero, err
	}
	if err := os.WriteFile(filepath.Join(root, ".unsigned"), nil, 0o644); err != nil {
		return zero, err
	}
	out := filepath.Join(outDir, m.ID+"-"+m.Version+".tar.gz")
	if err := tarGz(stage, out, m.ID); err != nil {
		return zero, err
	}
	return Result{Archive: out, ID: m.ID, Version: m.Version, Unsigned: true}, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		if base == ".git" || base == "SHA256SUMS" || base == ".unsigned" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if strings.Contains(rel, "..") {
			return fmt.Errorf("refusing path %s", rel)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeSHA256SUMS(root string) error {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Base(path) == "SHA256SUMS" || filepath.Base(path) == ".unsigned" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		sum, err := fileSHA(f)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), f)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, filepath.ToSlash(rel))
	}
	return os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(b.String()), 0o644)
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func tarGz(stage, out, top string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.WalkDir(filepath.Join(stage, top), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, in)
		_ = in.Close()
		return copyErr
	})
}
