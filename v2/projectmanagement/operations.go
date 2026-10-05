package projectmanagement

import (
	"fmt"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
)

type DescribeRequest struct {
	Scope Scope `json:"scope" bd:"subject"`
}
type DescribeResult struct {
	Capabilities Capabilities `json:"capabilities" bd:"public"`
	Binding      *Binding     `json:"binding,omitempty" bd:"subject"`
	Problem      *Problem     `json:"problem,omitempty" bd:"subject"`
}
type ChangesRequest struct {
	Scope Scope       `json:"scope" bd:"subject"`
	Page  PageRequest `json:"page" bd:"subject"`
}
type ChangesResult struct {
	Changes []Change `json:"changes" bd:"subject"`
	Page    PageInfo `json:"page" bd:"subject"`
	Problem *Problem `json:"problem,omitempty" bd:"subject"`
}
type RecoverRequest struct {
	Scope         Scope  `json:"scope" bd:"subject"`
	OperationID   string `json:"operationId" bd:"subject"`
	RecoveryToken string `json:"recoveryToken,omitempty" bd:"subject"`
}
type RecoverResult struct {
	Receipt Receipt           `json:"receipt" bd:"subject"`
	Offline *OfflineOperation `json:"offline,omitempty" bd:"subject"`
}
type TransferRequest struct {
	Scope      Scope       `json:"scope" bd:"subject"`
	Mutation   Mutation    `json:"mutation" bd:"subject"`
	Resource   ResourceRef `json:"resource" bd:"subject"`
	Action     string      `json:"action" bd:"public"`
	TransferID string      `json:"transferId,omitempty" bd:"subject"`
	Size       int64       `json:"size,string" bd:"public"`
	MediaType  string      `json:"mediaType" bd:"public"`
	Checksum   string      `json:"checksum" bd:"public"`
}
type TransferResult struct {
	Transfer *Transfer `json:"transfer,omitempty" bd:"subject"`
	Receipt  Receipt   `json:"receipt" bd:"subject"`
}
type MigrationRequest struct {
	Scope     Scope       `json:"scope" bd:"subject"`
	Mutation  Mutation    `json:"mutation" bd:"subject"`
	Action    string      `json:"action" bd:"public"`
	Migration Migration   `json:"migration" bd:"subject"`
	Page      PageRequest `json:"page" bd:"subject"`
}
type MigrationResult struct {
	Migration *Migration `json:"migration,omitempty" bd:"subject"`
	Receipt   Receipt    `json:"receipt" bd:"subject"`
	Page      PageInfo   `json:"page" bd:"subject"`
}

// Provider envelopes may only be sent by the host. Invocation identity and
// actor are authenticated out of band; possession of these JSON values is not a grant.
type ProviderDescribeRequest struct {
	Invocation Invocation      `json:"invocation" bd:"subject"`
	Request    DescribeRequest `json:"request" bd:"subject"`
}
type ProviderChangesRequest struct {
	Invocation Invocation     `json:"invocation" bd:"subject"`
	Request    ChangesRequest `json:"request" bd:"subject"`
}
type ProviderRecoverRequest struct {
	Invocation Invocation     `json:"invocation" bd:"subject"`
	Request    RecoverRequest `json:"request" bd:"subject"`
}
type ProviderTransferRequest struct {
	Invocation Invocation      `json:"invocation" bd:"subject"`
	Request    TransferRequest `json:"request" bd:"subject"`
}
type ProviderMigrationRequest struct {
	Invocation Invocation       `json:"invocation" bd:"subject"`
	Request    MigrationRequest `json:"request" bd:"subject"`
}

func (r DescribeRequest) Validate() error { return Validate(r) }
func (r DescribeResult) Validate() error {
	if r.Problem != nil {
		return Failure(r, r.Problem)
	}
	return Validate(r)
}
func (r ChangesRequest) Validate() error { return Validate(r) }
func (r ChangesResult) Validate() error {
	if r.Problem != nil {
		return Failure(r, r.Problem)
	}
	return Validate(r)
}
func (r RecoverRequest) Validate() error {
	return contractcheck.All(contractcheck.ID("operationId", r.OperationID), Validate(r))
}
func (r RecoverResult) Validate() error { return Validate(r) }
func (r RecoverResult) ValidateFor(q RecoverRequest) error {
	if r.Receipt.OperationID != q.OperationID {
		return fmt.Errorf("recovery returned another operation")
	}
	if r.Offline != nil && r.Offline.Scope != q.Scope {
		return fmt.Errorf("recovery returned another binding")
	}
	return r.Validate()
}
func (r TransferRequest) Validate() error {
	if r.Size < 0 {
		return fmt.Errorf("negative transfer size")
	}
	if r.Action == "complete" || r.Action == "cancel" {
		if err := contractcheck.ID("transferId", r.TransferID); err != nil {
			return err
		}
	}
	return contractcheck.All(contractcheck.Enum("transfer action", r.Action, "prepare-upload", "prepare-download", "complete", "cancel"), r.Mutation.Matches(r.Scope), required("checksum", r.Checksum), required("mediaType", r.MediaType), Validate(r))
}
func (r TransferResult) Validate() error { return Validate(r) }
func (r TransferResult) ValidateFor(q TransferRequest) error {
	return contractcheck.All(r.Receipt.ValidateFor(q.Mutation), r.Validate())
}
func (r MigrationRequest) Validate() error {
	if r.Scope != r.Migration.Source && r.Scope != r.Migration.Destination {
		return fmt.Errorf("migration invocation outside source and destination")
	}
	return contractcheck.All(contractcheck.Enum("migration action", r.Action, "plan", "export", "import", "verify", "abort"), r.Mutation.Matches(r.Scope), Validate(r))
}
func (r MigrationResult) Validate() error { return Validate(r) }
func (r MigrationResult) ValidateFor(q MigrationRequest) error {
	if r.Migration != nil && (r.Migration.ID != q.Migration.ID || r.Migration.Source != q.Migration.Source || r.Migration.Destination != q.Migration.Destination) {
		return fmt.Errorf("migration result scope mismatch")
	}
	return contractcheck.All(r.Receipt.ValidateFor(q.Mutation), r.Validate())
}
func (r ProviderDescribeRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}
func (r ProviderChangesRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}
func (r ProviderRecoverRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}
func (r ProviderTransferRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}
func (r ProviderMigrationRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}
