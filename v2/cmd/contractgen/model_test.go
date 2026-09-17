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
	return hashAt(t, req, resp, rev, "cmd.test.v1.do")
}

// hashAt is hashOf with the ADDRESS made explicit, because in v2 the address
// is part of the identity and a test that could not vary it could not prove so.
func hashAt(t *testing.T, req, resp reflect.Type, rev uint32, subject string) string {
	t.Helper()
	ms, err := buildModels([]root{{typ: req}, {typ: resp}})
	if err != nil {
		t.Fatal(err)
	}
	atoms, err := canonicalOperation(ms, operation{
		name: "cmd.test.v1.do", kind: kindCommand, rev: rev,
		req: req, resp: resp, subject: subject,
	})
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

// The deliberate, non-obvious properties of schema-id v2.
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
	_, err := canonicalOperation(ms, operation{name: "cmd.test.v1.do", kind: kindEvent, rev: 1, req: self, subject: "cmd.test.v1.do"})
	if err == nil || !strings.Contains(err.Error(), "cyclic payload graph") {
		t.Fatalf("canonicalOperation error = %v, want a cycle diagnostic", err)
	}
}

// TestSchemaIDCoversTheAddress is the v2 addition, and it is the one that
// earns the algorithm bump.
//
// v1 hashed only the shape. That was safe while an operation's name WAS its
// address: there was nowhere else for it to go. Once a plugin addresses the bus
// directly, a descriptor can be re-pointed at a different subject with its
// payloads untouched, and a shape-only digest reports that as the same
// operation — so the receiving schema check passes on a message going somewhere
// else entirely. The address is therefore inside the hash.
func TestSchemaIDCoversTheAddress(t *testing.T) {
	one := reflect.TypeOf(shapeOne{})
	leaf := reflect.TypeOf(okLeaf{})

	here := hashAt(t, one, leaf, 1, "cmd.test.v1.do")
	elsewhere := hashAt(t, one, leaf, 1, "cmd.other.v1.do")
	if here == elsewhere {
		t.Fatal("identical payloads on different subjects hashed the same; a descriptor could be re-pointed without the schema check noticing")
	}
	if again := hashAt(t, one, leaf, 1, "cmd.test.v1.do"); again != here {
		t.Fatal("the same operation hashed differently twice")
	}
}

// TestSchemaIDAddressesEveryKind checks the three kinds v2 adds, and that each
// one's address participates. A kind whose address were dropped would hash
// every instance of it identically, which is the failure that looks most like
// success.
func TestSchemaIDAddressesEveryKind(t *testing.T) {
	one := reflect.TypeOf(shapeOne{})
	leaf := reflect.TypeOf(okLeaf{})
	ms, err := buildModels([]root{{typ: one}, {typ: leaf}})
	if err != nil {
		t.Fatal(err)
	}
	hash := func(op operation) string {
		t.Helper()
		atoms, err := canonicalOperation(ms, op)
		if err != nil {
			t.Fatalf("%s: %v", op.kind, err)
		}
		return schemaHash(atoms)
	}

	for _, tc := range []struct {
		name string
		a, b operation
	}{
		{"stream name",
			operation{name: "s", kind: kindStream, rev: 1, req: one, stream: "A", subjects: []string{"event.x.v1.y"}},
			operation{name: "s", kind: kindStream, rev: 1, req: one, stream: "B", subjects: []string{"event.x.v1.y"}}},
		{"stream subjects",
			operation{name: "s", kind: kindStream, rev: 1, req: one, stream: "A", subjects: []string{"event.x.v1.y"}},
			operation{name: "s", kind: kindStream, rev: 1, req: one, stream: "A", subjects: []string{"event.x.v1.z"}}},
		{"bucket name",
			operation{name: "b", kind: kindBucket, rev: 1, req: one, bucket: "one"},
			operation{name: "b", kind: kindBucket, rev: 1, req: one, bucket: "two"}},
		{"service version",
			operation{name: "v", kind: kindService, rev: 1, version: "1.0.0", endpoints: []endpoint{{name: "e", subject: "svc.x.v1.e", req: one, resp: leaf}}},
			operation{name: "v", kind: kindService, rev: 1, version: "2.0.0", endpoints: []endpoint{{name: "e", subject: "svc.x.v1.e", req: one, resp: leaf}}}},
		{"service endpoint subject",
			operation{name: "v", kind: kindService, rev: 1, version: "1.0.0", endpoints: []endpoint{{name: "e", subject: "svc.x.v1.e", req: one, resp: leaf}}},
			operation{name: "v", kind: kindService, rev: 1, version: "1.0.0", endpoints: []endpoint{{name: "e", subject: "svc.x.v1.moved", req: one, resp: leaf}}}},
		{"service endpoint set",
			operation{name: "v", kind: kindService, rev: 1, version: "1.0.0", endpoints: []endpoint{{name: "e", subject: "svc.x.v1.e", req: one, resp: leaf}}},
			operation{name: "v", kind: kindService, rev: 1, version: "1.0.0", endpoints: []endpoint{
				{name: "e", subject: "svc.x.v1.e", req: one, resp: leaf},
				{name: "f", subject: "svc.x.v1.f", req: one, resp: leaf}}}},
	} {
		if hash(tc.a) == hash(tc.b) {
			t.Errorf("%s does not affect the digest", tc.name)
		}
	}
}

// TestCanonicalAddressRefusesAnIncompleteOperation: the generator must fail
// loudly rather than hash a placeholder. An operation with no address would
// otherwise get a stable, meaningless digest that every other incomplete
// operation of the same shape would share.
func TestSchemaIDRefusesAnIncompleteAddress(t *testing.T) {
	one := reflect.TypeOf(shapeOne{})
	leaf := reflect.TypeOf(okLeaf{})
	ms, err := buildModels([]root{{typ: one}, {typ: leaf}})
	if err != nil {
		t.Fatal(err)
	}
	for name, op := range map[string]operation{
		"command with no subject":   {name: "c", kind: kindCommand, rev: 1, req: one, resp: leaf},
		"event with no subject":     {name: "e", kind: kindEvent, rev: 1, req: one},
		"stream with no name":       {name: "s", kind: kindStream, rev: 1, req: one, subjects: []string{"event.x.v1.y"}},
		"stream with no subjects":   {name: "s", kind: kindStream, rev: 1, req: one, stream: "A"},
		"bucket with no name":       {name: "b", kind: kindBucket, rev: 1, req: one},
		"service with no version":   {name: "v", kind: kindService, rev: 1, endpoints: []endpoint{{name: "e", subject: "svc.x.v1.e", req: one, resp: leaf}}},
		"service with no endpoints": {name: "v", kind: kindService, rev: 1, version: "1.0.0"},
		"unknown kind":              {name: "u", kind: "telepathy", rev: 1, req: one},
	} {
		if _, err := canonicalOperation(ms, op); err == nil {
			t.Errorf("%s was hashed instead of refused", name)
		}
	}
}
