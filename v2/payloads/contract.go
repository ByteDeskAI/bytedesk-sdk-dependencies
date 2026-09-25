// Package payloads defines host-owned, short-lived payload handles. The host
// binds every handle to the substrate-stamped caller and generation AND the
// resolved provider generation. A request never chooses its caller identity.
// Handles are not URLs or filesystem paths and cannot confer access to another
// caller's data. Only committed handles may be consumed. The host revokes them
// at expiry, explicit revocation, or withdrawal of either bound generation.
package payloads

import (
	"encoding/base64"
	"fmt"
	"regexp"

	check "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
)

const (
	ContractRevision  = 1
	CommandCreate     = "cmd.gateway.payloads.v1.create"
	CommandAppend     = "cmd.gateway.payloads.v1.append"
	CommandCommit     = "cmd.gateway.payloads.v1.commit"
	CommandRead       = "cmd.gateway.payloads.v1.read"
	CommandRevoke     = "cmd.gateway.payloads.v1.revoke"
	MaxAssembledBytes = 8 << 20
	// Base64 expansion leaves room for JSON and the transport envelope.
	MaxChunkBytes           = 24 << 10
	MaxInlineTextBytes      = 32 << 10
	DefaultTTLSeconds       = 300
	MaxTTLSeconds           = 600
	PurposeDecision         = "ai-decision"
	PurposeProviderRequest  = "provider-request"
	PurposeProviderResponse = "provider-response"
	PurposeCodingPrompt     = "coding-prompt"
	StateUploading          = "uploading"
	StateCommitted          = "committed"
)

// TextInput carries exactly one text source. Handle content is assembled and
// checked by the host before dispatch; it must be UTF-8 text, not arbitrary bytes.
type TextInput struct {
	Inline   string `json:"inline,omitempty" bd:"subject"`
	HandleID string `json:"handleId,omitempty" bd:"subject"`
}

type CreateRequest struct {
	// Required for provider request/response payloads; absent on ordinary
	// consumer uploads. Host scope derives from this opaque invocation only.
	InvocationID string `json:"invocationId,omitempty" bd:"subject"`
	ProviderID   string `json:"providerId" bd:"subject"`
	Purpose      string `json:"purpose" bd:"public"`
	Size         uint64 `json:"size,string" bd:"subject"`
	SHA256       string `json:"sha256" bd:"subject"`
	// Zero requests the documented default; the host may impose a lower ceiling.
	TTLSeconds uint32 `json:"ttlSeconds,omitempty" bd:"subject"`
}

type Handle struct {
	ID                 string `json:"id" bd:"subject"`
	ProviderID         string `json:"providerId" bd:"subject"`
	ProviderGeneration string `json:"providerGeneration" bd:"subject"`
	Purpose            string `json:"purpose" bd:"public"`
	Size               uint64 `json:"size,string" bd:"subject"`
	SHA256             string `json:"sha256" bd:"subject"`
	ExpiresAt          string `json:"expiresAt" bd:"subject"`
	State              string `json:"state" bd:"public"`
}

