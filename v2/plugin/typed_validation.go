package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// DecodeValidated shares the generated command's strict JSON and semantic
// validation with host dispatchers. It does not authenticate a caller or verify
// a schema header: the dispatcher must do both before decoding a known payload.
func DecodeValidated[T interface{ Validate() error }](data []byte) (T, error) {
	var value T
	if err := decodeCommand(data, &value, true); err != nil {
		return value, err
	}
	if err := value.Validate(); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

func decodeCommand(data []byte, into any, strict bool) error {
	if !strict {
		return json.Unmarshal(data, into)
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("command exceeds 64 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := checkJSONValue(d, reflect.TypeOf(into).Elem()); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("command must contain one JSON value")
	}
	return json.Unmarshal(data, into)
}

// Check exact Go JSON field names before Unmarshal, whose default accepts
// case-folded and duplicate keys. This is validation, never schema hashing.
func checkJSONValue(d *json.Decoder, typ reflect.Type) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			return nil
		}
		return fmt.Errorf("null scalar or object")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			if typ.Kind() != reflect.Struct {
				return fmt.Errorf("unexpected object")
			}
			fields := map[string]reflect.Type{}
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				name := strings.Split(f.Tag.Get("json"), ",")[0]
				if name != "-" && f.IsExported() {
					if name == "" {
						name = f.Name
					}
					fields[name] = f.Type
				}
			}
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				field, known := fields[name]
				if !ok || !known || seen[name] {
					return fmt.Errorf("unknown or duplicate field %q", name)
				}
				seen[name] = true
				if err := checkJSONValue(d, field); err != nil {
					return err
				}
			}
		case '[':
			if typ.Kind() != reflect.Slice && typ.Kind() != reflect.Array {
				return fmt.Errorf("unexpected array")
			}
			for d.More() {
				if err := checkJSONValue(d, typ.Elem()); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected delimiter")
		}
		_, err := d.Token()
		return err
	}
	// Exact scalar kinds and numeric-string tags are enforced by Unmarshal.
	if typ.Kind() == reflect.Struct || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		return fmt.Errorf("unexpected scalar")
	}
	return nil
}
