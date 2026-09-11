package plugin

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Config field kinds. The set is closed: a renderer that meets a kind it does
// not know refuses the section rather than guessing a control.
const (
	ConfigKindBool       = "bool"
	ConfigKindInt        = "int"
	ConfigKindString     = "string"
	ConfigKindStringList = "stringList"
	ConfigKindEnum       = "enum"
	ConfigKindSecret     = "secret"
)

// ConfigField describes one value a settings section shows. Like the section it
// belongs to, it is schema and never a value: the host renders a control from it
// and reads the value from the section snapshot.
//
// Key is a dot-separated path into the section's snapshot and patch body, so a
// value nested under an object ("prefs.muteToasts") is addressable without a
// second structure. Default is the value as text, read by Kind ("true", "30",
// "high"); stringList and secret fields take none. Min and Max bound an int.
// Choices lists an enum's values. A secret field's value is never rendered: a
// read-only secret shows only whether it is set, and a writable one is sent only
// when the operator types a replacement.
type ConfigField struct {
	Key             string   `json:"key" bd:"public"`
	Kind            string   `json:"kind" bd:"public"`
	Label           string   `json:"label,omitempty" bd:"public"`
	Description     string   `json:"description,omitempty" bd:"public"`
	Default         string   `json:"default,omitempty" bd:"public"`
	Min             *int     `json:"min,omitempty" bd:"public"`
	Max             *int     `json:"max,omitempty" bd:"public"`
	Nullable        bool     `json:"nullable,omitempty" bd:"public"`
	Choices         []string `json:"choices,omitempty" bd:"public"`
	ReadOnly        bool     `json:"readOnly,omitempty" bd:"public"`
	RequiresRestart bool     `json:"requiresRestart,omitempty" bd:"public"`
}

// ConfigFieldsFromStruct derives fields from a struct's json and config tags, so
// a plugin whose settings already live in a typed struct declares its schema
// from that struct instead of restating it.
//
// The json tag gives the key; the Go type gives the kind (bool, int, string,
// []string); a pointer makes the field nullable. `json:"-"` or `config:"-"`
// skips a field. The config tag is a comma-separated list of:
//
//	label=Text  enum=a|b|c  min=0  max=23  default=high  readonly  secret  restart
//
// Unknown keys, a key used on the wrong kind, and a default the field would
// reject are errors naming Type.Field. A field of any other type is an error
// too: exclude it explicitly rather than have it silently vanish.
func ConfigFieldsFromStruct(v any) ([]ConfigField, error) {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("config fields: %T is not a struct", v)
	}
	var out []ConfigField
	for i := range t.NumField() {
		sf := t.Field(i)
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		tag := sf.Tag.Get("config")
		if !sf.IsExported() || name == "-" || tag == "-" {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		f, err := configFieldFor(name, sf.Type, tag)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), sf.Name, err)
		}
		out = append(out, f)
	}
	return out, nil
}