type CreateResult struct {
	Handle Handle `json:"handle" bd:"subject"`
}
type AppendRequest struct {
	InvocationID string `json:"invocationId,omitempty" bd:"subject"`
	HandleID     string `json:"handleId" bd:"subject"`
	Offset       uint64 `json:"offset,string" bd:"subject"`
	// Standard, padded base64. Append is contiguous; retrying the same offset
	// with identical bytes is idempotent; different bytes are a conflict.
	Data string `json:"data" bd:"subject"`
}
type AppendResult struct {
	NextOffset uint64 `json:"nextOffset,string" bd:"subject"`
}
type CommitRequest struct {
	InvocationID string `json:"invocationId,omitempty" bd:"subject"`
	HandleID     string `json:"handleId" bd:"subject"`
}
type CommitResult struct {
	Handle Handle `json:"handle" bd:"subject"`
}
type ReadRequest struct {
	InvocationID string `json:"invocationId,omitempty" bd:"subject"`
	HandleID     string `json:"handleId" bd:"subject"`
	Offset       uint64 `json:"offset,string" bd:"subject"`
	Limit        uint32 `json:"limit" bd:"subject"`
}
type ReadResult struct {
	Data       string `json:"data" bd:"subject"`
	NextOffset uint64 `json:"nextOffset,string" bd:"subject"`
	EOF        bool   `json:"eof" bd:"public"`
}
type RevokeRequest struct {
	InvocationID string `json:"invocationId,omitempty" bd:"subject"`
	HandleID     string `json:"handleId" bd:"subject"`
}
type RevokeResult struct {
	Revoked bool `json:"revoked" bd:"public"`
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (v TextInput) Validate() error {
	if (v.Inline == "") == (v.HandleID == "") {
		return fmt.Errorf("text requires exactly one inline or handle source")
	}
	if v.HandleID != "" {
		return check.ID("text.handleId", v.HandleID)
	}
	return check.Text("text.inline", v.Inline, MaxInlineTextBytes, true)
}
func purpose(v string) error {
	return check.Enum("payload purpose", v, PurposeDecision, PurposeProviderRequest, PurposeProviderResponse, PurposeCodingPrompt)
}
func sizeDigest(size uint64, digest string) error {
	if size == 0 || size > MaxAssembledBytes {
		return fmt.Errorf("payload size must be 1 to %d bytes", MaxAssembledBytes)
	}
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("sha256 must be lowercase hex SHA-256")
	}
	return nil
}
func (v CreateRequest) Validate() error {
	if err := check.OptionalID("invocationId", v.InvocationID); err != nil {
		return err
	}
	if (v.Purpose == PurposeProviderRequest || v.Purpose == PurposeProviderResponse) && v.InvocationID == "" {
		return fmt.Errorf("provider payload requires an invocation")
	}
	if v.TTLSeconds > MaxTTLSeconds {
		return fmt.Errorf("ttlSeconds exceeds %d", MaxTTLSeconds)
	}
	return check.All(check.ID("providerId", v.ProviderID), purpose(v.Purpose), sizeDigest(v.Size, v.SHA256), check.Size(v))
}
func (v Handle) Validate() error {
	return check.All(check.ID("handle.id", v.ID), check.ID("handle.providerId", v.ProviderID), check.ID("handle.providerGeneration", v.ProviderGeneration), purpose(v.Purpose), sizeDigest(v.Size, v.SHA256), check.Timestamp("handle.expiresAt", v.ExpiresAt), check.Enum("handle state", v.State, StateUploading, StateCommitted), check.Size(v))
}
func (v CreateResult) Validate() error {
	if v.Handle.State != StateUploading {
		return fmt.Errorf("new handle must be uploading")
	}
	return v.Handle.Validate()
}
func decode(data string) ([]byte, error) {
	if len(data) > base64.StdEncoding.EncodedLen(MaxChunkBytes) {
		return nil, fmt.Errorf("chunk exceeds %d bytes", MaxChunkBytes)
	}
	b, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil || base64.StdEncoding.EncodeToString(b) != data {
		return nil, fmt.Errorf("data must be canonical padded base64")
	}
	return b, nil
}
func (v AppendRequest) Validate() error {
	if err := check.OptionalID("invocationId", v.InvocationID); err != nil {
		return err
	}
	b, err := decode(v.Data)
	if err != nil {
		return err
	}
	if len(b) == 0 || v.Offset > MaxAssembledBytes || uint64(len(b)) > MaxAssembledBytes-v.Offset {
		return fmt.Errorf("chunk exceeds assembled payload bound")
	}
	return check.All(check.ID("handleId", v.HandleID), check.Size(v))
}
func (v AppendResult) Validate() error {
	if v.NextOffset == 0 || v.NextOffset > MaxAssembledBytes {
		return fmt.Errorf("invalid nextOffset")
	}
	return nil
}
func (v CommitRequest) Validate() error {
	return check.All(check.OptionalID("invocationId", v.InvocationID), check.ID("handleId", v.HandleID))
}
func (v CommitResult) Validate() error {
	if v.Handle.State != StateCommitted {
		return fmt.Errorf("commit requires committed handle")
	}
	return v.Handle.Validate()
}
func (v ReadRequest) Validate() error {
	if err := check.OptionalID("invocationId", v.InvocationID); err != nil {
		return err
	}
	if v.Offset > MaxAssembledBytes || v.Limit == 0 || v.Limit > MaxChunkBytes {
		return fmt.Errorf("invalid read bounds")
	}
	return check.ID("handleId", v.HandleID)
}
func (v ReadResult) Validate() error {
	b, err := decode(v.Data)
	if err != nil {
		return err
	}
	if v.NextOffset > MaxAssembledBytes || uint64(len(b)) > v.NextOffset || len(b) == 0 && !v.EOF {
		return fmt.Errorf("invalid read progress")
	}
	return check.Size(v)
}
func (v RevokeRequest) Validate() error {
	return check.All(check.OptionalID("invocationId", v.InvocationID), check.ID("handleId", v.HandleID))
}
func (v RevokeResult) Validate() error { return nil }
