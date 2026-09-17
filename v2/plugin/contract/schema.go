package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// The BDP1xxx band: plugin.json against plugin.schema.json.
//
// The schema is open, so the validator itself never refuses an unknown
// property; unknownProperties walks the document beside it and warns, which is
// the behaviour the plan asks of the CLI without making a newer manifest fail
// on an older host.

const schemaURL = "https://bytedesk.ai/schemas/plugin/v2/plugin.schema.json"

// schemaBytes is the canonical schema, read from the same embed Assets serves.
func schemaBytes() []byte {
	b, err := assets.ReadFile("v2/plugin.schema.json")
	if err != nil {
		panic("contract: embedded plugin.schema.json: " + err.Error())
	}
	return b
}

var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes()))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, err
	}
	return c.Compile(schemaURL)
})

// schemaDocument is the raw schema as a map, for the walks that read what the
// validator does not evaluate: the property lists behind the unknown-property
// warning and the x-bd-ref annotations.
var schemaDocument = sync.OnceValue(func() map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(schemaBytes(), &doc); err != nil {
		panic("contract: embedded plugin.schema.json: " + err.Error())
	}
	return doc
})

// decodeDocument parses raw as generic JSON, keeping numbers as json.Number so
// the validator judges "integer" by value rather than by float64 accident.
func decodeDocument(raw []byte) (any, error) {
	return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
}

func schemaDiagnostics(r *Report, doc any) {
	sch, err := compiledSchema()
	if err != nil {
		panic("contract: plugin.schema.json does not compile: " + err.Error())
	}
	if err := sch.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			flattenSchemaError(r, ve)
		} else {
			r.add("BDP1005", "", err.Error())
		}
	}
	unknownProperties(r, schemaDocument(), doc, "")
}

// flattenSchemaError turns the validator's tree into one diagnostic per leaf,
// except that a oneOf/anyOf mismatch is reported once at its own location:
// its causes are every alternative failing in its own way, which is not
// information about the document.
func flattenSchemaError(r *Report, ve *jsonschema.ValidationError) {
	path := instancePath(ve.InstanceLocation)
	switch k := ve.ErrorKind.(type) {
	case *kind.OneOf, *kind.AnyOf:
		r.add("BDP1005", path, "value matches none of the alternatives")
		return
	case *kind.Required:
		for _, name := range k.Missing {
			r.add("BDP1003", joinPath(path, name), name)
		}
		return
	case *kind.Type:
		r.add("BDP1004", path, k.Got, strings.Join(k.Want, " or "))
		return
	}
	if len(ve.Causes) > 0 {
		for _, cause := range ve.Causes {
			flattenSchemaError(r, cause)
		}
		return
	}
	leaf := &jsonschema.ValidationError{ErrorKind: ve.ErrorKind}
	r.add("BDP1005", path, strings.TrimPrefix(leaf.Error(), "jsonschema validation failed\n  - at '': "))
}

// unknownProperties warns on every property the schema does not name, at any
// depth the schema describes. Where the schema stops describing (an object
// with no properties list) the walk stops with it.
func unknownProperties(r *Report, schema map[string]any, doc any, path string) {
	schema = resolveSchema(schema, doc)
	switch v := doc.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		if props == nil {
			return
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			sub, ok := props[key].(map[string]any)
			if !ok {
				r.add("BDP1006", joinPath(path, key), key)
				continue
			}
			unknownProperties(r, sub, v[key], joinPath(path, key))
		}
	case []any:
		items, _ := schema["items"].(map[string]any)
		if items == nil {
			return
		}
		for i, el := range v {
			unknownProperties(r, items, el, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

// resolveSchema follows a local $ref and picks the oneOf alternative whose
// declared type matches the document value, so the walk reads the properties
// of the branch that applies.
func resolveSchema(schema map[string]any, doc any) map[string]any {
	if ref, ok := schema["$ref"].(string); ok {
		if target := resolveRef(ref); target != nil {
			return target
		}
	}
	alts, _ := schema["oneOf"].([]any)
	for _, alt := range alts {
		sub, ok := alt.(map[string]any)
		if !ok {
			continue
		}
		sub = resolveSchema(sub, doc)
		if typ, _ := sub["type"].(string); typ == jsonType(doc) {
			return sub
		}
	}
	return schema
}

// resolveRef resolves "#/$defs/Name" against the embedded schema.
func resolveRef(ref string) map[string]any {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	defs, _ := schemaDocument()["$defs"].(map[string]any)
	target, _ := defs[strings.TrimPrefix(ref, prefix)].(map[string]any)
	return target
}

func jsonType(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "number"
	}
}

// schemaRefs lists every x-bd-ref annotation as (dotted path -> ref kind).
// A path through an array is written with "[]", so "nav[].href" and "binary"
// are both addressable by lookupPath.
func schemaRefs() map[string]string {
	out := map[string]string{}
	var walk func(schema map[string]any, path string, seen map[string]bool)
	walk = func(schema map[string]any, path string, seen map[string]bool) {
		if ref, ok := schema["$ref"].(string); ok {
			if seen[ref] {
				return
			}
			seen[ref] = true
			if target := resolveRef(ref); target != nil {
				schema = target
			}
		}
		if kind, ok := schema["x-bd-ref"].(string); ok {
			out[path] = kind
		}
		if props, ok := schema["properties"].(map[string]any); ok {
			for name, sub := range props {
				if s, ok := sub.(map[string]any); ok {
					walk(s, joinPath(path, name), seen)
				}
			}
		}
		if items, ok := schema["items"].(map[string]any); ok {
			walk(items, path+"[]", seen)
		}
		if alts, ok := schema["oneOf"].([]any); ok {
			for _, alt := range alts {
				if s, ok := alt.(map[string]any); ok {
					walk(s, path, seen)
				}
			}
		}
	}
	walk(schemaDocument(), "", map[string]bool{})
	return out
}

// lookupPath returns every string value at a schemaRefs path in doc, with the
// concrete path ("nav[2].href") beside each.
func lookupPath(doc any, path string) map[string]string {
	out := map[string]string{}
	var walk func(v any, rest []string, concrete string)
	walk = func(v any, rest []string, concrete string) {
		if len(rest) == 0 {
			if s, ok := v.(string); ok {
				out[concrete] = s
			}
			return
		}
		head, tail := rest[0], rest[1:]
		if strings.HasSuffix(head, "[]") {
			name := strings.TrimSuffix(head, "[]")
			obj, _ := v.(map[string]any)
			list, _ := obj[name].([]any)
			for i, el := range list {
				walk(el, tail, fmt.Sprintf("%s[%d]", joinPath(concrete, name), i))
			}
			return
		}
		obj, _ := v.(map[string]any)
		walk(obj[head], tail, joinPath(concrete, head))
	}
	walk(doc, strings.Split(path, "."), "")
	return out
}

// instancePath renders the validator's location tokens in the diagnostic path
// spelling: "serves[0].name".
func instancePath(tokens []string) string {
	var b strings.Builder
	for _, tok := range tokens {
		if _, err := strconv.Atoi(tok); err == nil {
			b.WriteString("[" + tok + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(tok)
	}
	return b.String()
}

func joinPath(base, name string) string {
	if base == "" {
		return name
	}
	return base + "." + name
}
