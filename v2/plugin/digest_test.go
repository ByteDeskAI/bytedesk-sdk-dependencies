package plugin

import (
	"slices"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// TestGrantsDigestGolden pins the algorithm. It is not testing arithmetic: a
// dev-grants entry on a running host is KEYED by this string, so changing the
// canonicalisation silently invalidates every recorded approval and re-asks
// every operator. Failing here means someone has to decide that deliberately
// and say so in the changelog.
func TestGrantsDigestGolden(t *testing.T) {
	const want = "191228d5659340a3d5ff4c7ae7435c622a8e0cf9b04e31ca89249792d653c234"
	if got := GrantsDigest(fullManifest()); got != want {
		t.Fatalf("GrantsDigest(reference manifest) = %q, want %q\n\n"+
			"A dev-grants entry is keyed by this digest. If the canonicalisation "+
			"changed on purpose, bump GrantsDigestAlgorithm and update this "+
			"literal; if it did not, the change re-asks every operator.", got, want)
	}
}

// TestGrantsDigestCoversEveryConsentedField walks each field an operator
// approves and asserts the digest moved. A field that can change without moving
// the digest is a field a plugin can widen after approval.
func TestGrantsDigestCoversEveryConsentedField(t *testing.T) {
	base := GrantsDigest(fullManifest())
	cases := []struct {
		field  string
		mutate func(*Manifest)
	}{
		{"permissions.publish", func(m *Manifest) { m.Permissions.Publish = append(m.Permissions.Publish, "event.extra.>") }},
		{"permissions.subscribe", func(m *Manifest) { m.Permissions.Subscribe = append(m.Permissions.Subscribe, "event.extra.>") }},
		{"permissions.request", func(m *Manifest) { m.Permissions.Request = append(m.Permissions.Request, "cmd.extra.v1.go") }},
		{"serves[].name", func(m *Manifest) { m.Serves[0].Name = "tmux2" }},
		{"serves[].version", func(m *Manifest) { m.Serves[0].Version = "2.0.0" }},
		{"serves[].queueGroup", func(m *Manifest) { m.Serves[0].QueueGroup = "other" }},
		{"serves[].endpoints[].name", func(m *Manifest) { m.Serves[0].Endpoints[0].Name = "listing" }},
		{"serves[].endpoints[].subject", func(m *Manifest) { m.Serves[0].Endpoints[0].Subject = "svc.tmux-manager.listing" }},
		{"serves[].endpoints[].point", func(m *Manifest) { m.Serves[0].Endpoints[0].Point = string(PointMCPTool) }},
		{"serves[] added", func(m *Manifest) { m.Serves = append(m.Serves, ServiceDecl{Name: "extra"}) }},
		{"serves[].endpoints[] added", func(m *Manifest) {
			m.Serves[0].Endpoints = append(m.Serves[0].Endpoints, EndpointDecl{Name: "kill", Subject: "cmd.tmux-manager.v1.kill"})
		}},
		{"streams[].name", func(m *Manifest) { m.Streams[0].Name = "tmux-events-2" }},
		{"streams[].subjects", func(m *Manifest) { m.Streams[0].Subjects = append(m.Streams[0].Subjects, "event.other.>") }},
		{"streams[].maxBytes", func(m *Manifest) { m.Streams[0].MaxBytes *= 2 }},
		{"streams[].maxAgeSeconds", func(m *Manifest) { m.Streams[0].MaxAgeSeconds *= 2 }},
		{"streams[].maxMsgs", func(m *Manifest) { m.Streams[0].MaxMsgs *= 2 }},
		{"streams[] added", func(m *Manifest) { m.Streams = append(m.Streams, StreamDecl{Name: "extra"}) }},
		{"kv[].name", func(m *Manifest) { m.KV[0].Name = "tmux-prefs-2" }},
		{"kv[].maxBytes", func(m *Manifest) { m.KV[0].MaxBytes *= 2 }},
		{"kv[].ttlSeconds", func(m *Manifest) { m.KV[0].TTLSeconds *= 2 }},
		{"kv[].history", func(m *Manifest) { m.KV[0].History++ }},
		{"kv[] added", func(m *Manifest) { m.KV = append(m.KV, KVDecl{Name: "extra"}) }},
		{"objects[].name", func(m *Manifest) { m.Objects[0].Name = "tmux-captures-2" }},
		{"objects[].maxBytes", func(m *Manifest) { m.Objects[0].MaxBytes *= 2 }},
		{"objects[].ttlSeconds", func(m *Manifest) { m.Objects[0].TTLSeconds *= 2 }},
		{"objects[] added", func(m *Manifest) { m.Objects = append(m.Objects, ObjectDecl{Name: "extra"}) }},
		{"needs", func(m *Manifest) { m.Needs = append(m.Needs, "objects") }},
	}
	seen := map[string]string{base: "the reference manifest"}
	for _, c := range cases {
		m := fullManifest()
		c.mutate(&m)
		got := GrantsDigest(m)
		if got == base {
			t.Errorf("%s changed without moving the digest: a plugin can widen it after approval", c.field)
			continue
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s collides with %s", c.field, other)
		}
		seen[got] = c.field
	}
}

// TestGrantsDigestIgnoresOrder: reformatting a manifest must not re-ask the
// operator. An operator who is re-asked for nothing learns to approve without
// reading, which is the failure this property prevents.
func TestGrantsDigestIgnoresOrder(t *testing.T) {
	base := fullManifest()
	base.Permissions.Publish = append(base.Permissions.Publish, "event.other.>")
	base.Permissions.Subscribe = append(base.Permissions.Subscribe, "event.more.>")
	base.Permissions.Request = append(base.Permissions.Request, "cmd.files.v1.stat")
	base.Serves = append(base.Serves, ServiceDecl{Name: "aux", Endpoints: []EndpointDecl{
		{Name: "ping", Subject: "svc.tmux-manager.ping"},
	}})
	base.Streams[0].Subjects = append(base.Streams[0].Subjects, "tick.tmux-manager.>")
	base.KV = append(base.KV, KVDecl{Name: "aux-kv"})
	base.Objects = append(base.Objects, ObjectDecl{Name: "aux-obj"})

	shuffled := reverseCoveredLists(base)
	if GrantsDigest(base) != GrantsDigest(shuffled) {
		t.Fatal("reordering the manifest moved the digest; every reformat would re-ask the operator")
	}
}

func reverseCoveredLists(m Manifest) Manifest {
	m.Permissions.Publish = reversed(m.Permissions.Publish)
	m.Permissions.Subscribe = reversed(m.Permissions.Subscribe)
	m.Permissions.Request = reversed(m.Permissions.Request)
	m.Needs = reversed(m.Needs)
	m.Serves = reversed(m.Serves)
	for i := range m.Serves {
		m.Serves[i].Endpoints = reversed(m.Serves[i].Endpoints)
	}
	m.Streams = reversed(m.Streams)
	for i := range m.Streams {
		m.Streams[i].Subjects = reversed(m.Streams[i].Subjects)
	}
	m.KV = reversed(m.KV)
	m.Objects = reversed(m.Objects)
	return m
}

func reversed[T any](in []T) []T {
	out := slices.Clone(in)
	slices.Reverse(out)
	return out
}

// TestGrantsDigestCoversOnlyConsent: a version bump, a new nav entry or a
// renamed panel is not a widening of authority, and re-asking for one trains
// operators to click through the ones that are.
func TestGrantsDigestIgnoresFieldsOutsideConsent(t *testing.T) {
	base := GrantsDigest(fullManifest())
	for _, mutate := range []func(*Manifest){
		func(m *Manifest) { m.Version = "9.9.9" },
		func(m *Manifest) { m.Nav = append(m.Nav, NavItem{ID: "x", Label: "X", Href: "/x"}) },
		func(m *Manifest) { m.Panels[0].URL = "/p/tmux-manager/other" },
		func(m *Manifest) { m.Critical = true },
		func(m *Manifest) { m.Routes = append(m.Routes, "/p/tmux-manager/extra") },
	} {
		m := fullManifest()
		mutate(&m)
		if GrantsDigest(m) != base {
			t.Error("a field outside the consent surface moved the digest")
		}
	}
}

// TestGrantsDigestTreatsNilAndEmptyAlike: a manifest that omits a list and one
// that writes it empty consent to the same thing, and must not be re-asked.
func TestGrantsDigestTreatsNilAndEmptyAlike(t *testing.T) {
	empty := Manifest{ID: "x", Version: "1"}
	explicit := Manifest{ID: "x", Version: "1",
		Permissions: Permissions{Publish: []bus.Pattern{}, Subscribe: []bus.Pattern{}, Request: []bus.Pattern{}},
		Serves:      []ServiceDecl{}, Streams: []StreamDecl{}, KV: []KVDecl{}, Objects: []ObjectDecl{}, Needs: []string{},
	}
	if GrantsDigest(empty) != GrantsDigest(explicit) {
		t.Fatal("an omitted list and an empty one produced different digests")
	}
	if GrantsDigest(empty) == GrantsDigest(fullManifest()) {
		t.Fatal("an empty manifest digests the same as a fully populated one")
	}
}
