package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"sort"
)

// The two emitters v2 adds.
//
// v1 stopped at the Payload union and the typed wrappers, because a descriptor
// was only ever used from the package that declared it. v2's descriptors carry
// an ADDRESS as well as a shape, and two more consumers need them: a package
// that wants its descriptors without the wrappers, and the browser façade,
// which has no Go at all.
//
// Both are derived from the same operation table and the same digest, so a
// descriptor cannot mean one thing in Go and another in the browser.

// emitDescriptorsGo writes a package's descriptor variables on their own,
// without the Payload union or the wrappers. It is what a package that already
// has its union — or one that only ever consumes — includes.
func emitDescriptorsGo(t target, ms *modelSet) ([]byte, error) {
	digest, err := hashes(ms, t.ops)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(generatedHeader + "\n")
	fmt.Fprintf(&b, "// Regenerate: go run ./cmd/contractgen -emit=descriptors-go%s -out <path>\n\n", pkgFlag(t))
	fmt.Fprintf(&b, "package %s\n\n", t.pkg)
	b.WriteString("import \"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin\"\n")
	for _, op := range t.ops {
		fmt.Fprintf(&b, "\n// %s is the generated descriptor for %s revision %d.\n", op.goVar, op.name, op.rev)
		b.WriteString(descriptorVar(op, digest[op.goVar]))
	}
	return format.Source(b.Bytes())
}

// descriptorJSON is one descriptor as the browser sees it.
//
// The browser gets plain data, not a class: it has to be able to hold a
// descriptor it does not have a validator for, so that a contract added by a
// host newer than the page does not make the page throw.
type descriptorJSON struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Rev     uint32 `json:"rev"`
	Schema  string `json:"schema"`
	Subject string `json:"subject,omitempty"`
	// Stream, Bucket and Version are the addresses the other kinds use.
	Stream    string         `json:"stream,omitempty"`
	Subjects  []string       `json:"subjects,omitempty"`
	Bucket    string         `json:"bucket,omitempty"`
	Version   string         `json:"version,omitempty"`
	Endpoints []endpointJSON `json:"endpoints,omitempty"`
}

type endpointJSON struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
}

// emitDescriptorsJS writes the browser façade's descriptor table: the same
// operations, the same hashes, as a frozen JS object.
func emitDescriptorsJS(t target, ms *modelSet) ([]byte, error) {
	digest, err := hashes(ms, t.ops)
	if err != nil {
		return nil, err
	}
	byVar := make(map[string]descriptorJSON, len(t.ops))
	for _, op := range t.ops {
		d := descriptorJSON{
			Name:     op.name,
			Kind:     op.kind,
			Rev:      op.rev,
			Schema:   digest[op.goVar],
			Subject:  op.subject,
			Stream:   op.stream,
			Subjects: op.subjects,
			Bucket:   op.bucket,
			Version:  op.version,
		}
		for _, e := range op.endpoints {
			d.Endpoints = append(d.Endpoints, endpointJSON{Name: e.name, Subject: e.subject})
		}
		byVar[op.goVar] = d
	}

	names := make([]string, 0, len(byVar))
	for name := range byVar {
		names = append(names, name)
	}
	sort.Strings(names)

	var b bytes.Buffer
	b.WriteString(generatedHeader + "\n\n")
	b.WriteString("// Descriptors are frozen: a page that could rewrite a subject or a schema\n")
	b.WriteString("// hash could address a contract it was never granted.\n")
	b.WriteString("export const descriptors = Object.freeze({\n")
	for _, name := range names {
		raw, err := json.Marshal(byVar[name])
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %s: Object.freeze(%s),\n", name, raw)
	}
	b.WriteString("})\n")
	return b.Bytes(), nil
}
