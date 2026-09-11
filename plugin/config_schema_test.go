package plugin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

type tagged struct {
	Muted    []string  `json:"muted,omitempty" config:"label=Muted kinds"`
	Toasts   bool      `json:"toasts,omitempty" config:"label=Mute toasts,default=false"`
	Quiet    *int      `json:"quiet,omitempty" config:"label=Quiet hour,min=0,max=23"`
	Severity string    `json:"severity,omitempty" config:"enum=low|high,default=high"`
	Token    string    `json:"token" config:"secret,readonly,restart"`
	Plain    string    `json:"plain"`
	Updated  time.Time `json:"updated" config:"-"`
	Hidden   string    `json:"-"`
	internal int
}

func intp(n int) *int { return &n }

func TestConfigFieldsFromStruct(t *testing.T) {
	got, err := ConfigFieldsFromStruct(&tagged{})
	if err != nil {
		t.Fatal(err)
	}
	want := []ConfigField{
		{Key: "muted", Kind: ConfigKindStringList, Label: "Muted kinds"},
		{Key: "toasts", Kind: ConfigKindBool, Label: "Mute toasts", Default: "false"},
		{Key: "quiet", Kind: ConfigKindInt, Label: "Quiet hour", Min: intp(0), Max: intp(23), Nullable: true},
		{Key: "severity", Kind: ConfigKindEnum, Label: "severity", Default: "high", Choices: []string{"low", "high"}},
		{Key: "token", Kind: ConfigKindSecret, Label: "token", ReadOnly: true, RequiresRestart: true},
		{Key: "plain", Kind: ConfigKindString, Label: "plain"},
	}
	if !reflect.DeepEqual(got, want) {
		a, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("fields:\n%s", a)
	}
}

func TestConfigFieldsFromStructRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		v    any
		want string
	}{
		"not a struct": {42, "not a struct"},
		"unknown tag key": {struct {
			A bool `config:"lable=x"`
		}{}, `unknown config tag key "lable"`},
		"untyped kind": {struct{ A time.Time }{}, "has no config kind"},
		"enum on bool": {struct {
			A bool `config:"enum=a|b"`
		}{}, "enum needs a string field"},
		"secret on int": {struct {
			A int `config:"secret"`
		}{}, "secret needs a string field"},
		"min on string": {struct {
			A string `config:"min=1"`
		}{}, "min/max apply only to int"},
		"min not a number": {struct {
			A int `config:"min=x"`
		}{}, "not an integer"},
		"min above max": {struct {
			A int `config:"min=5,max=1"`
		}{}, "exceeds max"},
		"flag with value": {struct {
			A bool `config:"readonly=true"`
		}{}, "takes no value"},
		"bad bool default": {struct {
			A bool `config:"default=yes"`
		}{}, "not a bool"},
		"int out of bounds": {struct {
			A int `config:"max=3,default=9"`
		}{}, "within bounds"},
		"enum default": {struct {
			A string `config:"enum=a|b,default=c"`
		}{}, "not one of its choices"},
		"empty choice": {struct {
			A string `config:"enum=a||b"`
		}{}, "non-empty choices"},
		"list default": {struct {
			A []string `config:"default=a"`
		}{}, "takes no default"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ConfigFieldsFromStruct(tc.v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// The schema is manifest data: it parses, validates and survives a round trip
// with no call to the plugin.
func TestManifestConfigSchemaIsData(t *testing.T) {
	raw := []byte(`{"id":"notifications","version":"1","config":{"sections":[{"id":"notifications","title":"Notifications","fields":[
	  {"key":"prefs.muteToasts","kind":"bool","label":"Mute toasts"},
	  {"key":"prefs.quietStartHour","kind":"int","min":0,"max":23,"nullable":true},
	  {"key":"prefs.minSeverityNtfy","kind":"enum","choices":["low","high"],"default":"high"},
	  {"key":"ntfyURL","kind":"secret","readOnly":true,"requiresRestart":true}]}]}}`)
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if f := m.Config.Sections[0].Fields; len(f) != 4 || *f[1].Max != 23 || !f[3].ReadOnly {
		t.Fatalf("fields = %+v", f)
	}
	out, _ := json.Marshal(m)
	again, err := ParseManifest(out)
	if err != nil || !reflect.DeepEqual(m, again) {
		t.Fatalf("round trip: %v", err)
	}

	for name, cfg := range map[string]*Config{
		"repeated section": {Sections: []ConfigSection{{ID: "a"}, {ID: "a"}}},
		"empty section id": {Sections: []ConfigSection{{}}},
		"unknown kind":     {Sections: []ConfigSection{{ID: "a", Fields: []ConfigField{{Key: "x", Kind: "color"}}}}},
		"repeated key":     {Sections: []ConfigSection{{ID: "a", Fields: []ConfigField{{Key: "x", Kind: "bool"}, {Key: "x", Kind: "int"}}}}},
		"choices on int":   {Sections: []ConfigSection{{ID: "a", Fields: []ConfigField{{Key: "x", Kind: "int", Choices: []string{"1"}}}}}},
	} {
		if err := (Manifest{ID: "p", Version: "1", Config: cfg}).Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}
