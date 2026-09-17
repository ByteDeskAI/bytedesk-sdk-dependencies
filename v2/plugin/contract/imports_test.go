package contract

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const thisModule = "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2"

// allowedImports is the closed set: this module's plugin package and what it
// reaches inside the module, semver, the JSON Schema validator and the
// standard library. Nothing from any gateway, vault or store repository, ever
// — this package is what they all depend on, so a dependency the other way
// would be a cycle across repositories.
var allowedImports = []string{
	thisModule + "/plugin",
	thisModule + "/bus",
	"github.com/Masterminds/semver/v3",
	"github.com/santhosh-tekuri/jsonschema/v6",
}

// TestContractImportsAreClosed walks the non-test imports of this package and,
// transitively, of every in-module package it reaches, and refuses anything
// outside allowedImports. The transitive half is what keeps a future
// plugin-package import of a host repository from arriving here unnoticed.
func TestContractImportsAreClosed(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	visited := map[string]bool{}
	var visit func(pkgDir, pkgPath string)
	visit = func(pkgDir, pkgPath string) {
		if visited[pkgPath] {
			return
		}
		visited[pkgPath] = true
		for _, imp := range importsOf(t, pkgDir) {
			switch {
			case isStdlib(imp):
			case strings.Contains(imp, "bytedesk-remote-gateway"), strings.Contains(imp, "bytedesk-vault"), strings.Contains(imp, "bytedesk-store"):
				t.Errorf("%s imports %s: the contract package must not depend on any host", pkgPath, imp)
			case !isAllowed(imp):
				t.Errorf("%s imports %s, which is outside the closed set %v", pkgPath, imp, allowedImports)
			case strings.HasPrefix(imp, thisModule+"/"):
				visit(filepath.Join(moduleRoot, strings.TrimPrefix(imp, thisModule+"/")), imp)
			}
		}
	}
	visit(".", thisModule+"/plugin/contract")
	if len(visited) < 3 {
		t.Fatalf("visited only %v; the transitive walk is not reaching the plugin package", visited)
	}
}

func importsOf(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	var out []string
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, path)
			}
		}
	}
	return out
}

// isStdlib: the standard library has no dot in its first path element.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func isAllowed(path string) bool {
	for _, a := range allowedImports {
		if path == a || strings.HasPrefix(path, a+"/") {
			return true
		}
	}
	return false
}
