package projectknowledge

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"

const (
	CommandDescribe  = "cmd.gateway.project-knowledge.v1.describe"
	CommandChanges   = "cmd.gateway.project-knowledge.v1.changes"
	CommandRecover   = "cmd.gateway.project-knowledge.v1.recover"
	CommandTransfer  = "cmd.gateway.project-knowledge.v1.transfer"
	CommandMigration = "cmd.gateway.project-knowledge.v1.migration"
	CommandSchema    = "cmd.gateway.project-knowledge.v1.schema"
	CommandQuery     = "cmd.gateway.project-knowledge.v1.query"
	CommandRead      = "cmd.gateway.project-knowledge.v1.read"
	CommandWrite     = "cmd.gateway.project-knowledge.v1.write"
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
