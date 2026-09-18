package attest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// TM-401 (EP-025): the crypto half of the attestation. The envelope's shape is
// in envelope.go and is deliberately usable without any of this.
//
// Two signatures, made by two parties, over two different statements:
//
//   - the PUBLISHER signs the payload digest, which says "I built these bytes".
//   - the STORE countersigns the publisher's signature, which says "I checked
//     that this publisher is who they claim and that this package is theirs".
//
// A host that trusts only the Store roots can therefore verify a whole chain
// from one embedded key, and a host taking a direct upload can verify the
// publisher half alone once it has resolved that publisher's key.
//
// The signed statements are DOMAIN-PREFIXED and newline-separated rather than
// DSSE or any other envelope format.
//
// Two things keep a signature from being replayed as the other kind, and it is
// worth being precise about which does the work. TODAY the statements also
// differ in SHAPE — three lines against five — so a publisher signature would
// fail as a countersignature even with no prefix at all; removing the domain
// separation does not currently fail a test, and pretending otherwise would be
// a comment that outruns its evidence. The prefix is there for the statement
// nobody has written yet: the moment a third statement coincides in shape with
// one of these, the domain is the only thing left separating them.
// TestTheDomainsAreDistinct pins that directly instead of relying on shape.
//
// What both statements do carry is every field that decides what the signature
// MEANS, so an attacker cannot move a valid signature onto a different package,
// publisher or key by editing the JSON around it.

// Statement domains. Changing one of these invalidates every signature made
// under it, which is the point: a v2 statement must not verify as a v1 one.
const (
	publisherDomain = "bytedesk.publisher-release.v1"
	storeDomain     = "bytedesk.store-attestation.v1"
)

// Errors a caller distinguishes. Everything else is a wrapped detail.
var (
	// ErrUnsigned means the envelope carries no publisher signature at all.
	ErrUnsigned = errors.New("attest: package is unsigned")
	// ErrUntrustedRoot means the Store countersignature was made by a key
	// this host does not embed or pin.
	ErrUntrustedRoot = errors.New("attest: store countersignature is not from a trusted root")
	// ErrBadSignature means a signature is present and does not verify.
	ErrBadSignature = errors.New("attest: signature does not verify")
	// ErrDigestMismatch means the envelope describes different bytes than the
	// ones the caller has.
	ErrDigestMismatch = errors.New("attest: payload digest does not match the envelope")
)

// PublisherStatement is what a publisher signs. It is built here rather than
// by the caller so signing and verifying can never disagree about it.
func PublisherStatement(publisherID, payloadSHA256 string) []byte {
	return []byte(publisherDomain + "\n" + strings.TrimSpace(publisherID) + "\n" + normalizeDigest(payloadSHA256))
}

// StoreStatement is what the Store countersigns: the payload, WHO published
// it, WHICH key they used and the signature itself. Including the publisher's
// signature is what makes this a counter-signature rather than a second
// independent one — a Store attestation cannot be moved onto a package signed
// by a different key.
func StoreStatement(env Envelope) []byte {
	var id, kid, sig string
	if env.Publisher != nil {
		id, kid, sig = env.Publisher.ID, env.Publisher.KID, env.Publisher.Signature
	}
	return []byte(storeDomain + "\n" + normalizeDigest(env.SHA256) + "\n" + id + "\n" + kid + "\n" + sig)
}

// SignPublisher signs the payload digest with a publisher's seed and returns
// the envelope a packer writes. The seed is the 32-byte ed25519 seed, which is
// what the developer's key material is: never the expanded private key, so a
// seed read from a file is used directly.
func SignPublisher(seed []byte, publisherID, payloadSHA256 string) (Envelope, error) {
	if len(seed) != ed25519.SeedSize {
		return Envelope{}, fmt.Errorf("attest: publisher seed is %d bytes, want %d", len(seed), ed25519.SeedSize)
	}
	if strings.TrimSpace(publisherID) == "" {
		return Envelope{}, errors.New("attest: publisher id required")
	}
	digest := normalizeDigest(payloadSHA256)
	if err := checkDigest(digest); err != nil {
		return Envelope{}, err
	}
	key := ed25519.NewKeyFromSeed(seed)
	pub := key.Public().(ed25519.PublicKey)
	return Envelope{
		V:      EnvelopeVersion,
		SHA256: digest,
		Publisher: &PublisherSignature{
			ID:        strings.TrimSpace(publisherID),
			KID:       KID(pub),
			PublicKey: base64.StdEncoding.EncodeToString(pub),
			Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, PublisherStatement(publisherID, digest))),
		},
	}, nil
}

