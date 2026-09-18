package attest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// TM-410 (EP-025): the Publisher Registry, read side.
//
// No gateway stores publisher keys as source of truth. The registry is plain
// objects in a gateway-readable bucket, one self-contained prefix per
// publisher:
//
//	publishers/<id>/manifest.json      the object list, signed by the Store root
//	publishers/<id>/profile.json       who the publisher is
//	publishers/<id>/keys/<kid>.json    one key, active or revoked; immutable
//	publishers/<id>/secrets/<kid>.age  a seed the publisher encrypted; opaque here
//
// The integrity root per publisher is manifest.json: it lists every object with
// its sha256 and is signed by the STORE root, the same root set that
// countersigns packages. So a host that trusts one embedded key can verify a
// publisher's key material without trusting the CDN, the bucket, or the
// transport — a corrupted or substituted object fails its hash, and a
// substituted manifest fails the signature.
//
// Nothing here fetches or caches. A host supplies bytes it obtained however it
// likes (HTTP with ETag, a mirror, a warm cache, a test fixture); keeping I/O
// out means this logic is identical in every product and testable without a
// network.

// Registry statement domain. See sign.go for why every statement carries one.
const publisherManifestDomain = "bytedesk.publisher-manifest.v1"

// Key states. A key is immutable once written: revocation adds a new object
// and bumps the manifest rather than editing history, so a host can always tell
// "never existed" from "existed and was revoked".
const (
	KeyActive  = "active"
	KeyRevoked = "revoked"
)

// Registry errors a caller distinguishes.
var (
	// ErrKeyNotInRegistry means the manifest does not list that kid. A host
	// refreshes once before believing it.
	ErrKeyNotInRegistry = errors.New("attest: publisher key is not in the registry")
	// ErrKeyRevoked means the key exists and must no longer be honoured.
	ErrKeyRevoked = errors.New("attest: publisher key is revoked")
	// ErrObjectDigestMismatch means an object does not match the hash the
	// signed manifest gives for it.
	ErrObjectDigestMismatch = errors.New("attest: registry object does not match its manifest digest")
)

// PublisherManifest is publishers/<id>/manifest.json.
type PublisherManifest struct {
	V         int              `json:"v"`
	ID        string           `json:"id"`
	UpdatedAt string           `json:"updatedAt,omitempty"`
	Objects   []RegistryObject `json:"objects"`
	Store     *StoreSignature  `json:"store,omitempty"`
}

// RegistryObject is one file in a publisher's prefix, with the hash that makes
// it verifiable independently of how it was fetched.
type RegistryObject struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// PublisherKey is publishers/<id>/keys/<kid>.json.
type PublisherKey struct {
	KID       string `json:"kid"`
	PublicKey string `json:"publicKey"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt,omitempty"`
	RevokedAt string `json:"revokedAt,omitempty"`
}

// PublisherManifestStatement is what the Store signs: the publisher id and the
// canonical object list. Sorting by path means the statement does not depend on
// the order a writer happened to emit, so two Stores writing the same content
// produce the same signature.
func PublisherManifestStatement(m PublisherManifest) []byte {
	objects := make([]string, 0, len(m.Objects))
	for _, o := range m.Objects {
		objects = append(objects, strings.TrimSpace(o.Path)+" "+normalizeDigest(o.SHA256))
	}
	sort.Strings(objects)
	sum := sha256.Sum256([]byte(strings.Join(objects, "\n")))
	return []byte(publisherManifestDomain + "\n" + strings.TrimSpace(m.ID) + "\n" + hex.EncodeToString(sum[:]))
}

// SignPublisherManifest is the Store's write path: it signs the object list it
// is about to publish. The gateway never calls this.
func SignPublisherManifest(seed []byte, m PublisherManifest, at string) (PublisherManifest, error) {
	if len(seed) != ed25519.SeedSize {
		return PublisherManifest{}, fmt.Errorf("attest: store seed is %d bytes, want %d", len(seed), ed25519.SeedSize)
	}
	if strings.TrimSpace(m.ID) == "" {
		return PublisherManifest{}, errors.New("attest: publisher manifest needs an id")
	}
	key := ed25519.NewKeyFromSeed(seed)
	pub := key.Public().(ed25519.PublicKey)
	out := m
	out.V = EnvelopeVersion
	out.UpdatedAt = at
	out.Store = &StoreSignature{
		KID:       KID(pub),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, PublisherManifestStatement(m))),
		IssuedAt:  at,
	}
	return out, nil
}

