package attest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func seed(t *testing.T) []byte {
	t.Helper()
	s := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func digestOf(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func rootsFor(t *testing.T, s []byte) Roots {
	t.Helper()
	pub := ed25519.NewKeyFromSeed(s).Public().(ed25519.PublicKey)
	return Roots{KID(pub): pub}
}

// TestTheHappyChain: publisher signs, Store countersigns, a host with the
// Store root proves store provenance from that one key.
func TestTheHappyChain(t *testing.T) {
	pubSeed, storeSeed := seed(t), seed(t)
	sha := digestOf("package bytes")

	env, err := SignPublisher(pubSeed, "acme", sha)
	if err != nil {
		t.Fatal(err)
	}
	if env.Declares() != FromUpload {
		t.Fatalf("a publisher-signed envelope claims %q", env.Declares())
	}
	signed, err := Countersign(storeSeed, env, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if signed.Declares() != FromStore {
		t.Fatalf("a countersigned envelope claims %q", signed.Declares())
	}
	got, err := Verify(signed, sha, rootsFor(t, storeSeed))
	if err != nil || got != FromStore {
		t.Fatalf("Verify = (%v, %v), want store provenance", got, err)
	}
}

// TestPublisherOnlyProvesUploadNotStore is the distinction the whole design
// rests on: a package signed by its developer alone is an upload, and a host
// must not read it as having come from the Store.
func TestPublisherOnlyProvesUploadNotStore(t *testing.T) {
	pubSeed, storeSeed := seed(t), seed(t)
	sha := digestOf("package bytes")
	env, err := SignPublisher(pubSeed, "acme", sha)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(env, sha, rootsFor(t, storeSeed))
	if err != nil {
		t.Fatalf("a publisher-only envelope must verify as far as it goes: %v", err)
	}
	if got != FromUpload {
		t.Fatalf("Verify = %q, want upload", got)
	}
}

// TestAnUnknownStoreRootIsRefused: a countersignature from a key this host does
// not embed proves nothing, and must not be silently downgraded to "fine".
func TestAnUnknownStoreRootIsRefused(t *testing.T) {
	pubSeed, storeSeed, strangerSeed := seed(t), seed(t), seed(t)
	sha := digestOf("package bytes")
	env, _ := SignPublisher(pubSeed, "acme", sha)
	signed, err := Countersign(storeSeed, env, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(signed, sha, rootsFor(t, strangerSeed))
	if !errors.Is(err, ErrUntrustedRoot) {
		t.Fatalf("Verify = (%v, %v), want ErrUntrustedRoot", got, err)
	}
	if got == FromStore {
		t.Fatal("an untrusted countersignature was accepted as store provenance")
	}
}

// TestTamperedPayloadIsRefused: the envelope describes the bytes, so a host
// that has different bytes must refuse before it looks at any signature.
func TestTamperedPayloadIsRefused(t *testing.T) {
	env, _ := SignPublisher(seed(t), "acme", digestOf("package bytes"))
	if _, err := Verify(env, digestOf("other bytes"), nil); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Verify = %v, want ErrDigestMismatch", err)
	}
}

// TestASignatureCannotBeMovedBetweenPackagesOrPublishers: every field that
// decides what a signature MEANS is inside the signed statement, so editing the
// JSON around it invalidates it.
func TestASignatureCannotBeMovedBetweenPackagesOrPublishers(t *testing.T) {
	sha := digestOf("package bytes")
	env, _ := SignPublisher(seed(t), "acme", sha)

	moved := env
	moved.Publisher = &PublisherSignature{
		ID: "evilcorp", KID: env.Publisher.KID,
		PublicKey: env.Publisher.PublicKey, Signature: env.Publisher.Signature,
	}
	if _, err := Verify(moved, sha, nil); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a signature was accepted under a different publisher id: %v", err)
	}

	relabelled := env
	relabelled.SHA256 = digestOf("other bytes")
	if _, err := Verify(relabelled, "", nil); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a signature was accepted over a different digest: %v", err)
	}
}

// TestAPublisherSignatureIsNotAStoreCountersignature is what the domain
// prefixes are for. Without them the same bytes would satisfy both statements,
// and any publisher could mint Store provenance for their own package.
func TestAPublisherSignatureIsNotAStoreCountersignature(t *testing.T) {
	pubSeed := seed(t)
	sha := digestOf("package bytes")
	env, _ := SignPublisher(pubSeed, "acme", sha)

	// The publisher, holding only their own key, tries to also be the Store.
	forged := env
	forged.Store = &StoreSignature{
		KID:       env.Publisher.KID,
		Signature: env.Publisher.Signature,
		IssuedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	pub := ed25519.NewKeyFromSeed(pubSeed).Public().(ed25519.PublicKey)
	// Even with the publisher's own key WRONGLY trusted as a Store root, the
	// replayed signature must not verify, because it signs a different
	// statement.
	got, err := Verify(forged, sha, Roots{KID(pub): pub})
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a publisher signature was replayed as a store countersignature: (%v, %v)", got, err)
	}
	if got == FromStore {
		t.Fatal("forged store provenance was accepted")
	}
}

// TestKIDMustMatchTheKeyItShips: the envelope carries its own public key, so
// the kid is a claim about that key and is checked rather than believed.
func TestKIDMustMatchTheKeyItShips(t *testing.T) {
	env, _ := SignPublisher(seed(t), "acme", digestOf("package bytes"))
	env.Publisher.KID = strings.Repeat("a", 64)
	if _, err := Verify(env, "", nil); err == nil || !strings.Contains(err.Error(), "kid") {
		t.Fatalf("a mismatched kid was accepted: %v", err)
	}
}

// TestCountersignRefusesWhatItHasNotChecked: the Store verifies the publisher
// half before adding its own, or its root becomes a rubber stamp.
func TestCountersignRefusesWhatItHasNotChecked(t *testing.T) {
	sha := digestOf("package bytes")
	env, _ := SignPublisher(seed(t), "acme", sha)
	env.Publisher.Signature = base64.StdEncoding.EncodeToString([]byte("not a signature"))
	if _, err := Countersign(seed(t), env, time.Now()); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("the Store countersigned an invalid publisher signature: %v", err)
	}
	if _, err := Countersign(seed(t), Envelope{V: EnvelopeVersion, SHA256: sha}, time.Now()); !errors.Is(err, ErrUnsigned) {
		t.Fatal("the Store countersigned an unsigned package")
	}
}