func configFieldFor(key string, typ reflect.Type, tag string) (ConfigField, error) {
	f := ConfigField{Key: key, Label: key}
	if typ.Kind() == reflect.Pointer {
		f.Nullable = true
		typ = typ.Elem()
	}
	switch {
	case typ.Kind() == reflect.Bool:
		f.Kind = ConfigKindBool
	case typ.Kind() == reflect.Int || typ.Kind() == reflect.Int32:
		f.Kind = ConfigKindInt
	case typ.Kind() == reflect.String:
		f.Kind = ConfigKindString
	case typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.String:
		f.Kind = ConfigKindStringList
	default:
		return f, fmt.Errorf("%s has no config kind (bool, int, string, []string); tag it config:\"-\"", typ)
	}
	if tag == "" {
		return f, f.validate()
	}
	for _, opt := range strings.Split(tag, ",") {
		k, val, hasVal := strings.Cut(strings.TrimSpace(opt), "=")
		flag := func(set *bool) error {
			if hasVal {
				return fmt.Errorf("config tag %q takes no value", k)
			}
			*set = true
			return nil
		}
		var err error
		switch k {
		case "label":
			f.Label = val
		case "default":
			f.Default = val
		case "enum":
			if f.Kind != ConfigKindString {
				return f, fmt.Errorf("config tag enum needs a string field, not %s", f.Kind)
			}
			f.Kind, f.Choices = ConfigKindEnum, strings.Split(val, "|")
		case "min", "max":
			n, convErr := strconv.Atoi(val)
			if convErr != nil {
				return f, fmt.Errorf("config tag %s=%q is not an integer", k, val)
			}
			if k == "min" {
				f.Min = &n
			} else {
				f.Max = &n
			}
		case "readonly":
			err = flag(&f.ReadOnly)
		case "restart":
			err = flag(&f.RequiresRestart)
		case "secret":
			if f.Kind != ConfigKindString {
				return f, fmt.Errorf("config tag secret needs a string field, not %s", f.Kind)
			}
			f.Kind = ConfigKindSecret
			err = flag(new(bool))
		default:
			return f, fmt.Errorf("unknown config tag key %q", k)
		}
		if err != nil {
			return f, err
		}
	}
	return f, f.validate()
}

func (f ConfigField) validate() error {
	if strings.TrimSpace(f.Key) == "" {
		return fmt.Errorf("config field key required")
	}
	if !slices.Contains([]string{ConfigKindBool, ConfigKindInt, ConfigKindString, ConfigKindStringList, ConfigKindEnum, ConfigKindSecret}, f.Kind) {
		return fmt.Errorf("config field %s: unknown kind %q", f.Key, f.Kind)
	}
	if (f.Min != nil || f.Max != nil) && f.Kind != ConfigKindInt {
		return fmt.Errorf("config field %s: min/max apply only to int", f.Key)
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return fmt.Errorf("config field %s: min %d exceeds max %d", f.Key, *f.Min, *f.Max)
	}
	if (len(f.Choices) > 0) != (f.Kind == ConfigKindEnum) || slices.Contains(f.Choices, "") {
		return fmt.Errorf("config field %s: an enum needs non-empty choices, and only an enum has them", f.Key)
	}
	if f.Default == "" {
		return nil
	}
	switch f.Kind {
	case ConfigKindBool:
		if _, err := strconv.ParseBool(f.Default); err != nil {
			return fmt.Errorf("config field %s: default %q is not a bool", f.Key, f.Default)
		}
	case ConfigKindInt:
		n, err := strconv.Atoi(f.Default)
		if err != nil || (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return fmt.Errorf("config field %s: default %q is not an int within bounds", f.Key, f.Default)
		}
	case ConfigKindEnum:
		if !slices.Contains(f.Choices, f.Default) {
			return fmt.Errorf("config field %s: default %q is not one of its choices", f.Key, f.Default)
		}
	case ConfigKindStringList, ConfigKindSecret:
		return fmt.Errorf("config field %s: a %s field takes no default", f.Key, f.Kind)
	}
	return nil
}

// validate checks the declared sections: ids present and unique, and every
// field well formed with a key unique within its section.
func (c *Config) validate() error {
	if c == nil {
		return nil
	}
	sections := map[string]bool{}
	for _, s := range c.Sections {
		id := strings.TrimSpace(s.ID)
		if id == "" || sections[id] {
			return fmt.Errorf("config.sections: id %q is empty or repeated", s.ID)
		}
		sections[id] = true
		keys := map[string]bool{}
		for _, f := range s.Fields {
			if err := f.validate(); err != nil {
				return fmt.Errorf("config.sections %s: %w", id, err)
			}
			if keys[f.Key] {
				return fmt.Errorf("config.sections %s: field key %q repeated", id, f.Key)
			}
			keys[f.Key] = true
		}
	}
	return nil
}
