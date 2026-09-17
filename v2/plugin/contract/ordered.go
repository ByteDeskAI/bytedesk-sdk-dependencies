package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

// An ordered JSON object, because a migration that reorders keys is a
// migration nobody reviews. Every value stays a json.RawMessage, so a subtree
// the rewrite does not touch comes out byte-for-byte as the author wrote it,
// and a subtree it does touch is rebuilt from its own ordered members rather
// than from a Go map.
type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

// decodeObject reads a JSON object preserving key order. A duplicate key keeps
// its first position and its last value, which is what encoding/json does.
func decodeObject(raw []byte) (*jsonObject, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("want a JSON object, got %v", tok)
	}
	obj := &jsonObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("want an object key, got %v", keyTok)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		if _, seen := obj.vals[key]; !seen {
			obj.keys = append(obj.keys, key)
		}
		obj.vals[key] = val
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the object")
	}
	return obj, nil
}

func (o *jsonObject) has(key string) bool { _, ok := o.vals[key]; return ok }

func (o *jsonObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// str returns a string-valued member, and false for absent or any other type.
func (o *jsonObject) str(key string) (string, bool) {
	raw, ok := o.vals[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// set replaces a member in place, or appends it when it is new.
func (o *jsonObject) set(key string, val json.RawMessage) {
	if !o.has(key) {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// insertAfter puts a new member directly after anchor, so the migrated
// document reads in the order a human expects rather than in arrival order.
// An existing member keeps its position.
func (o *jsonObject) insertAfter(anchor, key string, val json.RawMessage) {
	if o.has(key) {
		o.vals[key] = val
		return
	}
	at := slices.Index(o.keys, anchor)
	if at < 0 {
		o.set(key, val)
		return
	}
	o.keys = slices.Insert(o.keys, at+1, key)
	o.vals[key] = val
}

func (o *jsonObject) prepend(key string, val json.RawMessage) {
	if o.has(key) {
		o.vals[key] = val
		return
	}
	o.keys = slices.Insert(o.keys, 0, key)
	o.vals[key] = val
}

func (o *jsonObject) delete(key string) {
	if !o.has(key) {
		return
	}
	delete(o.vals, key)
	o.keys = slices.DeleteFunc(o.keys, func(k string) bool { return k == key })
}

func (o *jsonObject) empty() bool { return len(o.keys) == 0 }

// MarshalJSON emits the members in their recorded order.
func (o *jsonObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(o.vals[key])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// indented is the file form: two-space indentation and a trailing newline, the
// shape the in-tree manifests already carry, so a migration diff shows only
// what the migration changed.
func (o *jsonObject) indented() ([]byte, error) {
	compact, err := o.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// mustRaw marshals a value that cannot fail to marshal (strings, ints, and
// structures built here). A failure is a programming error, not input.
func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("contract: marshalling a migrated value: " + err.Error())
	}
	return b
}
