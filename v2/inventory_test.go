package sdkv2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// publicPackages are the packages whose exported types form the SDK's public
// signature. bus/memory and bus/conformance are implementations of these
// contracts and are covered transitively: everything they expose is one of
// these types.
var publicPackages = []string{"bus", "plugin"}

// publicTypes is the registry the boundary walk starts from.
//
// It is written out by hand on purpose. A walk that discovered its own roots
// by reflection cannot exist in Go — a package's types are not enumerable at
// run time — so the alternative to a list is no walk at all. What makes the
// list safe is TestEveryExportedTypeIsWalked: it reads the same packages with
// go/doc and fails when a type is declared and not registered. Adding a type
// without adding it here breaks the build, which is the property a hand-written
// list normally lacks.
func publicTypes() []reflect.Type {
	return []reflect.Type{
		// bus — addressing
		reflect.TypeOf(bus.Subject("")),
		reflect.TypeOf(bus.Pattern("")),
		// bus — messages
		reflect.TypeOf(bus.Headers{}),
		reflect.TypeOf(bus.Msg{}),
		reflect.TypeOf(bus.Caller{}),
		reflect.TypeOf(bus.Fault{}),
		// bus — core surface
		reflect.TypeOf((*bus.Bus)(nil)).Elem(),
		reflect.TypeOf((*bus.Subscription)(nil)).Elem(),
		reflect.TypeOf((*bus.Handler)(nil)).Elem(),
		reflect.TypeOf(bus.Capabilities{}),
		reflect.TypeOf(bus.PublishOptions{}),
		reflect.TypeOf(bus.SubOptions{}),
		reflect.TypeOf(bus.ReqOptions{}),
		reflect.TypeOf((*bus.PublishOpt)(nil)).Elem(),
		reflect.TypeOf((*bus.SubOpt)(nil)).Elem(),
		reflect.TypeOf((*bus.ReqOpt)(nil)).Elem(),
		// bus — identity and grants
		reflect.TypeOf(bus.Identity{}),
		reflect.TypeOf(bus.Grants{}),
		reflect.TypeOf(bus.Lease{}),
		reflect.TypeOf(bus.Role("")),
		reflect.TypeOf(bus.GrantKind("")),
		reflect.TypeOf(bus.AssetKind("")),
		// bus — streams
		reflect.TypeOf(bus.Seq(0)),
		reflect.TypeOf(bus.Revision(0)),
		reflect.TypeOf(bus.Cursor{}),
		reflect.TypeOf(bus.Retention("")),
		reflect.TypeOf(bus.AckPolicy("")),
		reflect.TypeOf(bus.StartPolicy("")),
		reflect.TypeOf(bus.StreamSpec{}),
		reflect.TypeOf(bus.ConsumerSpec{}),
		reflect.TypeOf(bus.StreamInfo{}),
		reflect.TypeOf(bus.ConsumerInfo{}),
		reflect.TypeOf(bus.BatchMsg{}),
		reflect.TypeOf(bus.StreamMsg{}),
		reflect.TypeOf((*bus.StreamHandler)(nil)).Elem(),
		reflect.TypeOf((*bus.Streams)(nil)).Elem(),
		reflect.TypeOf((*bus.Consumer)(nil)).Elem(),
		// bus — kv and objects
		reflect.TypeOf(bus.BucketSpec{}),
		reflect.TypeOf(bus.Entry{}),
		reflect.TypeOf(bus.BucketStatus{}),
		reflect.TypeOf(bus.ObjectMeta{}),
		reflect.TypeOf((*bus.KV)(nil)).Elem(),
		reflect.TypeOf((*bus.Bucket)(nil)).Elem(),
		reflect.TypeOf((*bus.Watcher)(nil)).Elem(),
		reflect.TypeOf((*bus.Objects)(nil)).Elem(),
		reflect.TypeOf((*bus.ObjectWatcher)(nil)).Elem(),
		// bus — services
		reflect.TypeOf(bus.ServiceSpec{}),
		reflect.TypeOf(bus.EndpointSpec{}),
		reflect.TypeOf(bus.ServiceInfo{}),
		reflect.TypeOf(bus.EndpointInfo{}),
		reflect.TypeOf(bus.ServiceStats{}),
		reflect.TypeOf((*bus.Service)(nil)).Elem(),
		reflect.TypeOf((*bus.Services)(nil)).Elem(),
		// bus — scheduling and tracing
		reflect.TypeOf(bus.Schedule{}),
		reflect.TypeOf((*bus.Scheduler)(nil)).Elem(),
		reflect.TypeOf((*bus.Trace)(nil)).Elem(),

		// plugin — the base and the contract
		reflect.TypeOf(plugin.Base{}),
		reflect.TypeOf(plugin.Binding{}),
		reflect.TypeOf((*plugin.Bound)(nil)).Elem(),
		reflect.TypeOf((*plugin.Plugin)(nil)).Elem(),
		reflect.TypeOf((*plugin.Logger)(nil)).Elem(),
		reflect.TypeOf((*plugin.Profiler)(nil)).Elem(),
		// plugin — optional interfaces
		reflect.TypeOf((*plugin.HTTPPlugin)(nil)).Elem(),
		reflect.TypeOf((*plugin.HealthContributor)(nil)).Elem(),
		reflect.TypeOf((*plugin.Validator)(nil)).Elem(),
		reflect.TypeOf((*plugin.Readier)(nil)).Elem(),
		reflect.TypeOf((*plugin.ActivationChecker)(nil)).Elem(),
		reflect.TypeOf((*plugin.Draining)(nil)).Elem(),
		reflect.TypeOf((*plugin.DataVersioned)(nil)).Elem(),
		reflect.TypeOf(plugin.DataVersion("")),
		// plugin — extension points
		reflect.TypeOf(plugin.Point("")),
		// plugin — manifest v2
		reflect.TypeOf(plugin.Manifest{}),
		reflect.TypeOf(plugin.Permissions{}),
		reflect.TypeOf(plugin.ServiceDecl{}),
		reflect.TypeOf(plugin.EndpointDecl{}),
		reflect.TypeOf(plugin.StreamDecl{}),
		reflect.TypeOf(plugin.KVDecl{}),
		reflect.TypeOf(plugin.ObjectDecl{}),
		reflect.TypeOf(plugin.ProtocolRequirements{}),
		reflect.TypeOf(plugin.Config{}),
		reflect.TypeOf(plugin.ConfigSection{}),
		reflect.TypeOf(plugin.ConfigField{}),
		reflect.TypeOf(plugin.When{}),
		reflect.TypeOf(plugin.Family{}),
		reflect.TypeOf(plugin.FamilyMember{}),
		reflect.TypeOf(plugin.ExtensionPoint{}),
		reflect.TypeOf(plugin.Provider{}),
		reflect.TypeOf(plugin.Requirement{}),
		reflect.TypeOf(plugin.NavItem{}),
		reflect.TypeOf(plugin.PanelSpec{}),
		reflect.TypeOf(plugin.LauncherSpec{}),
		reflect.TypeOf(plugin.Pricing{}),
		reflect.TypeOf(plugin.Publisher{}),
		reflect.TypeOf(plugin.UIContribution{}),
		reflect.TypeOf(plugin.UIBinding{}),
		reflect.TypeOf(plugin.ContributionEligibility("")),
		reflect.TypeOf(plugin.ContributionRole{}),
		// plugin — the typed layer. Generics are registered at a concrete
		// instantiation; typeKey drops the type arguments, because the
		// contract is the generic type and not any one instantiation.
		reflect.TypeOf(plugin.Kind("")),
		reflect.TypeOf(plugin.Descriptor{}),
		reflect.TypeOf(plugin.Command[struct{}, struct{}]{}),
		reflect.TypeOf(plugin.Event[struct{}]{}),
		reflect.TypeOf(plugin.StreamDescriptor[struct{}]{}),
		reflect.TypeOf(plugin.BucketDescriptor[struct{}]{}),
		reflect.TypeOf(plugin.ServiceDescriptor{}),
		reflect.TypeOf(plugin.TypedStream[struct{}]{}),
		reflect.TypeOf(plugin.TypedBucket[struct{}]{}),
		reflect.TypeOf(plugin.Registrar{}),
	}
}

