package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestAIArtifactsMatchCanonicalModel(t *testing.T) {
	for _, pkg := range []string{"aidecision", "payloads", "provideraccess", "codingsessions", "hostsettings"} {
		for emit, path := range map[string]string{"go": "payload_gen.go", "dts": "typescript/contracts.d.ts", "js": "typescript/validators.js", "descriptors-js": "typescript/descriptors.js"} {
			t.Run(pkg+"/"+emit, func(t *testing.T) {
				got, err := generate(emit, pkg)
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(filepath.Join("../..", pkg, path))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("regenerate %s %s", pkg, emit)
				}
			})
		}
	}
}
func TestProviderSchemaChangesWithConcreteAddress(t *testing.T) {
	a, err := providerTarget("aidecision", "jev")
	if err != nil {
		t.Fatal(err)
	}
	b, err := providerTarget("aidecision", "another-provider")
	if err != nil {
		t.Fatal(err)
	}
	ma, _ := buildModels(a.roots)
	mb, _ := buildModels(b.roots)
	ha, _ := hashes(ma, a.ops)
	hb, _ := hashes(mb, b.ops)
	for name, hash := range ha {
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(hash) || hash == hb[name] {
			t.Fatalf("nonconcrete hash %s %q", name, hash)
		}
	}
	for _, pkg := range []string{"aidecision", "hostsettings"} {
		raw, err := generateProvider("go", pkg, "jev", "jevcontracts")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte("plugin.NewValidatedCommand")) || !bytes.Contains(raw, []byte("svc.jev.")) {
			t.Fatalf("bad generated descriptor %s", raw)
		}
	}
	for _, id := range []string{"gateway", "host", "a.b", "a.*", "../escape", ""} {
		if _, err := providerTarget("aidecision", id); err == nil {
			t.Fatalf("accepted provider %q", id)
		}
	}
}