// Countersign adds the Store's signature over the publisher's. It verifies the
// publisher half FIRST: countersigning a signature the Store has not checked
// would turn the Store's root into a rubber stamp, which is precisely the
// property the two-signature design exists to avoid.
func Countersign(seed []byte, env Envelope, at time.Time) (Envelope, error) {
	if len(seed) != ed25519.SeedSize {
		return Envelope{}, fmt.Errorf("attest: store seed is %d bytes, want %d", len(seed), ed25519.SeedSize)
	}
	if env.Publisher == nil {
		return Envelope{}, ErrUnsigned
	}
	if err := verifyPublisher(env); err != nil {
		return Envelope{}, err
	}
	key := ed25519.NewKeyFromSeed(seed)
	pub := key.Public().(ed25519.PublicKey)
	out := env
	out.Store = &StoreSignature{
		KID:       KID(pub),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, StoreStatement(env))),
		IssuedAt:  at.UTC().Format(time.RFC3339),
	}
	return out, nil
}

// Roots are the Store public keys a host trusts, by kid. A host embeds these
// per Store origin; an operator pin replaces them.
type Roots map[string]ed25519.PublicKey

// LoadRoots reads a roots document: {"roots":[{"kid","publicKey"}]}, the same
// shape the gateway embeds. Each kid is checked against its key, so a mistyped
// entry fails at load rather than silently trusting nothing.
func LoadRoots(r io.Reader) (Roots, error) {
	var doc struct {
		Roots []struct {
			KID       string `json:"kid"`
			PublicKey string `json:"publicKey"`
		} `json:"roots"`
	}
	if err := decodeJSON(r, &doc); err != nil {
		return nil, fmt.Errorf("attest: roots: %w", err)
	}
	out := Roots{}
	for _, root := range doc.Roots {
		pub, err := decodePublicKey(root.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("attest: root %q: %w", root.KID, err)
		}
		if got := KID(pub); !strings.EqualFold(got, root.KID) {
			return nil, fmt.Errorf("attest: root kid %q is not sha256 of its public key (%q)", root.KID, got)
		}
		out[strings.ToLower(root.KID)] = pub
	}
	return out, nil
}

// Verify checks an envelope against a payload digest and a set of trusted Store
// roots, and reports the provenance it PROVED — never what the envelope claims.
//
// With roots supplied, a Store countersignature must verify against one of
// them: that is the store-provenance path. With no roots, only the publisher
// half is checked, which is the direct-upload path where the host has resolved
// the publisher's key some other way (the registry) and is asking a narrower
// question.
func Verify(env Envelope, payloadSHA256 string, roots Roots) (Provenance, error) {
	if digest := normalizeDigest(payloadSHA256); digest != "" && digest != normalizeDigest(env.SHA256) {
		return FromDev, fmt.Errorf("%w: have %s, envelope says %s", ErrDigestMismatch, digest, env.SHA256)
	}
	if env.Publisher == nil {
		return FromDev, ErrUnsigned
	}
	if err := verifyPublisher(env); err != nil {
		return FromDev, err
	}
	if env.Store == nil {
		return FromUpload, nil
	}
	root, ok := roots[strings.ToLower(env.Store.KID)]
	if !ok {
		return FromUpload, fmt.Errorf("%w: kid %s", ErrUntrustedRoot, env.Store.KID)
	}
	sig, err := base64.StdEncoding.DecodeString(env.Store.Signature)
	if err != nil {
		return FromUpload, fmt.Errorf("%w: store signature is not base64: %v", ErrBadSignature, err)
	}
	if !ed25519.Verify(root, StoreStatement(env), sig) {
		return FromUpload, fmt.Errorf("%w: store countersignature", ErrBadSignature)
	}
	return FromStore, nil
}

// verifyPublisher checks the publisher signature against the key IN the
// envelope. That proves the bytes were signed by whoever holds that key; it
// does NOT prove the key belongs to the named publisher. Binding key to
// publisher is the registry's job (TM-410) or the Store's countersignature.
func verifyPublisher(env Envelope) error {
	pub, err := decodePublicKey(env.Publisher.PublicKey)
	if err != nil {
		return fmt.Errorf("attest: publisher key: %w", err)
	}
	if got := KID(pub); !strings.EqualFold(got, env.Publisher.KID) {
		return fmt.Errorf("attest: publisher kid %q is not sha256 of the key it ships (%q)", env.Publisher.KID, got)
	}
	sig, err := base64.StdEncoding.DecodeString(env.Publisher.Signature)
	if err != nil {
		return fmt.Errorf("%w: publisher signature is not base64: %v", ErrBadSignature, err)
	}
	if !ed25519.Verify(pub, PublisherStatement(env.Publisher.ID, env.SHA256), sig) {
		return fmt.Errorf("%w: publisher signature", ErrBadSignature)
	}
	return nil
}

func decodePublicKey(encoded string) (ed25519.PublicKey, error) {
	encoded = strings.TrimSpace(encoded)
	if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	if raw, err := hex.DecodeString(encoded); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	return nil, fmt.Errorf("not a %d-byte ed25519 public key in base64 or hex", ed25519.PublicKeySize)
}

// normalizeDigest lowercases and trims, so a digest that differs only in case
// is the same digest. Signing over the normalized form means a caller cannot
// produce two valid signatures for one package by changing case.
func normalizeDigest(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func checkDigest(digest string) error {
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != sha256.Size {
		return fmt.Errorf("attest: %q is not a hex sha256", digest)
	}
	return nil
}