// TestEveryExportedTypeIsWalked is what makes the hand-written registry
// trustworthy. It reads every public package with go/doc and fails on any
// exported type name the registry does not carry.
//
// Without it, the boundary walk would be exactly as complete as whoever last
// remembered to update a list — which is to say, complete until the first type
// that mattered.
//
// Aliases are resolved rather than registered twice. `type Bus = bus.Bus` in
// package plugin IS bus.Bus at run time, so reflect can never report it under
// the plugin name; the check follows the alias to its target and requires THAT
// to be walked. An alias is also exactly what the SDK must not get wrong in the
// other direction — a local redefinition where an alias was meant is the drift
// the gateway's TestManifestTypesComeFromSDK exists to catch — so a declaration
// that is NOT an alias is required to be registered on its own.
func TestEveryExportedTypeIsWalked(t *testing.T) {
	registered := map[string]bool{}
	for _, rt := range publicTypes() {
		registered[typeKey(rt)] = true
	}

	declared, aliases := 0, 0
	var missing []string
	for _, pkgDir := range publicPackages {
		for _, d := range exportedTypes(t, pkgDir) {
			declared++
			want := pkgDir + "." + d.name
			if d.alias != "" {
				aliases++
				want = d.alias
			}
			if !registered[want] {
				missing = append(missing, pkgDir+"."+d.name+" (walk needs "+want+")")
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("%s is exported but the boundary walk does not reach it; add it to publicTypes()", m)
	}
	if declared < 40 {
		t.Fatalf("go/doc found only %d exported types across %v; the inventory is not reading the packages", declared, publicPackages)
	}
	t.Logf("go/doc resolved %d exported types (%d of them aliases); %d registered", declared, aliases, len(registered))
}

// typeDecl is one exported type as the source declares it. alias is the
// target's "<package>.<Name>" when the declaration is `type X = pkg.Y`.
type typeDecl struct {
	name  string
	alias string
}

// exportedTypes lists the exported types declared in pkgDir. It reads the
// source rather than the compiled package because that is the only view that
// sees a type nobody registered, and the only one that can tell an alias from
// a definition.
func exportedTypes(t *testing.T, pkgDir string) []typeDecl {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), pkgDir)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("public package %q is missing: %v", pkgDir, err)
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		// Test files declare helpers and fakes that are not the contract.
		return !hasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var out []typeDecl
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ast.IsExported(ts.Name.Name) {
						continue
					}
					d := typeDecl{name: ts.Name.Name}
					if ts.Assign.IsValid() {
						d.alias = qualified(ts.Type)
						if d.alias == "" {
							t.Errorf("%s.%s is an alias to an expression this check cannot resolve; keep aliases simple", pkgDir, ts.Name.Name)
						}
					}
					out = append(out, d)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// qualified renders an alias target as "<package>.<Name>", or "" for a target
// this check cannot resolve.
func qualified(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
	case *ast.Ident:
		// An alias to a type in the same package; the target is registered
		// under that package's own name, which the caller supplies.
		return ""
	}
	return ""
}

// typeKey renders a reflect.Type as "<last path element>.<name>", the same
// spelling go/doc reports.
func typeKey(rt reflect.Type) string {
	p := rt.PkgPath()
	if i := lastSlash(p); i >= 0 {
		p = p[i+1:]
	}
	// A generic type instantiated for the registry reports its arguments —
	// "Command[int,string]". The contract is the generic type, so the
	// instantiation is dropped.
	n := rt.Name()
	if i := indexByte(n, '['); i >= 0 {
		n = n[:i]
	}
	return p + "." + n
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// Probe types for TestReflectWalkDetectsAForeignType. Each reaches a bus type
// by a different route, so a walker that handles only struct fields is caught.

type probeField struct{ S bus.Subject }

type probeMethod struct{}

func (probeMethod) Reply() bus.Msg { return bus.Msg{} }

type probeIface interface{ Take(bus.Headers) }

type probeNested struct {
	M map[string][]bus.Fault
}

// probeClean reaches nothing outside the standard library.
type probeClean struct {
	N int
	S []string
	M map[string]int
}
