package sdkv2

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The operator's first rule for this SDK: all broker functionality is
// abstracted behind it. No nats type appears in any SDK-public signature, and
// plugin code never imports github.com/nats-io/*.
//
// These are the two halves of that rule, and they fail for different reasons:
// the source scan catches an import, the reflect walk catches a type that
// arrived through one. Neither alone is the fence — an import can exist with no
// exported type, and an exported type can arrive through a re-export.
//
// The gateway has just spent a whole task learning that a fence which cannot
// detect its own vacuity is not a fence. Both halves here are therefore
// accompanied by a test that breaks them on purpose and asserts they notice:
// TestReflectWalkDetectsAForeignType below, and
// TestEveryExportedTypeIsWalked in inventory_test.go.

// forbiddenImport is the prefix no file in this module may import.
const forbiddenImport = "github.com/nats-io/"

// TestNoBrokerImportInModule scans every Go file in the module, tests
// included, for a forbidden import.
//
// Tests are NOT exempt here, unlike in the gateway. The gateway's
// internal/bus/natsembed is allowed to import nats-io because it IS the
// adapter; this module has no adapter and never will. The SDK's own broker
// client lives in the other repository (bytedesk-remote-gateway-plugin-sdk,
// v2/transport/natsconn), which is the second and last importer.
func TestNoBrokerImportInModule(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		scanned++
		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			if strings.HasPrefix(p, forbiddenImport) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s imports %q: the SDK abstracts the broker, it does not wrap it", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A scan that walked nothing would pass silently, which is the failure
	// mode this whole file exists to prevent.
	if scanned < 10 {
		t.Fatalf("scanned only %d Go files; the walk is not reaching the module", scanned)
	}
	t.Logf("scanned %d Go files", scanned)
}

// TestGoModRequiresNoBroker is the dependency-graph half: even a transitive
// require would put nats-io in every consumer's build, so the module graph is
// checked, not just the import list.
func TestGoModRequiresNoBroker(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), forbiddenImport) {
		t.Fatalf("go.mod names %s; the v2 module must not depend on the broker at all", forbiddenImport)
	}
	sum, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.sum"))
	if err == nil && strings.Contains(string(sum), forbiddenImport) {
		t.Fatalf("go.sum names %s", forbiddenImport)
	}
}

// TestNoForeignTypeInPublicSignatures is the reflect half of the rule: it
// walks every exported type of the SDK's public packages — struct fields,
// method parameters and results, interface methods, and everything they point
// at — and fails if any of them was declared in a foreign package.
//
// inventory_test.go proves this walk visits every exported type there is, so a
// type added tomorrow cannot escape it by not being on a list.
func TestNoForeignTypeInPublicSignatures(t *testing.T) {
	w := newWalker(isBrokerPkg)
	for _, rt := range publicTypes() {
		w.walk(rt, rt.String())
	}
	for _, f := range w.findings {
		t.Errorf("%s reaches %s, declared in %s", f.where, f.typ, f.pkg)
	}
	if w.visited() < 40 {
		t.Fatalf("walked only %d types; the walk is not reaching the API", w.visited())
	}
	t.Logf("walked %d distinct types across %d roots", w.visited(), len(publicTypes()))
}

// TestReflectWalkDetectsAForeignType breaks the walk on purpose.
//
// It runs the same walker with a predicate that treats the SDK's own bus
// package as foreign, over a type that reaches one. If the walk cannot find a
// type it is pointed straight at, TestNoForeignTypeInPublicSignatures passing
// means nothing at all.
func TestReflectWalkDetectsAForeignType(t *testing.T) {
	foreign := func(pkg string) bool { return strings.HasSuffix(pkg, "/v2/bus") }

	for _, tc := range []struct {
		name string
		rt   reflect.Type
	}{
		{"through a struct field", reflect.TypeOf(probeField{})},
		{"through a method result", reflect.TypeOf(probeMethod{})},
		{"through an interface method parameter", reflect.TypeOf((*probeIface)(nil)).Elem()},
		{"through a slice of a map value", reflect.TypeOf(probeNested{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWalker(foreign)
			w.walk(tc.rt, tc.rt.String())
			if len(w.findings) == 0 {
				t.Fatalf("walker missed a foreign type reachable %s: the real gate is vacuous", tc.name)
			}
		})
	}

	// And the negative: a type that reaches nothing foreign produces nothing,
	// so the walker is not simply reporting everything it sees.
	w := newWalker(foreign)
	w.walk(reflect.TypeOf(probeClean{}), "probeClean")
	if len(w.findings) != 0 {
		t.Fatalf("walker reported %d findings for a clean type: it flags indiscriminately", len(w.findings))
	}
}

// isBrokerPkg is the real predicate: a type declared under any nats-io module.
func isBrokerPkg(pkg string) bool {
	return strings.Contains(pkg, "nats-io") || strings.HasPrefix(pkg, forbiddenImport)
}

// walker is a cycle-safe type traversal. It records where each foreign type
// was reached from, because "a nats type is exported" is unactionable and
// "Streams.Consume's third parameter reaches nats.Msg" is not.
type walker struct {
	foreign  func(pkg string) bool
	seen     map[reflect.Type]bool
	findings []finding
}

type finding struct {
	where string
	typ   string
	pkg   string
}

func newWalker(foreign func(string) bool) *walker {
	return &walker{foreign: foreign, seen: map[reflect.Type]bool{}}
}

func (w *walker) visited() int { return len(w.seen) }

func (w *walker) walk(t reflect.Type, where string) {
	if t == nil || w.seen[t] {
		return
	}
	w.seen[t] = true

	if p := t.PkgPath(); p != "" && w.foreign(p) {
		w.findings = append(w.findings, finding{where: where, typ: t.String(), pkg: p})
		return
	}

	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
		w.walk(t.Elem(), where+" -> elem")
	case reflect.Map:
		w.walk(t.Key(), where+" -> key")
		w.walk(t.Elem(), where+" -> value")
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			w.walk(f.Type, where+"."+f.Name)
		}
	case reflect.Func:
		w.walkFunc(t, where)
	case reflect.Interface:
		for i := 0; i < t.NumMethod(); i++ {
			m := t.Method(i)
			w.walkFunc(m.Type, where+"."+m.Name)
		}
	}

	// Methods on the concrete type and on its pointer.
	w.walkMethods(t, where)
	if t.Kind() != reflect.Ptr && t.Kind() != reflect.Interface {
		w.walkMethods(reflect.PointerTo(t), where)
	}
}

func (w *walker) walkMethods(t reflect.Type, where string) {
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		w.walkFunc(m.Type, where+"."+m.Name)
	}
}

func (w *walker) walkFunc(ft reflect.Type, where string) {
	if ft.Kind() != reflect.Func {
		return
	}
	for i := 0; i < ft.NumIn(); i++ {
		w.walk(ft.In(i), where+" arg"+strconv.Itoa(i))
	}
	for i := 0; i < ft.NumOut(); i++ {
		w.walk(ft.Out(i), where+" ret"+strconv.Itoa(i))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
