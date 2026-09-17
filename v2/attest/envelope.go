// Package attest is the attestation that travels inside a .bdx package: who
// built it, who published it, and over what bytes.
//
// This file is the DATA half, and it is deliberately separate from the crypto.
// A package is written, moved, listed and inspected far more often than it is
// verified, and every one of those readers needs the envelope's shape without
// needing an ed25519 implementation. Signing, countersigning and verification
// against a root set are TM-401's and land in this same package, over these
// same types — there is one envelope, not one per consumer.
package attest

import (
	"crypto/sha256"
	"encoding/hex"
)

// EnvelopeVersion is the envelope schema this package writes. It is separate
// from the manifest contract: the envelope describes the package, the contract
// describes the manifest inside it, and they version independently.
const EnvelopeVersion = 1

// Envelope is attestation.json inside a .bdx. SHA256 is over package.tar.gz
// alone — the payload — so the signatures stay valid however the outer
// container is rewritten, and a host can verify before it extracts anything.
type Envelope struct {
	V      int    `json:"v"`
	SHA256 string `json:"sha256"`

	// Publisher is the developer's signature over the payload. Absent means
	// unsigned: a dev-tier package, never an installable one outside a host
	// that was explicitly told to allow it.
	Publisher *PublisherSignature `json:"publisher,omitempty"`

	// Store is the Store's countersignature over the publisher's. Absent
	// means the package never went through a Store — a direct upload, whose
	// publisher key the host resolves through the publisher registry.
	Store *StoreSignature `json:"store,omitempty"`
}

// PublisherSignature carries the key that made it, so a host can verify the
// signature before it has decided whether to trust the key. Trust is a
// separate question, answered by the registry and the Store root.
type PublisherSignature struct {
	ID        string `json:"id"`
	KID       string `json:"kid"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
}

// StoreSignature is the Store's countersignature. It carries no public key:
// the roots a host trusts are embedded per Store origin, and a key that
// arrived with the thing it signs proves nothing.
type StoreSignature struct {
	KID       string `json:"kid"`
	Signature string `json:"signature"`
	IssuedAt  string `json:"issuedAt"`
}

// Provenance is where a package came from. The host WRITES it after verifying,
// and it is never read out of a package — see Declares.
type Provenance string

// The provenances, strongest first.
const (
	FromStore  Provenance = "store"
	FromUpload Provenance = "upload"
	FromDev    Provenance = "dev"
)

// Declares reports the provenance this envelope CLAIMS, from which signature
// slots are filled. It is not proof of anything: no signature is checked here,
// and a package can claim whatever it likes. A host calls Verify (TM-401) and
// records the result; this is for a lister, an inspector or an error message
// that has to say something about a package it has not verified.
func (e Envelope) Declares() Provenance {
	switch {
	case e.Store != nil && e.Publisher != nil:
		return FromStore
	case e.Publisher != nil:
		return FromUpload
	default:
		return FromDev
	}
}

// KID identifies a public key: the hex sha256 of its raw bytes. It is what a
// manifest's publisher.kid holds and what the registry names key objects by.
func KID(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}
