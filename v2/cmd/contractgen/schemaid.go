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
//
// The algorithm string is bumped for v2 because the canonical form CHANGED:
// the operation's address is now inside the hash. Keeping "v1" while hashing
// different atoms would be the worst of both worlds — two incompatible
// canonical forms claiming one identity, and no way for a consumer to tell
// which one it holds.
const schemaAlgorithm = "bd.schema-id.v2"

const (
	kindCommand = "command"
	kindEvent   = "event"
	kindStream  = "stream"
	kindBucket  = "bucket"
	kindService = "service"
)

// endpoint is one operation of a service. A service's identity covers every
// endpoint it mounts, so adding, removing or re-addressing one changes the
// service's hash: a caller that resolved the service yesterday must not keep a
// stale endpoint map.
type endpoint struct {
	name    string
	subject string
	req     reflect.Type
	resp    reflect.Type
}

// operation is one host-exposed contract. The generator owns this table; a call
// site never names an operation, it uses the generated descriptor.
type operation struct {
	name  string
	kind  string
	rev   uint32
	goVar string       // generated descriptor identifier
	req   reflect.Type // for an event, stream or bucket, the payload type
	resp  reflect.Type // nil except for a command

	// The ADDRESS, new in v2 and inside the hash.
	//
	// v1 hashed only the shape, which was safe while an operation's name WAS
	// its address. Once a plugin addresses the bus directly, a descriptor can
	// be re-pointed at a different subject with its payloads unchanged, and a
	// shape-only digest would report that as the same operation. The schema
	// check would then pass on a message going somewhere else entirely.
	subject   string     // command, event
	stream    string     // stream
	subjects  []string   // stream
	bucket    string     // bucket
	version   string     // service
	endpoints []endpoint // service
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
	if err := canonicalAddress(b, op); err != nil {
		return nil, err
	}
	var path []string
	switch op.kind {
	case kindService:
		for _, e := range op.endpoints {
			if err := canonicalSchema(b, ms, e.req, &path); err != nil {
				return nil, fmt.Errorf("%s endpoint %s request: %w", op.name, e.name, err)
			}
			if err := canonicalSchema(b, ms, e.resp, &path); err != nil {
				return nil, fmt.Errorf("%s endpoint %s response: %w", op.name, e.name, err)
			}
		}
	default:
		if err := canonicalSchema(b, ms, op.req, &path); err != nil {
			return nil, fmt.Errorf("%s request: %w", op.name, err)
		}
		if op.kind == kindCommand {
			if err := canonicalSchema(b, ms, op.resp, &path); err != nil {
				return nil, fmt.Errorf("%s response: %w", op.name, err)
			}
		}
	}
	return b.atoms, nil
}

// canonicalAddress writes the atoms that say WHERE the operation lives. Each
// kind writes a fixed number of atoms so no address can be read as part of the
// schema that follows it, and the count is written before any list so two
// different lists cannot concatenate to the same atom stream.
func canonicalAddress(b *ast, op operation) error {
	switch op.kind {
	case kindCommand, kindEvent:
		if op.subject == "" {
			return fmt.Errorf("%s: %s has no subject", op.name, op.kind)
		}
		b.atom("subject")
		b.atom(op.subject)
	case kindStream:
		if op.stream == "" {
			return fmt.Errorf("%s: stream has no name", op.name)
		}
		if len(op.subjects) == 0 {
			return fmt.Errorf("%s: stream %s declares no subjects", op.name, op.stream)
		}
		b.atom("stream")
		b.atom(op.stream)
		b.atom(strconv.Itoa(len(op.subjects)))
		for _, s := range op.subjects {
			b.atom(s)
		}
	case kindBucket:
		if op.bucket == "" {
			return fmt.Errorf("%s: bucket has no name", op.name)
		}
		b.atom("bucket")
		b.atom(op.bucket)
	case kindService:
		if op.version == "" {
			return fmt.Errorf("%s: service has no version", op.name)
		}
		if len(op.endpoints) == 0 {
			return fmt.Errorf("%s: service mounts no endpoints", op.name)
		}
		b.atom("service")
		b.atom(op.version)
		b.atom(strconv.Itoa(len(op.endpoints)))
		for _, e := range op.endpoints {
			if e.name == "" || e.subject == "" {
				return fmt.Errorf("%s: endpoint needs a name and a subject", op.name)
			}
			b.atom(e.name)
			b.atom(e.subject)
		}
	default:
		return fmt.Errorf("%s: unknown kind %q", op.name, op.kind)
	}
	return nil
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
