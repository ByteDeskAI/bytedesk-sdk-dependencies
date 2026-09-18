package attest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// registryFixture builds a signed publisher prefix in memory: one active key,
// one revoked, and the Store root that signed the manifest.
type registryFixture struct {
	manifest  PublisherManifest
	roots     Roots
	objects   map[string][]byte
	activeKID string
	activePub ed25519.PublicKey
	revokedID string
}

func newRegistryFixture(t *testing.T) registryFixture {
	t.Helper()
	storeSeed := seed(t)
	activeSeed, revokedSeed := seed(t), seed(t)
	activePub := ed25519.NewKeyFromSeed(activeSeed).Public().(ed25519.PublicKey)
	revokedPub := ed25519.NewKeyFromSeed(revokedSeed).Public().(ed25519.PublicKey)

	objects := map[string][]byte{
		"profile.json":           mustJSON(t, map[string]any{"id": "acme", "name": "Acme"}),
		KeyPath(KID(activePub)):  mustJSON(t, PublisherKey{KID: KID(activePub), PublicKey: base64.StdEncoding.EncodeToString(activePub), Status: KeyActive}),
		KeyPath(KID(revokedPub)): mustJSON(t, PublisherKey{KID: KID(revokedPub), PublicKey: base64.StdEncoding.EncodeToString(revokedPub), Status: KeyRevoked, RevokedAt: "2026-09-18T00:00:00Z"}),
	}
	m := PublisherManifest{ID: "acme"}
	for path, body := range objects {
		sum := sha256.Sum256(body)
		m.Objects = append(m.Objects, RegistryObject{Path: path, SHA256: hex.EncodeToString(sum[:])})
	}
	signed, err := SignPublisherManifest(storeSeed, m, "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return registryFixture{
		manifest:  signed,
		roots:     rootsFor(t, storeSeed),
		objects:   objects,
		activeKID: KID(activePub),
		activePub: activePub,
		revokedID: KID(revokedPub),
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestAnActiveKeyResolves is the ordinary path: one manifest, one key object,
// one trusted root, and the host may honour the key.
func TestAnActiveKeyResolves(t *testing.T) {
	f := newRegistryFixture(t)
	got, err := ResolvePublisherKey(f.manifest, "acme", f.activeKID, f.objects[KeyPath(f.activeKID)], f.roots)
	if err != nil {
		t.Fatalf("ResolvePublisherKey: %v", err)
	}
	if !got.Equal(f.activePub) {
		t.Fatal("resolved a different key than the registry holds")
	}
}

// TestARevokedKeyIsRefusedAndSaysSo: "revoked" and "never existed" are
// different answers, and a host that conflates them cannot tell a rotation
// from an attack.
func TestARevokedKeyIsRefusedAndSaysSo(t *testing.T) {
	f := newRegistryFixture(t)
	_, err := ResolvePublisherKey(f.manifest, "acme", f.revokedID, f.objects[KeyPath(f.revokedID)], f.roots)
	if !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("a revoked key resolved: %v", err)
	}
	_, err = ResolvePublisherKey(f.manifest, "acme", strings.Repeat("a", 64), nil, f.roots)
	if !errors.Is(err, ErrKeyNotInRegistry) {
		t.Fatalf("an unknown kid gave %v, want ErrKeyNotInRegistry", err)
	}
}

// TestASubstitutedObjectIsRefused: the manifest's hash is what makes an object
// trustworthy, so an object that does not match it must not be parsed — this
// is the whole reason the registry can live on a CDN nobody trusts.
func TestASubstitutedObjectIsRefused(t *testing.T) {
	f := newRegistryFixture(t)
	attacker := seed(t)
	attackerPub := ed25519.NewKeyFromSeed(attacker).Public().(ed25519.PublicKey)
	swapped := mustJSON(t, PublisherKey{
		KID:       f.activeKID, // claims the kid it is replacing
		PublicKey: base64.StdEncoding.EncodeToString(attackerPub),
		Status:    KeyActive,
	})
	_, err := ResolvePublisherKey(f.manifest, "acme", f.activeKID, swapped, f.roots)
	if !errors.Is(err, ErrObjectDigestMismatch) {
		t.Fatalf("a swapped key object was accepted: %v", err)
	}
}

// TestATamperedManifestIsRefused: adding an object to the list, or editing a
// hash in it, breaks the Store signature.
func TestATamperedManifestIsRefused(t *testing.T) {
	f := newRegistryFixture(t)
	attacker := seed(t)
	attackerPub := ed25519.NewKeyFromSeed(attacker).Public().(ed25519.PublicKey)
	body := mustJSON(t, PublisherKey{KID: KID(attackerPub), PublicKey: base64.StdEncoding.EncodeToString(attackerPub), Status: KeyActive})
	sum := sha256.Sum256(body)

	tampered := f.manifest
	tampered.Objects = append(append([]RegistryObject{}, f.manifest.Objects...),
		RegistryObject{Path: KeyPath(KID(attackerPub)), SHA256: hex.EncodeToString(sum[:])})

	if _, err := ResolvePublisherKey(tampered, "acme", KID(attackerPub), body, f.roots); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a key added to the manifest without the Store's signature was accepted: %v", err)
	}
}

