package contract

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// The BDP3xxx (layout), BDP4xxx (references) and BDP5002 (ignored files)
// bands: the tree against layout.json for the package's kind.

type layoutFile struct {
	Manifest string `json:"manifest"`
	Kinds    map[string]struct {
		Classes  []string `json:"classes"`
		Required []string `json:"required"`
	} `json:"kinds"`
	Classes map[string]struct {
		Globs      []string `json:"globs"`
		Extensions []string `json:"extensions"`
	} `json:"classes"`
	Forbidden struct {
		Globs []string `json:"globs"`
	} `json:"forbidden"`
	Ignored struct {
		Globs []string `json:"globs"`
	} `json:"ignored"`
	Limits struct {
		MaxFiles      int   `json:"maxFiles"`
		MaxBytes      int64 `json:"maxBytes"`
		MaxImageBytes int64 `json:"maxImageBytes"`
	} `json:"limits"`
}

var layoutSpec = sync.OnceValue(func() *layoutFile {
	raw, err := assets.ReadFile("v2/layout.json")
	if err != nil {
		panic("contract: embedded layout.json: " + err.Error())
	}
	var l layoutFile
	if err := json.Unmarshal(raw, &l); err != nil {
		panic("contract: embedded layout.json: " + err.Error())
	}
	return &l
})

// treeFile is one regular file under the root, path slash-separated and
// relative to it.
type treeFile struct {
	rel  string
	size int64
	mode fs.FileMode
}

// verifyTree walks root once. Every mode applies the symlink rule; package
// mode goes on to classify each file, apply the limits and check references.
// The returned error is a walk failure, not a verdict.
func verifyTree(r *Report, root string, m plugin.Manifest, doc any, decoded bool, mode Mode) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	var files []treeFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(p)
			if err != nil || !within(resolvedRoot, target) {
				r.add("BDP4003", rel, rel, root)
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, treeFile{rel: rel, size: info.Size(), mode: info.Mode()})
		return nil
	})
	if err != nil {
		return err
	}
	if mode == ModeSource || !decoded {
		return nil
	}
	lay := layoutSpec()
	kind := inferKind(m, files)
	spec := lay.Kinds[kind]
	binary := ""
	if kind == plugin.KindProcess {
		binary = path.Base(strings.TrimSpace(m.Binary))
	}
	var total int64
	for _, f := range files {
		total += f.size
		if f.rel == lay.Manifest {
			continue
		}
		if g := matchAny(lay.Forbidden.Globs, f.rel, true); g != "" {
			r.add("BDP3002", f.rel, f.rel, g)
			continue
		}
		if matchAny(lay.Ignored.Globs, f.rel, false) != "" {
			r.add("BDP5002", f.rel, f.rel)
			continue
		}
		class := classify(lay, spec.Classes, f.rel, binary)
		if class == "" {
			r.add("BDP3001", f.rel, f.rel, kind)
			continue
		}
		if class == "images" {
			ext := strings.ToLower(path.Ext(f.rel))
			if !contains(lay.Classes["images"].Extensions, ext) || f.size > lay.Limits.MaxImageBytes {
				r.add("BDP3005", f.rel, f.rel, lay.Limits.MaxImageBytes)
			}
		}
	}
	for _, req := range spec.Required {
		if !hasFile(files, req) {
			r.add("BDP3003", req, req)
		}
	}
	if len(files) > lay.Limits.MaxFiles {
		r.add("BDP3004", "", "maxFiles", len(files), lay.Limits.MaxFiles)
	}
	if total > lay.Limits.MaxBytes {
		r.add("BDP3004", "", "maxBytes", total, lay.Limits.MaxBytes)
	}
	verifyRefs(r, root, m, doc, kind)
	return nil
}

// verifyRefs follows the schema's x-bd-ref annotations. Only "binary" is
// checked against the tree today: it must exist at the root and be executable,
// and only for a process package, because another package kind's binary is
// nothing the host will run. "route" is recorded for tooling.
func verifyRefs(r *Report, root string, m plugin.Manifest, doc any, kind string) {
	refs := schemaRefs()
	paths := make([]string, 0, len(refs))
	for p := range refs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if refs[p] != "binary" || kind != plugin.KindProcess {
			continue
		}
		for at, value := range lookupPath(doc, p) {
			base := path.Base(strings.TrimSpace(value))
			if base == "" || base == "." || base == "/" {
				continue // BDP2040 already said so
			}
			info, err := os.Stat(filepath.Join(root, base))
			switch {
			case err != nil:
				r.add("BDP4001", at, value)
			case runtime.GOOS != "windows" && info.Mode()&0o111 == 0:
				r.add("BDP4002", at, value)
			}
		}
	}
}

// inferKind prefers the manifest's own kind field and falls back to the tree's
// evidence, in the order layout.json documents.
//
// The fallback is for manifests written before the field existed. It stays
// only until the v1 window closes: what a package IS should not depend on
// which files happen to be in it, and the ui/builtin split below is exactly
// that dependency — a ui package whose build has not run yet reads as a
// builtin and is judged by builtin's rules.
func inferKind(m plugin.Manifest, files []treeFile) string {
	if k := strings.ToLower(strings.TrimSpace(m.Kind)); k != "" {
		return k
	}
	switch {
	case m.Family != nil:
		return plugin.KindFamily
	case m.Spawn:
		return plugin.KindProcess
	case hasFile(files, "ui/index.html"):
		return plugin.KindUI
	default:
		return plugin.KindBuiltin
	}
}

// classify names the first allowed class whose globs match rel, or "".
func classify(lay *layoutFile, classes []string, rel, binary string) string {
	for _, name := range classes {
		for _, g := range lay.Classes[name].Globs {
			if g == "${binary}" {
				if binary == "" {
					continue
				}
				g = binary
			}
			if globMatch(g, rel) {
				return name
			}
		}
	}
	return ""
}

// matchAny returns the first glob matching rel. anyDepth also tries every
// suffix of rel's segments, which is how a forbidden name is refused wherever
// it sits.
func matchAny(globs []string, rel string, anyDepth bool) string {
	segs := strings.Split(rel, "/")
	for _, g := range globs {
		if globMatch(g, rel) {
			return g
		}
		if !anyDepth {
			continue
		}
		for i := 1; i < len(segs); i++ {
			if globMatch(g, strings.Join(segs[i:], "/")) {
				return g
			}
		}
	}
	return ""
}

// globMatch matches a slash-separated glob against a slash-separated path.
// "**" matches zero or more whole segments; everything else is path.Match
// segment by segment, so "*" never crosses a slash.
func globMatch(pattern, rel string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegments(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], segs[0])
	return err == nil && ok && matchSegments(pat[1:], segs[1:])
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hasFile(files []treeFile, rel string) bool {
	for _, f := range files {
		if f.rel == rel {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