// VerifyPublisherManifest checks the Store's signature over the object list.
// Everything else in the registry hangs off this one check: an object is
// trusted because its hash is in a manifest a trusted root signed.
func VerifyPublisherManifest(m PublisherManifest, roots Roots) error {
	if m.Store == nil {
		return fmt.Errorf("%w: publisher manifest is unsigned", ErrUntrustedRoot)
	}
	root, ok := roots[strings.ToLower(m.Store.KID)]
	if !ok {
		return fmt.Errorf("%w: manifest kid %s", ErrUntrustedRoot, m.Store.KID)
	}
	sig, err := base64.StdEncoding.DecodeString(m.Store.Signature)
	if err != nil {
		return fmt.Errorf("%w: manifest signature is not base64: %v", ErrBadSignature, err)
	}
	if !ed25519.Verify(root, PublisherManifestStatement(m), sig) {
		return fmt.Errorf("%w: publisher manifest", ErrBadSignature)
	}
	return nil
}

// ObjectDigest returns the digest the signed manifest gives for path, or "".
func (m PublisherManifest) ObjectDigest(path string) string {
	path = strings.TrimSpace(path)
	for _, o := range m.Objects {
		if strings.TrimSpace(o.Path) == path {
			return normalizeDigest(o.SHA256)
		}
	}
	return ""
}

// VerifyObject checks bytes against the digest the manifest lists for path. A
// path the manifest does not list is refused rather than accepted unverified:
// an object nobody signed for is an object anybody could have added.
func (m PublisherManifest) VerifyObject(path string, body []byte) error {
	want := m.ObjectDigest(path)
	if want == "" {
		return fmt.Errorf("%w: %s is not listed in the manifest", ErrObjectDigestMismatch, path)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("%w: %s is %s, manifest says %s", ErrObjectDigestMismatch, path, got, want)
	}
	return nil
}

// KeyPath is where a publisher's key object lives inside their prefix.
func KeyPath(kid string) string { return "keys/" + strings.ToLower(strings.TrimSpace(kid)) + ".json" }

// ResolvePublisherKey answers the one question a host asks of the registry:
// may I honour this kid for this publisher, right now?
//
// It takes the manifest and the key object's bytes because fetching, caching
// and refreshing belong to the host — the same logic then runs against a live
// CDN, a mirror, a warm cache or a fixture. The order is deliberate: verify the
// manifest, then verify the object against it, then read the object. Reading
// first and checking later is how an unverified file ends up parsed.
func ResolvePublisherKey(m PublisherManifest, publisherID, kid string, keyObject []byte, roots Roots) (ed25519.PublicKey, error) {
	if !strings.EqualFold(strings.TrimSpace(m.ID), strings.TrimSpace(publisherID)) {
		return nil, fmt.Errorf("%w: manifest is for %q, not %q", ErrKeyNotInRegistry, m.ID, publisherID)
	}
	if err := VerifyPublisherManifest(m, roots); err != nil {
		return nil, err
	}
	path := KeyPath(kid)
	if m.ObjectDigest(path) == "" {
		return nil, fmt.Errorf("%w: %s has no %s", ErrKeyNotInRegistry, publisherID, path)
	}
	if err := m.VerifyObject(path, keyObject); err != nil {
		return nil, err
	}
	var key PublisherKey
	if err := json.Unmarshal(keyObject, &key); err != nil {
		return nil, fmt.Errorf("attest: %s: %w", path, err)
	}
	pub, err := decodePublicKey(key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("attest: %s: %w", path, err)
	}
	// The kid is derived, never believed: an object claiming a kid it does not
	// hash to would otherwise let one key masquerade as another.
	if got := KID(pub); !strings.EqualFold(got, strings.TrimSpace(kid)) || !strings.EqualFold(got, strings.TrimSpace(key.KID)) {
		return nil, fmt.Errorf("attest: %s holds key %s, not %s", path, got, kid)
	}
	if strings.ToLower(strings.TrimSpace(key.Status)) == KeyRevoked {
		return nil, fmt.Errorf("%w: %s (revoked %s)", ErrKeyRevoked, kid, key.RevokedAt)
	}
	if strings.ToLower(strings.TrimSpace(key.Status)) != KeyActive {
		return nil, fmt.Errorf("%w: %s has status %q", ErrKeyNotInRegistry, kid, key.Status)
	}
	return pub, nil
}