// TestAnUntrustedStoreRootIsRefused: a manifest signed by a key the host does
// not embed proves nothing, however well formed it is.
func TestAnUntrustedStoreRootIsRefused(t *testing.T) {
	f := newRegistryFixture(t)
	stranger := rootsFor(t, seed(t))
	if _, err := ResolvePublisherKey(f.manifest, "acme", f.activeKID, f.objects[KeyPath(f.activeKID)], stranger); !errors.Is(err, ErrUntrustedRoot) {
		t.Fatalf("a manifest from an unknown root was accepted: %v", err)
	}
}

// TestAManifestForAnotherPublisherIsRefused: a valid manifest is still the
// wrong answer if it describes somebody else, which is what a mirror serving a
// stale or misrouted prefix looks like.
func TestAManifestForAnotherPublisherIsRefused(t *testing.T) {
	f := newRegistryFixture(t)
	if _, err := ResolvePublisherKey(f.manifest, "evilcorp", f.activeKID, f.objects[KeyPath(f.activeKID)], f.roots); !errors.Is(err, ErrKeyNotInRegistry) {
		t.Fatalf("acme's manifest answered for evilcorp: %v", err)
	}
}

// TestTheKIDIsDerivedNotBelieved: a key object claiming a kid it does not hash
// to would let one key masquerade as another inside a correctly signed
// manifest, so the kid is recomputed from the key material.
func TestTheKIDIsDerivedNotBelieved(t *testing.T) {
	storeSeed, realSeed := seed(t), seed(t)
	realPub := ed25519.NewKeyFromSeed(realSeed).Public().(ed25519.PublicKey)
	lyingKID := strings.Repeat("b", 64)

	// The object is at the path for the lying kid and claims it, but holds a
	// different key. The manifest signs it honestly, so only the derivation
	// catches this.
	body := mustJSON(t, PublisherKey{KID: lyingKID, PublicKey: base64.StdEncoding.EncodeToString(realPub), Status: KeyActive})
	sum := sha256.Sum256(body)
	m, err := SignPublisherManifest(storeSeed, PublisherManifest{
		ID:      "acme",
		Objects: []RegistryObject{{Path: KeyPath(lyingKID), SHA256: hex.EncodeToString(sum[:])}},
	}, "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePublisherKey(m, "acme", lyingKID, body, rootsFor(t, storeSeed)); err == nil {
		t.Fatal("a key object was honoured under a kid it does not hash to")
	}
}

// TestTheStatementIsOrderIndependent: two writers emitting the same objects in
// a different order produce the same signature, so a manifest rewritten by a
// mirror or a re-serialisation does not become unverifiable.
func TestTheStatementIsOrderIndependent(t *testing.T) {
	a := PublisherManifest{ID: "acme", Objects: []RegistryObject{
		{Path: "profile.json", SHA256: strings.Repeat("1", 64)},
		{Path: "keys/x.json", SHA256: strings.Repeat("2", 64)},
	}}
	b := PublisherManifest{ID: "acme", Objects: []RegistryObject{
		{Path: "keys/x.json", SHA256: strings.Repeat("2", 64)},
		{Path: "profile.json", SHA256: strings.Repeat("1", 64)},
	}}
	if string(PublisherManifestStatement(a)) != string(PublisherManifestStatement(b)) {
		t.Fatal("the signed statement depends on the order objects were listed in")
	}
	// But it does depend on the CONTENT, or a swapped hash would go unnoticed.
	c := b
	c.Objects = append([]RegistryObject{}, b.Objects...)
	c.Objects[0].SHA256 = strings.Repeat("3", 64)
	if string(PublisherManifestStatement(b)) == string(PublisherManifestStatement(c)) {
		t.Fatal("changing an object hash did not change the statement")
	}
}
