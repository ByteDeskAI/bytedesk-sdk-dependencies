package bus

import "testing"

func grants() Grants {
	return Grants{
		Publish:   []Pattern{"event.files.>"},
		Subscribe: []Pattern{"event.*.changed"},
		Request:   []Pattern{"cmd.host.v1.*"},
		Serves:    []Pattern{"svc.files.>"},
		Streams:   []string{"FILES"},
		KV:        []string{"files"},
		Objects:   []string{"blobs"},
	}
}

func TestGrantsCan(t *testing.T) {
	g := grants()
	for _, tc := range []struct {
		kind GrantKind
		subj Subject
		want bool
	}{
		{GrantPublish, "event.files.changed", true},
		{GrantPublish, "event.other.changed", false},
		{GrantSubscribe, "event.files.changed", true},
		{GrantSubscribe, "event.files.deep.changed", false},
		{GrantRequest, "cmd.host.v1.list", true},
		{GrantRequest, "cmd.host.v2.list", false},
		{GrantServe, "svc.files.list", true},
		{GrantServe, "svc.other.list", false},
		{GrantKind("nonsense"), "event.files.changed", false},
	} {
		if got := g.Can(tc.kind, tc.subj); got != tc.want {
			t.Errorf("Can(%q, %q) = %v, want %v", tc.kind, tc.subj, got, tc.want)
		}
	}
}

func TestGrantsCanPattern(t *testing.T) {
	g := grants()
	if !g.CanPattern(GrantPublish, "event.files.changed") {
		t.Error("a concrete subject under the grant must be covered")
	}
	if !g.CanPattern(GrantPublish, "event.files.*") {
		t.Error(`"event.files.*" is inside "event.files.>"`)
	}
	if g.CanPattern(GrantPublish, "event.>") {
		t.Error(`"event.>" is wider than "event.files.>" and must NOT be covered`)
	}
}

func TestGrantsCanUse(t *testing.T) {
	g := grants()
	for _, tc := range []struct {
		kind AssetKind
		name string
		want bool
	}{
		{AssetStream, "FILES", true},
		{AssetStream, "OTHER", false},
		{AssetKV, "files", true},
		{AssetObjects, "blobs", true},
		{AssetKind("nonsense"), "files", false},
	} {
		if got := g.CanUse(tc.kind, tc.name); got != tc.want {
			t.Errorf("CanUse(%q, %q) = %v, want %v", tc.kind, tc.name, got, tc.want)
		}
	}
}

// TestGrantsCloneCannotBeWidened is the reason Clone exists: a caller handed a
// Grants must not be able to append to the slice it received and find itself
// with more authority than the host compiled.
func TestGrantsCloneCannotBeWidened(t *testing.T) {
	g := grants()
	c := g.Clone()
	c.Publish = append(c.Publish, ">")
	if g.Can(GrantPublish, "anything.at.all") {
		t.Fatal("appending to a clone widened the original")
	}
	c.Streams[0] = "OTHER"
	if !g.CanUse(AssetStream, "FILES") {
		t.Fatal("writing through a clone's slice changed the original")
	}
}

func TestGrantsIsZero(t *testing.T) {
	if !(Grants{}).IsZero() {
		t.Error("an empty Grants is zero")
	}
	if grants().IsZero() {
		t.Error("a populated Grants is not zero")
	}
	if !(Grants{Publish: []Pattern{}}).IsZero() {
		t.Error("empty slices are still zero")
	}
}

// TestLeaseIsOpaque pins the one operation a lease supports.
func TestLeaseIsOpaque(t *testing.T) {
	a, b := NewLease("tok-1"), NewLease("tok-1")
	if !a.Equal(b) {
		t.Error("two leases with the same token are equal")
	}
	if a.Equal(NewLease("tok-2")) {
		t.Error("different tokens are not equal")
	}
	if !(Lease{}).IsZero() {
		t.Error("the zero lease is zero")
	}
	if a.IsZero() {
		t.Error("a minted lease is not zero")
	}
}

// TestIdentityStringNeverLeaksTheLease matters because Identity.String is what
// goes into every refusal an operator reads. A lease token in a log is a
// credential in a log.
func TestIdentityStringNeverLeaksTheLease(t *testing.T) {
	id := Identity{PluginID: "files", Generation: "7", Lease: NewLease("secret-lease-token"), Role: RolePlugin}
	got := id.String()
	if got != "files@7" {
		t.Errorf("Identity.String() = %q, want %q", got, "files@7")
	}
	if contains(got, "secret-lease-token") {
		t.Fatal("Identity.String leaked the lease token")
	}
	if (Identity{}).String() != "<unbound>" {
		t.Error("an unbound identity says so")
	}
	if (Identity{PluginID: "files"}).String() != "files" {
		t.Error("an identity with no generation prints just the id")
	}
}

func TestIdentityAutonomous(t *testing.T) {
	if !(Identity{PluginID: "files"}).Autonomous() {
		t.Error("no lease means autonomous")
	}
	if (Identity{PluginID: "files", Lease: NewLease("x")}).Autonomous() {
		t.Error("a lease means a human principal is attached")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
