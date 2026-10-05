package projecttasks

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"

const (
	CommandDescribe  = "cmd.gateway.project-tasks.v1.describe"
	CommandChanges   = "cmd.gateway.project-tasks.v1.changes"
	CommandRecover   = "cmd.gateway.project-tasks.v1.recover"
	CommandTransfer  = "cmd.gateway.project-tasks.v1.transfer"
	CommandMigration = "cmd.gateway.project-tasks.v1.migration"
	CommandSchema    = "cmd.gateway.project-tasks.v1.schema"
	CommandQuery     = "cmd.gateway.project-tasks.v1.query"
	CommandRead      = "cmd.gateway.project-tasks.v1.read"
	CommandWrite     = "cmd.gateway.project-tasks.v1.write"
	CommandClaim     = "cmd.gateway.project-tasks.v1.claim"
)

type ProviderSchemaRequest struct {
	Invocation Invocation    `json:"invocation" bd:"subject"`
	Request    SchemaRequest `json:"request" bd:"subject"`
}

func (r ProviderSchemaRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}

type ProviderQueryRequest struct {
	Invocation Invocation   `json:"invocation" bd:"subject"`
	Request    QueryRequest `json:"request" bd:"subject"`
}

func (r ProviderQueryRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}

type ProviderReadRequest struct {
	Invocation Invocation  `json:"invocation" bd:"subject"`
	Request    ReadRequest `json:"request" bd:"subject"`
}

func (r ProviderReadRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}

type ProviderWriteRequest struct {
	Invocation Invocation   `json:"invocation" bd:"subject"`
	Request    WriteRequest `json:"request" bd:"subject"`
}

func (r ProviderWriteRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}

type ProviderClaimRequest struct {
	Invocation Invocation   `json:"invocation" bd:"subject"`
	Request    ClaimRequest `json:"request" bd:"subject"`
}

func (r ProviderClaimRequest) Validate() error {
	return contractcheck.All(contractcheck.Size(r), r.Invocation.Matches(r.Request.Scope), r.Request.Validate())
}