// TestUnsignedIsNamedRatherThanGuessed: a package with no publisher signature
// is dev provenance and says so, instead of failing with something vague.
func TestUnsignedIsNamedRatherThanGuessed(t *testing.T) {
	got, err := Verify(Envelope{V: EnvelopeVersion, SHA256: digestOf("x")}, "", nil)
	if !errors.Is(err, ErrUnsigned) || got != FromDev {
		t.Fatalf("Verify = (%v, %v), want dev/ErrUnsigned", got, err)
	}
}

// TestLoadRootsChecksEveryKID: a mistyped root must fail at load, not trust
// nothing silently at verify time.
func TestLoadRootsChecksEveryKID(t *testing.T) {
	s := seed(t)
	pub := ed25519.NewKeyFromSeed(s).Public().(ed25519.PublicKey)
	good := `{"roots":[{"kid":"` + KID(pub) + `","publicKey":"` + base64.StdEncoding.EncodeToString(pub) + `"}]}`
	roots, err := LoadRoots(strings.NewReader(good))
	if err != nil || len(roots) != 1 {
		t.Fatalf("LoadRoots = (%v, %v)", roots, err)
	}
	bad := `{"roots":[{"kid":"` + strings.Repeat("b", 64) + `","publicKey":"` + base64.StdEncoding.EncodeToString(pub) + `"}]}`
	if _, err := LoadRoots(strings.NewReader(bad)); err == nil {
		t.Fatal("a root whose kid does not match its key was loaded")
	}
	if _, err := LoadRoots(strings.NewReader(`{"roots":[{"kid":"x","publicKey":"nope"}]}`)); err == nil {
		t.Fatal("a root with an unusable key was loaded")
	}
}

// TestSigningIsDeterministic: ed25519 is deterministic, and the statement is
// built by this package rather than the caller, so the same inputs sign to the
// same bytes. A packer can therefore prove two builds produced one package.
func TestSigningIsDeterministic(t *testing.T) {
	s := seed(t)
	sha := digestOf("package bytes")
	first, err := SignPublisher(s, "acme", sha)
	if err != nil {
		t.Fatal(err)
	}
	// Case and padding in the digest must not change the signature either.
	second, err := SignPublisher(s, " acme ", "  "+strings.ToUpper(sha)+"  ")
	if err != nil {
		t.Fatal(err)
	}
	if first.Publisher.Signature != second.Publisher.Signature {
		t.Fatal("the same package signed to two different signatures")
	}
}

// TestTheDomainsAreDistinct tests the domain separation directly, because the
// replay test above does not: the two statements differ in shape as well, so
// that test passes even when both domains are identical (injected and
// confirmed). This one fails the moment they collapse.
func TestTheDomainsAreDistinct(t *testing.T) {
	if publisherDomain == storeDomain {
		t.Fatal("the publisher and store statements share a domain; a signature for one could be replayed as the other the moment their shapes coincide")
	}
	sha := digestOf("package bytes")
	env, _ := SignPublisher(seed(t), "acme", sha)
	pubStmt := string(PublisherStatement("acme", sha))
	storeStmt := string(StoreStatement(env))
	if strings.HasPrefix(storeStmt, publisherDomain+"\n") || strings.HasPrefix(pubStmt, storeDomain+"\n") {
		t.Fatal("one statement starts with the other's domain")
	}
	if pubStmt == storeStmt {
		t.Fatal("the two statements are identical for the same package")
	}
}
