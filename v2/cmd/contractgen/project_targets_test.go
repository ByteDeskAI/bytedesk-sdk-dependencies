package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectArtifactsMatchCanonicalModel(t *testing.T) {
	for _, pkg := range []string{"projecttasks", "projectknowledge"} {
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

func TestProjectProviderDescriptorsAreAddressBoundAndValidated(t *testing.T) {
	for pkg, point := range map[string]string{"projecttasks": "project.tasks", "projectknowledge": "project.knowledge"} {
		a, err := providerTarget(pkg, "example-one")
		if err != nil {
			t.Fatal(err)
		}
		b, err := providerTarget(pkg, "example-two")
		if err != nil {
			t.Fatal(err)
		}
		ma, err := buildModels(a.roots)
		if err != nil {
			t.Fatal(err)
		}
		mb, err := buildModels(b.roots)
		if err != nil {
			t.Fatal(err)
		}
		ha, err := hashes(ma, a.ops)
		if err != nil {
			t.Fatal(err)
		}
		hb, err := hashes(mb, b.ops)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range a.ops {
			if !strings.HasPrefix(op.name, "svc.example-one."+point+".v1.") || !op.validated || ha[op.goVar] == hb[op.goVar] {
				t.Fatalf("unfenced provider descriptor %+v", op)
			}
		}
		for _, emit := range []string{"go", "json", "descriptors-js"} {
			got, err := generateProvider(emit, pkg, "example-one", "contracts")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(got, []byte("svc.example-one."+point+".v1.write")) {
				t.Fatalf("missing concrete provider in %s", emit)
			}
		}
	}
}
