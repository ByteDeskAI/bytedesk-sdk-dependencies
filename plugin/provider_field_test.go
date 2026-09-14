package plugin

import (
	"reflect"
	"testing"
)

func TestProviderFieldTags(t *testing.T) {
	for _, tag := range []string{"provider=host.data.store,requires=transactions|blobs", "requires=transactions|blobs,provider=host.data.store"} {
		f, err := configFieldFor("store", reflect.TypeOf(""), tag)
		if err != nil || f.Kind != "provider" || f.Point != "host.data.store" || !reflect.DeepEqual(f.Requires, []string{"transactions", "blobs"}) {
			t.Fatalf("%s: %+v %v", tag, f, err)
		}
	}
	for _, tag := range []string{"provider", "provider=", "requires", "requires=", "requires=transactions", "provider=host.data.store,requires=a|a", "provider=host.data.store,requires=a||b", "provider=host.data.store,requires=*", "provider=host.data.store,enum=x", "enum=x,provider=host.data.store", "provider=host.data.store,secret", "secret,provider=host.data.store", "provider=host.data.store,provider=host.other.store", "provider=host.data.store,min=1", "provider=host.data.store,requires=a,requires=b", "provider=host.*"} {
		if _, err := configFieldFor("store", reflect.TypeOf(""), tag); err == nil {
			t.Errorf("accepted %q", tag)
		}
	}
	if _, err := configFieldFor("store", reflect.TypeOf(1), "provider=host.data.store"); err == nil {
		t.Fatal("accepted int provider")
	}
	f, err := configFieldFor("store", reflect.TypeOf((*string)(nil)), "provider=host.data.store,default=sqlite,readonly,restart")
	if err != nil || !f.Nullable || !f.ReadOnly || !f.RequiresRestart {
		t.Fatalf("nullable: %+v %v", f, err)
	}
}

func TestProviderFieldManifestValidation(t *testing.T) {
	for _, field := range []ConfigField{
		{Key: "store", Kind: "provider", Point: "host.data.store"},
		{Key: "store", Kind: "provider", Point: "acme.store", Requires: []string{"transactions"}, Default: "sqlite"},
	} {
		if err := field.validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []ConfigField{
		{Key: "store", Kind: "provider"},
		{Key: "store", Kind: "string", Point: "host.data.store"},
		{Key: "store", Kind: "string", Requires: []string{"transactions"}},
		{Key: "store", Kind: "provider", Point: "host"},
		{Key: "store", Kind: "provider", Point: "host.data"},
		{Key: "store", Kind: "provider", Point: "Host.data.store"},
		{Key: "store", Kind: "provider", Point: "host..store"},
		{Key: "store", Kind: "provider", Point: "host.data.store", Default: "*"},
	} {
		if err := field.validate(); err == nil {
			t.Fatalf("accepted %+v", field)
		}
	}
}
