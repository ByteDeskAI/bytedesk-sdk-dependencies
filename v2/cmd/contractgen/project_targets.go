package main

import (
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/projectknowledge"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/projecttasks"
	"reflect"
)

func projectTasksTarget(provider bool) target {
	if provider {
		return commandTarget("projecttasks", []contractCommand{
			{"describe", "Describe", projecttasks.ProviderDescribeRequest{}, projecttasks.DescribeResult{}},
			{"changes", "Changes", projecttasks.ProviderChangesRequest{}, projecttasks.ChangesResult{}},
			{"recover", "Recover", projecttasks.ProviderRecoverRequest{}, projecttasks.RecoverResult{}},
			{"transfer", "ManageTransfer", projecttasks.ProviderTransferRequest{}, projecttasks.TransferResult{}},
			{"migration", "Migrate", projecttasks.ProviderMigrationRequest{}, projecttasks.MigrationResult{}},
			{"schema", "Schema", projecttasks.ProviderSchemaRequest{}, projecttasks.SchemaResult{}},
			{"query", "Search", projecttasks.ProviderQueryRequest{}, projecttasks.QueryResult{}},
			{"read", "Read", projecttasks.ProviderReadRequest{}, projecttasks.ReadResult{}},
			{"write", "Write", projecttasks.ProviderWriteRequest{}, projecttasks.WriteResult{}},
			{"claim", "Claim", projecttasks.ProviderClaimRequest{}, projecttasks.ClaimResult{}},
		})
	}
	t := commandTarget("projecttasks", []contractCommand{
		{projecttasks.CommandDescribe, "Describe", projecttasks.DescribeRequest{}, projecttasks.DescribeResult{}},
		{projecttasks.CommandChanges, "Changes", projecttasks.ChangesRequest{}, projecttasks.ChangesResult{}},
		{projecttasks.CommandRecover, "Recover", projecttasks.RecoverRequest{}, projecttasks.RecoverResult{}},
		{projecttasks.CommandTransfer, "ManageTransfer", projecttasks.TransferRequest{}, projecttasks.TransferResult{}},
		{projecttasks.CommandMigration, "Migrate", projecttasks.MigrationRequest{}, projecttasks.MigrationResult{}},
		{projecttasks.CommandSchema, "Schema", projecttasks.SchemaRequest{}, projecttasks.SchemaResult{}},
		{projecttasks.CommandQuery, "Search", projecttasks.QueryRequest{}, projecttasks.QueryResult{}},
		{projecttasks.CommandRead, "Read", projecttasks.ReadRequest{}, projecttasks.ReadResult{}},
		{projecttasks.CommandWrite, "Write", projecttasks.WriteRequest{}, projecttasks.WriteResult{}},
		{projecttasks.CommandClaim, "Claim", projecttasks.ClaimRequest{}, projecttasks.ClaimResult{}},
	})
	t.roots = append(t.roots,
		root{typ: reflect.TypeOf(projecttasks.ProviderDescribeRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderChangesRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderRecoverRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderTransferRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderMigrationRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderSchemaRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderQueryRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderReadRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderWriteRequest{})},
		root{typ: reflect.TypeOf(projecttasks.ProviderClaimRequest{})},
	)
	return t
}

func projectKnowledgeTarget(provider bool) target {
	if provider {
		return commandTarget("projectknowledge", []contractCommand{
			{"describe", "Describe", projectknowledge.ProviderDescribeRequest{}, projectknowledge.DescribeResult{}},
			{"changes", "Changes", projectknowledge.ProviderChangesRequest{}, projectknowledge.ChangesResult{}},
			{"recover", "Recover", projectknowledge.ProviderRecoverRequest{}, projectknowledge.RecoverResult{}},
			{"transfer", "ManageTransfer", projectknowledge.ProviderTransferRequest{}, projectknowledge.TransferResult{}},
			{"migration", "Migrate", projectknowledge.ProviderMigrationRequest{}, projectknowledge.MigrationResult{}},
			{"schema", "Schema", projectknowledge.ProviderSchemaRequest{}, projectknowledge.SchemaResult{}},
			{"query", "Search", projectknowledge.ProviderQueryRequest{}, projectknowledge.QueryResult{}},
			{"read", "Read", projectknowledge.ProviderReadRequest{}, projectknowledge.ReadResult{}},
			{"write", "Write", projectknowledge.ProviderWriteRequest{}, projectknowledge.WriteResult{}},
		})
	}
	t := commandTarget("projectknowledge", []contractCommand{
		{projectknowledge.CommandDescribe, "Describe", projectknowledge.DescribeRequest{}, projectknowledge.DescribeResult{}},
		{projectknowledge.CommandChanges, "Changes", projectknowledge.ChangesRequest{}, projectknowledge.ChangesResult{}},
		{projectknowledge.CommandRecover, "Recover", projectknowledge.RecoverRequest{}, projectknowledge.RecoverResult{}},
		{projectknowledge.CommandTransfer, "ManageTransfer", projectknowledge.TransferRequest{}, projectknowledge.TransferResult{}},
		{projectknowledge.CommandMigration, "Migrate", projectknowledge.MigrationRequest{}, projectknowledge.MigrationResult{}},
		{projectknowledge.CommandSchema, "Schema", projectknowledge.SchemaRequest{}, projectknowledge.SchemaResult{}},
		{projectknowledge.CommandQuery, "Search", projectknowledge.QueryRequest{}, projectknowledge.QueryResult{}},
		{projectknowledge.CommandRead, "Read", projectknowledge.ReadRequest{}, projectknowledge.ReadResult{}},
		{projectknowledge.CommandWrite, "Write", projectknowledge.WriteRequest{}, projectknowledge.WriteResult{}},
	})
	t.roots = append(t.roots,
		root{typ: reflect.TypeOf(projectknowledge.ProviderDescribeRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderChangesRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderRecoverRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderTransferRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderMigrationRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderSchemaRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderQueryRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderReadRequest{})},
		root{typ: reflect.TypeOf(projectknowledge.ProviderWriteRequest{})},
	)
	return t
}
