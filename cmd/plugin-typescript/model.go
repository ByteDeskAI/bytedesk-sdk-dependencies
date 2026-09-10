package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Classification labels (ADR 0025 §6). Absent is not a default: a JSON-visible
// field with no label is a generator error, so the most restrictive outcome —
// refusing to build — is what forgetting one produces.
const (
	classPublic  = "public"
	classSubject = "subject"
	classSecret  = "secret"
)

// The closed set of wire scalar kinds. This is the stated rule rather than an
// accident of the emitter's type switch: anything outside it is an error naming
// Type.Field, not a panic and not a silent omission.
const (
	wireString = "string"
	wireBool   = "bool"
	wireInt32  = "int32" // also plain `int` — see the int rule on wireKind
	wireUint32 = "uint32"
	wireInt64  = "int64"
	wireUint64 = "uint64"
	wireObject = "object"
)

var timeType = reflect.TypeOf(time.Time{})

// field is one JSON-visible field of an exposed type.
type field struct {
	goName   string
	jsonName string
	optional bool         // omitempty or omitzero
	asString bool         // ,string — the 64-bit wire discipline of §9
	class    string       // public | subject | secret
	typ      reflect.Type // as declared, before pointer/slice unwrapping
	elem     reflect.Type // struct reached through pointer/slice, else nil
	wire     string
}

func (f field) list() bool     { return f.typ.Kind() == reflect.Slice }
func (f field) nullable() bool { return !f.optional && (f.list() || f.typ.Kind() == reflect.Pointer) }

// model is one exposed struct type, in declaration order.
type model struct {
	name     string
	typ      reflect.Type
	fields   []field
	readonly bool // reached from a seed root flagged readonly
}

type modelSet struct {
	byName map[string]*model
	names  []string // sorted
}

// root is a seed of the exposure graph. readonly marks the roots whose whole
// reachable subgraph the browser treats as immutable — replacing the
// name-prefix guess the emitter used to make.
type root struct {
	typ      reflect.Type
	readonly bool
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t
}

// wireKind maps a Go field type to its wire kind, or explains why it has none.
//
// THE INT RULE, stated here because the generator cannot fully enforce it:
//
//   - `int` is reserved for SMALL BOUNDED values — an ordering, a priority, a
//     day count. It is admitted as int32 and crosses JSON as a number.
//   - Any numeric identifier, sequence number, cursor, offset or timestamp must
//     be `int64`/`uint64` with `,string`, or an opaque string. Above 2^53 a JSON
//     number silently loses precision through a browser client, and no amount of
//     TypeScript catches it (ADR 0025 §9).
//
// `int` is 64 bits on every platform this ships to, so a cursor typed `int`
// evades the check below. That gap is deliberate: erroring on reflect.Int would
// churn NavItem.Order, Provider.Priority and Pricing.TrialDays — all provably
// bounded — for a zero-byte .d.ts difference. Review every new numeric field
// against the rule above; the generator only catches the explicitly 64-bit
// widths.
func wireKind(t reflect.Type, asString bool) (string, error) {
	base, err := baseWireKind(t, asString)
	if err != nil {
		return "", err
	}
	if asString && base != wireObject && !strings.HasSuffix(base, ".string") {
		base += ".string"
	}
	return base, nil
}

func baseWireKind(t reflect.Type, asString bool) (string, error) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		return baseWireKind(t.Elem(), asString)
	case reflect.Struct:
		if t == timeType {
			return "", fmt.Errorf("time.Time is not a wire kind; carry timestamps as RFC3339 strings (ADR 0025 §9)")
		}
		return wireObject, nil
	case reflect.String:
		return wireString, nil
	case reflect.Bool:
		return wireBool, nil
	case reflect.Int, reflect.Int32:
		return wireInt32, nil
	case reflect.Uint32:
		return wireUint32, nil
	case reflect.Int64:
		if !asString {
			return "", fmt.Errorf("int64 needs `,string`: 64-bit integers cross JSON as decimal strings, or carry the value as an opaque string (ADR 0025 §9)")
		}
		return wireInt64 + ".string", nil
	case reflect.Uint64:
		if !asString {
			return "", fmt.Errorf("uint64 needs `,string`: 64-bit integers cross JSON as decimal strings, or carry the value as an opaque string (ADR 0025 §9)")
		}
		return wireUint64 + ".string", nil
	default:
		return "", fmt.Errorf("unsupported wire kind %s", t.String())
	}
}

