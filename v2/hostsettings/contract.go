// Package hostsettings lets external settings sections opt into host-owned
// persistence. The provider advertises the versioned validate endpoint at
// host.settings.section. The host calls it only with merged, schema-filtered,
// REDACTED values. Secret input is stripped before dispatch and is never patched
// remotely. Successful validation lets the host commit its own settings store.
package hostsettings

import (
	"encoding/json"
	"fmt"
	"strings"

	check "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
)

const (
	ContractRevision = 1
	// ValidateOperation is the portable marker and endpoint operation name.
	// Generate its concrete svc.<provider>.host.settings.section.v1.validate
	// descriptor with contractgen -provider-id; never retarget another hash.
	ValidateOperation = "validate"
	CommandOwnerRead  = "cmd.gateway.settings.v1.read-owner"
)

type ValidateRequest struct {
	SectionID string `json:"sectionId" bd:"subject"`
	// Secret properties are absent, not plaintext or UI masking placeholders.
	ValuesJSON        string   `json:"valuesJson" bd:"subject"`
	ConfiguredSecrets []string `json:"configuredSecrets" bd:"subject"`
}
type FieldError struct {
	Field   string `json:"field" bd:"subject"`
	Message string `json:"message" bd:"subject"`
}
type ValidateResult struct {
	Valid  bool         `json:"valid" bd:"public"`
	Errors []FieldError `json:"errors" bd:"subject"`
}

// OwnerRead derives the section owner from the authenticated workload. It is not
// an arbitrary-plugin read primitive. Only nonsecret merged values and secret
// presence are returned; resolving the credential requires provideraccess.
type OwnerReadRequest struct {
	SectionID string `json:"sectionId" bd:"subject"`
}
type OwnerReadResult struct {
	SectionID         string   `json:"sectionId" bd:"subject"`
	ValuesJSON        string   `json:"valuesJson" bd:"subject"`
	ConfiguredSecrets []string `json:"configuredSecrets" bd:"subject"`
}

func valuesJSON(value string) error {
	trimmed := strings.TrimSpace(value)
	if len(value) > 32768 || !json.Valid([]byte(value)) || len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("settings values must be a JSON object of at most 32768 bytes")
	}
	return nil
}
func slots(values []string) error {
	if err := check.Count("configured secrets", len(values), 0, 64); err != nil {
		return err
	}
	for _, v := range values {
		if err := check.ID("secret slot", v); err != nil {
			return err
		}
	}
	return check.Unique("secret slot", values)
}
func (v ValidateRequest) Validate() error {
	return check.All(check.ID("sectionId", v.SectionID), valuesJSON(v.ValuesJSON), slots(v.ConfiguredSecrets), check.Size(v))
}
func (v FieldError) Validate() error {
	return check.All(check.Text("error.field", v.Field, 255, true), check.Text("error.message", v.Message, 1024, true))
}
func (v ValidateResult) Validate() error {
	if v.Valid != (len(v.Errors) == 0) {
		return fmt.Errorf("valid settings must have no errors; invalid settings require an error")
	}
	if err := check.Count("errors", len(v.Errors), 0, 64); err != nil {
		return err
	}
	for _, e := range v.Errors {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	return check.Size(v)
}
func (v OwnerReadRequest) Validate() error { return check.ID("sectionId", v.SectionID) }
func (v OwnerReadResult) Validate() error {
	return check.All(check.ID("sectionId", v.SectionID), valuesJSON(v.ValuesJSON), slots(v.ConfiguredSecrets), check.Size(v))
}
