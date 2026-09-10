package main

import (
	"reflect"
	"strings"
	"testing"
)

// Fixtures for the generator's refusals. They live here rather than in the
// plugin package because the point is what the generator does with a bad type,
// and a bad type must never be shipped.

type okLeaf struct {
	Name    string `json:"name" bd:"public"`
	skipped string //lint:ignore U1000 unexported fields need no classification
	Dropped string `json:"-"`
}

type okRoot struct {
	Leaf   okLeaf   `json:"leaf" bd:"public"`
	Leaves []okLeaf `json:"leaves,omitempty" bd:"subject"`
	Cursor int64    `json:"cursor,string" bd:"public"`
}

type untagged struct {
	Name string `json:"name"`
}

type badClass struct {
	Name string `json:"name" bd:"internal"`
}

type secretLeaf struct {
	APIKey string `json:"apiKey" bd:"secret"`
}

type secretMid struct {
	Leaf *secretLeaf `json:"leaf" bd:"public"`
}

type secretRoot struct {
	Mids []secretMid `json:"mids" bd:"public"`
}

type badKind struct {
	Attrs map[string]string `json:"attrs" bd:"public"`
}

type wideInt struct {
	Cursor int64 `json:"cursor" bd:"public"`
}

type cycleA struct {
	B *cycleB `json:"b" bd:"public"`
}

type cycleB struct {
	A *cycleA `json:"a" bd:"public"`
}

func buildFailure(t *testing.T, v any) string {
	t.Helper()
	if _, err := buildModels([]root{{typ: reflect.TypeOf(v)}}); err != nil {
		return err.Error()
	}
	t.Fatalf("%T: expected a generator error, got none", v)
	return ""
}

// The generator errors rather than silently omitting, and every diagnostic
// names Type.Field so the edit is obvious from the build output alone.
func TestGeneratorRefusesUnclassifiableContracts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{"unclassified field", untagged{}, []string{"untagged.Name", "no bd classification"}},
		{"unknown classification", badClass{}, []string{"badClass.Name", `unknown bd classification "internal"`}},
		{"secret reachable transitively", secretRoot{}, []string{"secretRoot.Mids", "secretMid.Leaf", "secretLeaf.APIKey"}},
		{"unsupported kind", badKind{}, []string{"badKind.Attrs", "unsupported wire kind"}},
		{"64-bit integer without ,string", wideInt{}, []string{"wideInt.Cursor", "int64 needs `,string`", "opaque string"}},
		{"cyclic payload graph", cycleA{}, []string{"cyclic payload graph", "cycleA.B", "cycleB.A", "cycleA"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildFailure(t, tc.value)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("diagnostic missing %q:\n%s", want, got)
				}
			}
		})
	}
}

// Unexported and json:"-" fields need no tag: they cannot cross the wire.
func TestGeneratorAcceptsClassifiedContract(t *testing.T) {
	ms, err := buildModels([]root{{typ: reflect.TypeOf(okRoot{})}})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ms.byName["okLeaf"].fields); got != 1 {
		t.Fatalf("okLeaf JSON-visible fields = %d, want 1", got)
	}
	if union := ms.union(); len(union) != 2 || union[0] != "okLeaf" || union[1] != "okRoot" {
		t.Fatalf("union = %v, want [okLeaf okRoot]", union)
	}
}

// A type that can reach a secret is not in the union, so no command can carry
// it. Asserted on the set directly: buildModels refuses any root that reaches a
// secret, so a shipped contract can never demonstrate the exclusion.
func TestSecretReachabilityRemovesTypesFromTheUnion(t *testing.T) {
	str := reflect.TypeOf("")
	leaf, mid, clean := reflect.TypeOf(secretLeaf{}), reflect.TypeOf(secretMid{}), reflect.TypeOf(okLeaf{})
	ms := &modelSet{
		names: []string{"okLeaf", "secretLeaf", "secretMid"},
		byName: map[string]*model{
			"okLeaf":     {name: "okLeaf", typ: clean, fields: []field{{goName: "Name", jsonName: "name", class: classPublic, typ: str, wire: wireString}}},
			"secretLeaf": {name: "secretLeaf", typ: leaf, fields: []field{{goName: "APIKey", jsonName: "apiKey", class: classSecret, typ: str, wire: wireString}}},
			"secretMid":  {name: "secretMid", typ: mid, fields: []field{{goName: "Leaf", jsonName: "leaf", class: classPublic, typ: reflect.PointerTo(leaf), elem: leaf, wire: wireObject}}},
		},
	}
	if got := ms.union(); len(got) != 1 || got[0] != "okLeaf" {
		t.Fatalf("union = %v, want [okLeaf]: a type that can reach a secret must not be in it", got)
	}
}

func hashOf(t *testing.T, req, resp reflect.Type, rev uint32) string {
	t.Helper()
	ms, err := buildModels([]root{{typ: req}, {typ: resp}})
	if err != nil {
		t.Fatal(err)
	}
	atoms, err := canonicalOperation(ms, operation{name: "cmd.test.v1.do", kind: kindCommand, rev: rev, req: req, resp: resp})
	if err != nil {
		t.Fatal(err)
	}
	return schemaHash(atoms)
}

type shapeOne struct {
	Alpha string `json:"alpha" bd:"public"`
	Beta  bool   `json:"beta,omitempty" bd:"public"`
}

// Same JSON shape, different Go type name, field names and declaration order.
type shapeTwo struct {
	Second bool   `json:"beta,omitempty" bd:"public"`
	First  string `json:"alpha" bd:"public"`
}

type shapeReclassified struct {
	Alpha string `json:"alpha" bd:"subject"`
	Beta  bool   `json:"beta,omitempty" bd:"public"`
}

// The three deliberate, non-obvious properties of schema-id v1.
func TestSchemaIDIdentityProperties(t *testing.T) {
	one := reflect.TypeOf(shapeOne{})
	leaf := reflect.TypeOf(okLeaf{})
	base := hashOf(t, one, leaf, 1)

	if renamed := hashOf(t, reflect.TypeOf(shapeTwo{}), leaf, 1); renamed != base {
		t.Error("a Go rename or field reorder churned wire identity; type and package names must not affect the hash")
	}
	if reclassified := hashOf(t, reflect.TypeOf(shapeReclassified{}), leaf, 1); reclassified == base {
		t.Error("public -> subject changed access semantics without changing the hash")
	}
	if bumped := hashOf(t, one, leaf, 2); bumped == base {
		t.Error("the descriptor revision must be inside the hash")
	}
	if len(base) != 64 || strings.ToLower(base) != base {
		t.Errorf("hash %q is not lowercase hex SHA-256", base)
	}
}

// Cycles are an error in the hasher too, not only in the classification walk.
func TestSchemaIDRefusesCycles(t *testing.T) {
	ms := &modelSet{byName: map[string]*model{}}
	self := reflect.TypeOf(cycleA{})
	ms.byName["cycleA"] = &model{name: "cycleA", typ: self, fields: []field{{
		goName: "B", jsonName: "b", class: classPublic, typ: self, elem: self, wire: wireObject,
	}}}
	_, err := canonicalOperation(ms, operation{name: "cmd.test.v1.do", kind: kindEvent, rev: 1, req: self})
	if err == nil || !strings.Contains(err.Error(), "cyclic payload graph") {
		t.Fatalf("canonicalOperation error = %v, want a cycle diagnostic", err)
	}
}
