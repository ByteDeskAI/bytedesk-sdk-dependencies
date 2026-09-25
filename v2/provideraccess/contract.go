// Package provideraccess defines the public host boundary for an enabled
// provider's credential handle and allowlisted API operations. It never carries
// API keys, URLs, headers, executable names or caller identities. Operators enter
// keys through host-owned secret settings; only the host resolves their values.
// Every operation requires the provider's current capability grant. A dependency
// does not inherit credential.secret or egress.provider from its provider.
package provideraccess

import (
	"fmt"

	check "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
)

const (
	ContractRevision    = 1
	CommandCredential   = "cmd.gateway.provider-access.v1.credential"
	CommandRevoke       = "cmd.gateway.provider-access.v1.revoke"
	CommandEgressStart  = "cmd.gateway.provider-access.v1.egress-start"
	CommandEgressRead   = "cmd.gateway.provider-access.v1.egress-read"
	CommandEgressCancel = "cmd.gateway.provider-access.v1.egress-cancel"
	OperationEvaluate   = "evaluate"
	OperationModels     = "models"
	MaxTimeoutSeconds   = 30
)

// CredentialRequest resolves this provider's administrator-configured slot.
// The host derives provider ownership from the authenticated workload. A consumer
// of the AI facade cannot obtain the provider's handle by naming its ID here.
type CredentialRequest struct {
	Slot string `json:"slot" bd:"subject"`
}
type CredentialHandle struct {
	ID         string `json:"id" bd:"subject"`
	ProviderID string `json:"providerId" bd:"subject"`
	Generation string `json:"generation" bd:"subject"`
	Slot       string `json:"slot" bd:"subject"`
}
type CredentialResult struct {
	Credential CredentialHandle `json:"credential" bd:"subject"`
}
type RevokeRequest struct {
	CredentialID string `json:"credentialId" bd:"subject"`
}
type RevokeResult struct {
	Revoked bool `json:"revoked" bd:"public"`
}

// EgressRequest names an operation in the HOST'S provider catalog. The host
// chooses method, destination and credentials, checks the payload's owner and
// provider generation, and caps both directions at payloads.MaxAssembledBytes.
// Evaluate requires a committed request body. Models has no request body.
type EgressRequest struct {
	InvocationID   string `json:"invocationId" bd:"subject"`
	CredentialID   string `json:"credentialId" bd:"subject"`
	Operation      string `json:"operation" bd:"public"`
	PayloadID      string `json:"payloadId,omitempty" bd:"subject"`
	IdempotencyKey string `json:"idempotencyKey" bd:"subject"`
	// Zero selects the host default; nonzero can only shorten its budget.
	TimeoutSeconds uint32 `json:"timeoutSeconds,omitempty" bd:"public"`
}
type EgressResult struct {
	StatusCode        uint32 `json:"statusCode" bd:"public"`
	PayloadID         string `json:"payloadId" bd:"subject"`
	RetryAfterSeconds uint32 `json:"retryAfterSeconds,omitempty" bd:"public"`
}

// EgressStart does not wait for HTTP. Each job belongs to the authenticated
// provider generation. Read is an immediate snapshot, never a long poll. Cancel
// aborts HTTP and revokes the job's request/response handles and callback scope.
// Caller disconnection is not cancellation; the host deadline always applies.
type EgressStartResult struct {
	Job EgressJob `json:"job" bd:"subject"`
}
type EgressReadRequest struct {
	InvocationID string `json:"invocationId" bd:"subject"`
	JobID        string `json:"jobId" bd:"subject"`
}
type EgressReadResult struct {
	Job EgressJob `json:"job" bd:"subject"`
}
type EgressCancelRequest struct {
	InvocationID string `json:"invocationId" bd:"subject"`
	JobID        string `json:"jobId" bd:"subject"`
}
type EgressCancelResult struct {
	Job EgressJob `json:"job" bd:"subject"`
}
type EgressFailure struct {
	Code      string `json:"code" bd:"public"`
	Message   string `json:"message" bd:"subject"`
	Retryable bool   `json:"retryable" bd:"public"`
}
type EgressJob struct {
	ID         string         `json:"id" bd:"subject"`
	State      string         `json:"state" bd:"public"`
	DeadlineAt string         `json:"deadlineAt" bd:"subject"`
	Result     *EgressResult  `json:"result,omitempty" bd:"subject"`
	Failure    *EgressFailure `json:"failure,omitempty" bd:"subject"`
}

func (v CredentialRequest) Validate() error { return check.ID("slot", v.Slot) }
func (v CredentialHandle) Validate() error {
	return check.All(check.ID("credential.id", v.ID), check.ID("credential.providerId", v.ProviderID), check.ID("credential.generation", v.Generation), check.ID("credential.slot", v.Slot))
}
func (v CredentialResult) Validate() error { return v.Credential.Validate() }
func (v RevokeRequest) Validate() error    { return check.ID("credentialId", v.CredentialID) }
func (v RevokeResult) Validate() error     { return nil }
func (v EgressRequest) Validate() error {
	if err := check.ID("invocationId", v.InvocationID); err != nil {
		return err
	}
	if v.TimeoutSeconds > MaxTimeoutSeconds {
		return fmt.Errorf("timeout exceeds host operation maximum")
	}
	if v.Operation == OperationEvaluate && v.PayloadID == "" {
		return fmt.Errorf("evaluate requires a committed payload")
	}
	if v.Operation == OperationModels && v.PayloadID != "" {
		return fmt.Errorf("models does not accept a request body")
	}
	return check.All(check.ID("credentialId", v.CredentialID), check.Enum("provider operation", v.Operation, OperationEvaluate, OperationModels), check.OptionalID("payloadId", v.PayloadID), check.ID("idempotencyKey", v.IdempotencyKey), check.Size(v))
}
func (v EgressResult) Validate() error {
	if v.StatusCode < 100 || v.StatusCode > 599 || v.RetryAfterSeconds > 86400 {
		return fmt.Errorf("invalid provider response status or retry delay")
	}
	return check.ID("payloadId", v.PayloadID)
}
func (v EgressFailure) Validate() error {
	return check.All(check.ID("failure.code", v.Code), check.Text("failure.message", v.Message, 2048, true))
}
func (v EgressJob) Validate() error {
	if err := check.All(check.ID("job.id", v.ID), check.Timestamp("job.deadlineAt", v.DeadlineAt), check.Enum("job state", v.State, "queued", "running", "completed", "failed", "cancelled", "expired")); err != nil {
		return err
	}
	if (v.Result != nil) != (v.State == "completed") || (v.Failure != nil) != (v.State == "failed") {
		return fmt.Errorf("egress result/failure does not match job state")
	}
	if v.Result != nil {
		return v.Result.Validate()
	}
	if v.Failure != nil {
		return v.Failure.Validate()
	}
	return nil
}
func (v EgressStartResult) Validate() error { return v.Job.Validate() }
func (v EgressReadRequest) Validate() error {
	return check.All(check.ID("invocationId", v.InvocationID), check.ID("jobId", v.JobID))
}
func (v EgressReadResult) Validate() error { return v.Job.Validate() }
func (v EgressCancelRequest) Validate() error {
	return check.All(check.ID("invocationId", v.InvocationID), check.ID("jobId", v.JobID))
}
func (v EgressCancelResult) Validate() error { return v.Job.Validate() }
