package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	goast "go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestBrowserContractsMatchCanonicalGoModel(t *testing.T) {
	want, err := os.ReadFile("../../typescript/contracts.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	got, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("browser contracts drifted; run go run ./cmd/plugin-typescript -out typescript/contracts.d.ts")
	}
}

func TestGeneratedArtifactsMatchCheckedInCopies(t *testing.T) {
	// path reads from the test's cwd; out is the same file from the repo root,
	// so the printed command is copy-pasteable where the generator is run.
	for _, tc := range []struct{ emit, pkg, path, out string }{
		{"js", "plugin", "../../typescript/validators.js", "typescript/validators.js"},
		{"go", "plugin", "../../plugin/payload_gen.go", "plugin/payload_gen.go"},
		{"go", "consumer", "consumer/payload_gen.go", "cmd/plugin-typescript/consumer/payload_gen.go"},
		{"go", "messaging", "../../messaging/payload_gen.go", "messaging/payload_gen.go"},
	} {
		t.Run(tc.emit+"/"+tc.pkg, func(t *testing.T) {
			want, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := generate(tc.emit, tc.pkg)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s drifted; run go run ./cmd/plugin-typescript -emit=%s -package=%s -out %s", tc.out, tc.emit, tc.pkg, tc.out)
			}
		})
	}
}

// The sidecar is byte-compared like the generated code, so a hash change can
// never land without the canonical AST that explains it.
func TestSchemaSidecarMatchesCheckedInCopy(t *testing.T) {
	want, err := os.ReadFile("../../typescript/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := generate("json", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("schemas.json drifted; run go run ./cmd/plugin-typescript -emit=json -out typescript/schemas.json")
	}
}

type shippedDescriptor struct{ rev, hash string }

func TestShippedDescriptorsResolveConstantValues(t *testing.T) {
	hash := strings.Repeat("a", 64)
	contract := []byte(fmt.Sprintf("package fixture\nconst Name = \"cmd.example.v1.work\"\nconst Alias = Name\nconst Rev = 1 + 1\nconst Hash = %q\n", hash))
	for _, args := range []string{`Alias, Rev, Hash`, fmt.Sprintf("\"cmd.example.v1.work\", 2, %q", hash), "(Name),\n (Rev),\n (Hash),"} {
		got, err := shippedDescriptors(contract, []byte("package fixture\nvar D = plugin.NewDescriptor("+args+")"))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got["cmd.example.v1.work"] != (shippedDescriptor{rev: "2", hash: hash}) {
			t.Fatalf("unexpected descriptor values: %#v", got)
		}
	}
	for _, args := range []string{`Missing, Rev, Hash`, `Name, "two", Hash`, `Name, Rev, "bad"`, `Name, Rev`, `Name, dynamic(), Hash`} {
		if _, err := shippedDescriptors(contract, []byte("package fixture\nvar D = plugin.NewDescriptor("+args+")")); err == nil {
			t.Fatalf("accepted unresolved or invalid arguments: %s", args)
		}
	}
	duplicate := []byte("package fixture\nvar A = plugin.NewDescriptor(Name, Rev, Hash)\nvar B = plugin.NewDescriptor(Alias, Rev, Hash)")
	if _, err := shippedDescriptors(contract, duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate check: %v", err)
	}
	if _, err := shippedDescriptors(contract, []byte("package fixture")); err == nil {
		t.Fatal("accepted empty descriptor set")
	}
	// Name, revision and hash changes must remain visible to the bijection check.
	changed := []byte(fmt.Sprintf("package fixture\nvar D = plugin.NewDescriptor(\"cmd.other.v1.work\", 3, %q)", strings.Repeat("b", 64)))
	got, err := shippedDescriptors(contract, changed)
	if err != nil || got["cmd.other.v1.work"] != (shippedDescriptor{rev: "3", hash: strings.Repeat("b", 64)}) {
		t.Fatalf("changed values were hidden: %#v %v", got, err)
	}
}

