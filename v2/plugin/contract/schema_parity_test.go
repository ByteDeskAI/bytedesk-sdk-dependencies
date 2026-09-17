package contract

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// TestSchemaParity keeps plugin.schema.json and plugin.Manifest the same
// shape: every property is a field and every field is a property, and a
// property is required exactly when its field has no omitempty/omitzero. The
// schema is canonical — a field added to the struct without the schema fails
// here, and so does a schema property nobody decodes. Types are not compared:
// the corpus proves those.
func TestSchemaParity(t *testing.T) {
	schema := schemaDocument()
	defs, _ := schema["$defs"].(map[string]any)
	if defs == nil {
		t.Fatal("schema has no $defs")
	}

	reachable := map[string]reflect.Type{}
	collectStructs(reflect.TypeOf(plugin.Manifest{}), reachable)
	delete(reachable, "Manifest")

	checkStruct(t, "Manifest", reflect.TypeOf(plugin.Manifest{}), schema)
	for name, rt := range reachable {
		def, ok := defs[name].(map[string]any)
		if !ok {
			t.Errorf("plugin.%s is reachable from Manifest but the schema has no $defs/%s", name, name)
			continue
		}
		checkStruct(t, name, rt, def)
	}
	for name := range defs {
		if _, ok := reachable[name]; !ok {
			t.Errorf("schema defines $defs/%s but no Go type of that name is reachable from Manifest", name)
		}
	}
	if len(reachable) < 20 {
		t.Fatalf("only %d nested types found; the reflect walk is not reaching the manifest", len(reachable))
	}
}

func checkStruct(t *testing.T, name string, rt reflect.Type, schema map[string]any) {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		t.Errorf("%s: schema object has no properties", name)
		return
	}
	var wantRequired []string
	fields := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		jsonName, opts, _ := strings.Cut(tag, ",")
		if jsonName == "-" || jsonName == "" {
			continue
		}
		fields[jsonName] = true
		if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
			wantRequired = append(wantRequired, jsonName)
		}
		if _, ok := props[jsonName]; !ok {
			t.Errorf("%s.%s (json %q) has no schema property", name, f.Name, jsonName)
		}
	}
	for prop := range props {
		if !fields[prop] {
			t.Errorf("%s: schema property %q has no Go field", name, prop)
		}
	}
	var gotRequired []string
	if list, ok := schema["required"].([]any); ok {
		for _, v := range list {
			gotRequired = append(gotRequired, v.(string))
		}
	}
	slices.Sort(wantRequired)
	slices.Sort(gotRequired)
	if !slices.Equal(wantRequired, gotRequired) {
		t.Errorf("%s: required %v, but the fields without omitempty are %v", name, gotRequired, wantRequired)
	}
}

// collectStructs registers every named struct type reachable through fields,
// pointers and slices, keyed by its Go name, which is also its $defs name.
func collectStructs(rt reflect.Type, out map[string]reflect.Type) {
	for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct || rt == reflect.TypeOf(time.Time{}) {
		return
	}
	if _, seen := out[rt.Name()]; seen {
		return
	}
	out[rt.Name()] = rt
	for i := 0; i < rt.NumField(); i++ {
		collectStructs(rt.Field(i).Type, out)
	}
}