// buildModels walks the exposure graph from roots, classifying every
// JSON-visible field. It reports every problem it finds rather than the first,
// so one build names the whole set of edits.
func buildModels(roots []root) (*modelSet, error) {
	ms := &modelSet{byName: map[string]*model{}}
	var problems []string
	state := map[reflect.Type]int{} // 0 unvisited, 1 on the stack, 2 done
	var path []string               // Type.Field edges currently on the stack

	var visit func(reflect.Type)
	visit = func(t reflect.Type) {
		if t.Kind() != reflect.Struct {
			return
		}
		switch state[t] {
		case 2:
			return
		case 1:
			problems = append(problems, fmt.Sprintf("cyclic payload graph: %s -> %s (a recursive payload is unbounded against the 64 KiB envelope cap)",
				strings.Join(path, " -> "), t.Name()))
			return
		}
		if prior, found := ms.byName[t.Name()]; found && prior.typ != t {
			problems = append(problems, fmt.Sprintf("%s: two different types share this name (%s and %s)", t.Name(), prior.typ, t))
			return
		}
		state[t] = 1
		m := &model{name: t.Name(), typ: t}
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			parts := strings.Split(f.Tag.Get("json"), ",")
			if parts[0] == "-" {
				continue
			}
			fl := field{goName: f.Name, jsonName: parts[0], typ: f.Type}
			if fl.jsonName == "" {
				fl.jsonName = f.Name
			}
			for _, opt := range parts[1:] {
				switch opt {
				case "omitempty", "omitzero":
					fl.optional = true
				case "string":
					fl.asString = true
				}
			}
			switch fl.class = strings.TrimSpace(f.Tag.Get("bd")); fl.class {
			case classPublic, classSubject, classSecret:
			case "":
				problems = append(problems, fmt.Sprintf(`%s.%s: JSON-visible field has no bd classification (bd:"public"|"subject"|"secret")`, t.Name(), f.Name))
			default:
				problems = append(problems, fmt.Sprintf("%s.%s: unknown bd classification %q (public|subject|secret)", t.Name(), f.Name, fl.class))
			}
			kind, err := wireKind(f.Type, fl.asString)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s.%s: %v", t.Name(), f.Name, err))
			} else {
				fl.wire = kind
				if el := deref(f.Type); el.Kind() == reflect.Struct {
					fl.elem = el
					path = append(path, t.Name()+"."+f.Name)
					visit(el)
					path = path[:len(path)-1]
				}
			}
			m.fields = append(m.fields, fl)
		}
		state[t] = 2
		ms.byName[m.name] = m
		ms.names = append(ms.names, m.name)
	}

	for _, r := range roots {
		visit(deref(r.typ))
	}
	sort.Strings(ms.names)
	ms.markReadonly(roots)

	// A secret reachable from an exposed root is an error, not a silent
	// omission: the type is on the wire either way, so the build stops.
	memo := map[string][]string{}
	for _, r := range roots {
		if trail := ms.reachesSecret(deref(r.typ).Name(), memo); len(trail) > 0 {
			problems = append(problems, fmt.Sprintf("secret reachable from exposed type %s: %s",
				deref(r.typ).Name(), strings.Join(trail, " -> ")))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("payload contract errors:\n  %s", strings.Join(problems, "\n  "))
	}
	return ms, nil
}

// markReadonly is a second pass rather than a flag carried through the first,
// because a type reachable from both a readonly and a plain root must come out
// readonly regardless of which edge the walk took first.
func (ms *modelSet) markReadonly(roots []root) {
	seen := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		m := ms.byName[deref(t).Name()]
		if m == nil || seen[m.name] {
			return
		}
		seen[m.name] = true
		m.readonly = true
		for _, f := range m.fields {
			if f.elem != nil {
				walk(f.elem)
			}
		}
	}
	for _, r := range roots {
		if r.readonly {
			walk(r.typ)
		}
	}
}

// reachesSecret is the memoized DFS of §6: transitive through nested structs,
// pointers and slices, collapsing to a per-type property. It returns the field
// trail to the first secret so the diagnostic can name it.
func (ms *modelSet) reachesSecret(name string, memo map[string][]string) []string {
	if trail, done := memo[name]; done {
		return trail
	}
	memo[name] = nil // cycles are already rejected; this also stops re-entry
	m := ms.byName[name]
	if m == nil {
		return nil
	}
	for _, f := range m.fields {
		if f.class == classSecret {
			memo[name] = []string{name + "." + f.goName}
			return memo[name]
		}
	}
	for _, f := range m.fields {
		if f.elem == nil {
			continue
		}
		if trail := ms.reachesSecret(f.elem.Name(), memo); len(trail) > 0 {
			memo[name] = append([]string{name + "." + f.goName}, trail...)
			return memo[name]
		}
	}
	return nil
}

// union is the generated Payload membership: every exposed type that cannot
// reach a secret. Removing a name from it is a compile break for consumers.
func (ms *modelSet) union() []string {
	memo := map[string][]string{}
	out := make([]string, 0, len(ms.names))
	for _, name := range ms.names {
		if len(ms.reachesSecret(name, memo)) == 0 {
			out = append(out, name)
		}
	}
	return out
}

// byJSONName returns the fields sorted for the canonical AST. Declaration order
// stays authoritative for the emitted declarations; the hash must not move when
// someone reorders a struct.
func byJSONName(fields []field) []field {
	out := append([]field(nil), fields...)
	sort.Slice(out, func(i, j int) bool { return out[i].jsonName < out[j].jsonName })
	return out
}