func shippedDescriptors(contract, source []byte) (map[string]shippedDescriptor, error) {
	fset := token.NewFileSet()
	decls, err := parser.ParseFile(fset, "contract.go", contract, 0)
	if err != nil {
		return nil, err
	}
	config := types.Config{Importer: importer.Default()}
	pkg, err := config.Check("descriptor-fixture", fset, []*goast.File{decls}, nil)
	if err != nil {
		return nil, fmt.Errorf("resolve contract: %w", err)
	}
	file, err := parser.ParseFile(fset, "payload_gen.go", source, 0)
	if err != nil {
		return nil, err
	}
	shipped := map[string]shippedDescriptor{}
	var failure error
	goast.Inspect(file, func(node goast.Node) bool {
		if failure != nil {
			return false
		}
		call, ok := node.(*goast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*goast.SelectorExpr)
		if !ok || selector.Sel.Name != "NewDescriptor" {
			return true
		}
		owner, ok := selector.X.(*goast.Ident)
		if !ok || owner.Name != "plugin" {
			return true
		}
		if len(call.Args) != 3 {
			failure = fmt.Errorf("descriptor needs three arguments")
			return false
		}
		values := make([]constant.Value, 3)
		for i, arg := range call.Args {
			start, end := fset.Position(arg.Pos()).Offset, fset.Position(arg.End()).Offset
			value, err := types.Eval(fset, pkg, token.NoPos, string(source[start:end]))
			if err != nil || value.Value == nil {
				failure = fmt.Errorf("descriptor argument %d is not a resolved constant: %v", i, err)
				return false
			}
			values[i] = value.Value
		}
		if values[0].Kind() != constant.String || values[1].Kind() != constant.Int || values[2].Kind() != constant.String {
			failure = fmt.Errorf("descriptor needs string, integer and string constants")
			return false
		}
		name, hash := constant.StringVal(values[0]), constant.StringVal(values[2])
		if name == "" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(hash) {
			failure = fmt.Errorf("invalid descriptor name or hash")
			return false
		}
		if _, exists := shipped[name]; exists {
			failure = fmt.Errorf("duplicate descriptor %s", name)
			return false
		}
		shipped[name] = shippedDescriptor{rev: values[1].ExactString(), hash: hash}
		return true
	})
	if failure != nil {
		return nil, failure
	}
	if len(shipped) == 0 {
		return nil, fmt.Errorf("found no descriptors in payload source")
	}
	return shipped, nil
}

// Compare descriptor values, not source spelling: both named constants and
// generated literals must match the sidecar by name, revision and hash. Checking
// only hashes would miss a changed operation name or contract revision.
func TestEveryShippedMessagingDescriptorMatchesItsSidecarEntry(t *testing.T) {
	const regen = "go run ./cmd/plugin-typescript -emit=json -out typescript/schemas.json"

	contract, err := os.ReadFile("../../messaging/contract.go")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("../../messaging/payload_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := shippedDescriptors(contract, src)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile("../../typescript/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Operations map[string]map[string]struct {
			Hash      string   `json:"hash"`
			Canonical []string `json:"canonical"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	recorded := map[string]map[string]string{} // name -> revision -> hash
	for name, revs := range doc.Operations {
		if !strings.Contains(name, ".messaging.") {
			continue
		}
		for rev, entry := range revs {
			if len(entry.Canonical) == 0 {
				t.Errorf("%s rev %s has a hash with no canonical AST beside it", name, rev)
			}
			if recorded[name] == nil {
				recorded[name] = map[string]string{}
			}
			recorded[name][rev] = entry.Hash
		}
	}

	for name, got := range shipped {
		revs, ok := recorded[name]
		if !ok {
			t.Errorf("%s ships in messaging/payload_gen.go with no sidecar entry.\n"+
				"Register the operation in the messaging target, then: %s", name, regen)
			continue
		}
		want, ok := revs[got.rev]
		if !ok {
			t.Errorf("%s ships revision %s; the sidecar records revision(s) %s.\n"+
				"Align the messaging target's revision, then: %s",
				name, got.rev, strings.Join(slices.Sorted(maps.Keys(revs)), ", "), regen)
			continue
		}
		if got.hash != want {
			t.Errorf("%s rev %s ships hash %s; the sidecar records %s.\n"+
				"Regenerate: %s", name, got.rev, got.hash, want, regen)
		}
	}
	for name := range recorded {
		if _, ok := shipped[name]; !ok {
			t.Errorf("the sidecar carries %s but no messaging descriptor ships it.\n"+
				"Drop it from the messaging target, then: %s", name, regen)
		}
	}
}

// The .d.ts and the validators are one module surface: repo B ships them as
// contracts.d.ts beside contracts.js. A guard that exists at runtime with no
// declaration is a TS2305 on an import that works perfectly, which is a
// genuinely confusing failure for a plugin author. Declarations are not part of
// any schema, so this asserts surface parity only — no hash moves with it.
func TestDeclarationsCoverEveryEmittedValidator(t *testing.T) {
	names := func(pattern string, src []byte) map[string]bool {
		out := map[string]bool{}
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(string(src), -1) {
			out[m[1]] = true
		}
		return out
	}
	dts, err := generate("dts", "plugin")
	if err != nil {
		t.Fatal(err)
	}
	js, err := generate("js", "plugin")
	if err != nil {
		t.Fatal(err)
	}
	declared := names(`(?m)^export declare function (is\w+)\(`, dts)
	exported := names(`(?m)^export function (is\w+)\(`, js)
	if len(exported) == 0 {
		t.Fatal("found no validators in the js emission; this guard is not looking where it thinks it is")
	}
	for name := range exported {
		if !declared[name] {
			t.Errorf("validators.js exports %s with no declaration in contracts.d.ts (TS2305 for consumers)", name)
		}
	}
	for name := range declared {
		if !exported[name] {
			t.Errorf("contracts.d.ts declares %s but no validator ships it", name)
		}
	}
}
