package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// schema-id v1 (ADR 0025 §9), frozen as generator output. Never runtime
// reflection, never hand-computed.
//
// The digest is SHA-256, lowercase hex, over a length-delimited canonical AST.
// Each atom is written as a netstring — <byte length>:<bytes>; — so no atom can
// be confused with a concatenation of its neighbours. The digest is taken over
// the atoms concatenated; the sidecar records them one per line, because a bare
// hash mismatch is undiagnosable and a single 5 KB line is not much better.
//
// Three properties are deliberate:
//
//   - Type and package names are absent, so a Go rename does not churn wire
//     identity.
//   - Classification is inside the hash, so moving a field public -> subject
//     forces renegotiation even though the shape is unchanged.
//   - The descriptor revision is inside the hash, which buys a deliberate
//     identity change for a semantic change with no shape change.
const schemaAlgorithm = "bd.schema-id.v1"

const (
	kindCommand = "command"
	kindEvent   = "event"
)

// operation is one host-exposed contract. The generator owns this table; a call
// site never names an operation, it uses the generated descriptor.
type operation struct {
	name  string
	kind  string
	rev   uint32
	goVar string       // generated descriptor identifier
	req   reflect.Type // for an event, the payload type
	resp  reflect.Type // nil for an event
}

// ast collects the canonical atoms in order.
type ast struct{ atoms []string }

func (a *ast) atom(s string) { a.atoms = append(a.atoms, strconv.Itoa(len(s))+":"+s+";") }

// canonicalOperation renders the AST the digest is taken over.
func canonicalOperation(ms *modelSet, op operation) ([]string, error) {
	b := &ast{}
	b.atom(schemaAlgorithm)
	b.atom(op.kind)
	b.atom(op.name)
	b.atom(strconv.FormatUint(uint64(op.rev), 10))
	var path []string
	if err := canonicalSchema(b, ms, op.req, &path); err != nil {
		return nil, fmt.Errorf("%s request: %w", op.name, err)
	}
	if op.kind == kindCommand {
		if err := canonicalSchema(b, ms, op.resp, &path); err != nil {
			return nil, fmt.Errorf("%s response: %w", op.name, err)
		}
	}
	return b.atoms, nil
}

func canonicalSchema(b *ast, ms *modelSet, t reflect.Type, path *[]string) error {
	if t == nil {
		return fmt.Errorf("missing schema type")
	}
	name := deref(t).Name()
	m := ms.byName[name]
	if m == nil {
		return fmt.Errorf("%s is not an exposed payload type", t)
	}
	for _, seen := range *path {
		if seen == name {
			return fmt.Errorf("cyclic payload graph: %s -> %s", strings.Join(*path, " -> "), name)
		}
	}
	*path = append(*path, name)
	defer func() { *path = (*path)[:len(*path)-1] }()

	fields := byJSONName(m.fields)
	b.atom("struct")
	b.atom(strconv.Itoa(len(fields)))
	for _, f := range fields {
		b.atom(f.jsonName)
		if f.optional {
			b.atom("optional")
		} else {
			b.atom("required")
		}
		if f.nullable() {
			b.atom("nullable")
		} else {
			b.atom("nonnull")
		}
		if f.list() {
			b.atom("list")
		} else {
			b.atom("single")
		}
		b.atom(f.wire)
		b.atom(f.class)
		if f.elem == nil {
			b.atom("-")
			continue
		}
		if err := canonicalSchema(b, ms, f.elem, path); err != nil {
			return fmt.Errorf("%s.%s: %w", name, f.goName, err)
		}
	}
	return nil
}

func schemaHash(canonical []string) string {
	sum := sha256.Sum256([]byte(strings.Join(canonical, "")))
	return hex.EncodeToString(sum[:])
}

type schemaEntry struct {
	Kind      string   `json:"kind"`
	Algorithm string   `json:"algorithm"`
	Hash      string   `json:"hash"`
	Canonical []string `json:"canonical"`
}

type schemaSidecar struct {
	Algorithm string `json:"algorithm"`
	// Operations is keyed by operation name, then by decimal revision.
	Operations map[string]map[string]schemaEntry `json:"operations"`
}

// hashes returns each operation's digest, keyed by descriptor variable.
func hashes(ms *modelSet, ops []operation) (map[string]string, error) {
	out := make(map[string]string, len(ops))
	for _, op := range ops {
		canonical, err := canonicalOperation(ms, op)
		if err != nil {
			return nil, err
		}
		out[op.goVar] = schemaHash(canonical)
	}
	return out, nil
}

// collectSchemas adds one target's operations to the sidecar document. It
// exists because a bare hash mismatch is undiagnosable: with the canonical AST
// recorded next to the digest, a mismatch is a diff. The sidecar is a build
// artifact — descriptors embed only the hash constant at runtime.
func collectSchemas(into map[string]map[string]schemaEntry, ms *modelSet, ops []operation) error {
	for _, op := range ops {
		canonical, err := canonicalOperation(ms, op)
		if err != nil {
			return err
		}
		rev := strconv.FormatUint(uint64(op.rev), 10)
		if into[op.name] == nil {
			into[op.name] = map[string]schemaEntry{}
		}
		if _, dup := into[op.name][rev]; dup {
			return fmt.Errorf("duplicate operation %s revision %s", op.name, rev)
		}
		into[op.name][rev] = schemaEntry{
			Kind:      op.kind,
			Algorithm: schemaAlgorithm,
			Hash:      schemaHash(canonical),
			Canonical: canonical,
		}
	}
	return nil
}

func renderSidecar(entries map[string]map[string]schemaEntry) ([]byte, error) {
	raw, err := json.MarshalIndent(schemaSidecar{Algorithm: schemaAlgorithm, Operations: entries}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
