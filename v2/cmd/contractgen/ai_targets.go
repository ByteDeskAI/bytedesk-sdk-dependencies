package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"reflect"
	"regexp"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/aidecision"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/codingsessions"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/hostsettings"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/payloads"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/provideraccess"
)

type contractCommand struct {
	name, goVar string
	req, resp   any
}

func commandTarget(pkg string, commands []contractCommand) target {
	t := target{pkg: pkg, published: true}
	for _, c := range commands {
		req, resp := reflect.TypeOf(c.req), reflect.TypeOf(c.resp)
		t.roots = append(t.roots, root{typ: req}, root{typ: resp})
		t.ops = append(t.ops, operation{name: c.name, kind: kindCommand, rev: 1, goVar: c.goVar, req: req, resp: resp, subject: c.name, validated: true})
	}
	return t
}
func aiDecisionTarget() target {
	t := commandTarget("aidecision", []contractCommand{
		{aidecision.CommandStart, "Start", aidecision.BatchRequest{}, aidecision.StartResult{}},
		{aidecision.CommandRead, "Read", aidecision.ReadRequest{}, aidecision.ReadResult{}},
		{aidecision.CommandCancel, "Cancel", aidecision.CancelRequest{}, aidecision.CancelResult{}},
		{aidecision.CommandModels, "Models", aidecision.ModelsRequest{}, aidecision.ModelsResult{}},
	})
	t.roots = append(t.roots, root{typ: reflect.TypeOf(aidecision.ProviderStartRequest{})}, root{typ: reflect.TypeOf(aidecision.ProviderReadRequest{})}, root{typ: reflect.TypeOf(aidecision.ProviderCancelRequest{})}, root{typ: reflect.TypeOf(aidecision.ProviderModelsRequest{})})
	return t
}
func payloadsTarget() target {
	return commandTarget("payloads", []contractCommand{
		{payloads.CommandCreate, "Create", payloads.CreateRequest{}, payloads.CreateResult{}},
		{payloads.CommandAppend, "Append", payloads.AppendRequest{}, payloads.AppendResult{}},
		{payloads.CommandCommit, "Commit", payloads.CommitRequest{}, payloads.CommitResult{}},
		{payloads.CommandRead, "Read", payloads.ReadRequest{}, payloads.ReadResult{}},
		{payloads.CommandRevoke, "Revoke", payloads.RevokeRequest{}, payloads.RevokeResult{}},
	})
}
func providerAccessTarget() target {
	return commandTarget("provideraccess", []contractCommand{
		{provideraccess.CommandCredential, "Credential", provideraccess.CredentialRequest{}, provideraccess.CredentialResult{}},
		{provideraccess.CommandRevoke, "Revoke", provideraccess.RevokeRequest{}, provideraccess.RevokeResult{}},
		{provideraccess.CommandEgressStart, "EgressStart", provideraccess.EgressRequest{}, provideraccess.EgressStartResult{}},
		{provideraccess.CommandEgressRead, "EgressRead", provideraccess.EgressReadRequest{}, provideraccess.EgressReadResult{}},
		{provideraccess.CommandEgressCancel, "EgressCancel", provideraccess.EgressCancelRequest{}, provideraccess.EgressCancelResult{}},
	})
}
func hostSettingsTarget() target {
	t := commandTarget("hostsettings", []contractCommand{{hostsettings.CommandOwnerRead, "OwnerRead", hostsettings.OwnerReadRequest{}, hostsettings.OwnerReadResult{}}})
	t.roots = append(t.roots, root{typ: reflect.TypeOf(hostsettings.ValidateRequest{})}, root{typ: reflect.TypeOf(hostsettings.ValidateResult{})})
	return t
}
func codingSessionsTarget() target {
	t := commandTarget("codingsessions", []contractCommand{
		{codingsessions.CommandCatalog, "Catalog", codingsessions.CatalogRequest{}, codingsessions.CatalogResult{}},
		{codingsessions.CommandPreview, "Preview", codingsessions.PreviewRequest{}, codingsessions.PreviewJobResult{}},
		{codingsessions.CommandPreviewRead, "PreviewRead", codingsessions.PreviewJobRequest{}, codingsessions.PreviewJobResult{}},
		{codingsessions.CommandPreviewCancel, "PreviewCancel", codingsessions.PreviewJobRequest{}, codingsessions.PreviewJobResult{}},
		{codingsessions.CommandCreate, "Create", codingsessions.CreateRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandRead, "Read", codingsessions.SessionRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandRecover, "Recover", codingsessions.SessionRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandList, "List", codingsessions.ListRequest{}, codingsessions.ListResult{}},
		{codingsessions.CommandPrompt, "Prompt", codingsessions.PromptRequest{}, codingsessions.PromptResult{}},
		{codingsessions.CommandStop, "Stop", codingsessions.StopRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandEnd, "End", codingsessions.SessionRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandComplete, "Complete", codingsessions.SessionRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandNewTask, "NewTask", codingsessions.NewTaskRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandPreferences, "UpdatePreferences", codingsessions.PreferencesRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandApprove, "Approve", codingsessions.ApproveRequest{}, codingsessions.SessionResult{}},
		{codingsessions.CommandEvents, "Events", codingsessions.EventsRequest{}, codingsessions.EventsResult{}},
		{codingsessions.CommandOpenSurface, "OpenSurface", codingsessions.OpenSurfaceRequest{}, codingsessions.OpenSurfaceResult{}},
	})
	e := reflect.TypeOf(codingsessions.ChangedEvent{})
	t.roots = append(t.roots, root{typ: e})
	t.ops = append(t.ops, operation{name: codingsessions.EventChanged, kind: kindEvent, rev: 1, goVar: "Changed", req: e, subject: codingsessions.EventChanged})
	return t
}

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// providerTarget binds canonical DTOs to concrete provider addresses BEFORE
// hashing. No descriptor is retargeted, and no plugin hashes schemas at runtime.
func providerTarget(pkg, id string) (target, error) {
	if !providerIDPattern.MatchString(id) || id == "gateway" || id == "host" {
		return target{}, fmt.Errorf("invalid or reserved provider id")
	}
	var t target
	var point string
	switch pkg {
	case "aidecision":
		t = commandTarget("aidecision", []contractCommand{
			{"start", "Start", aidecision.ProviderStartRequest{}, aidecision.StartResult{}},
			{"read", "Read", aidecision.ProviderReadRequest{}, aidecision.ReadResult{}},
			{"cancel", "Cancel", aidecision.ProviderCancelRequest{}, aidecision.CancelResult{}},
			{"models", "Models", aidecision.ProviderModelsRequest{}, aidecision.ModelsResult{}},
		})
		point = "ai.decision"
	case "hostsettings":
		t = commandTarget(pkg, []contractCommand{{"validate", "Validate", hostsettings.ValidateRequest{}, hostsettings.ValidateResult{}}})
		point = "host.settings.section"
	default:
		return target{}, fmt.Errorf("provider generation supports aidecision or hostsettings")
	}
	for i := range t.ops {
		op := &t.ops[i]
		suffix := op.goVar
		switch suffix {
		case "Start":
			suffix = "start"
		case "Read":
			suffix = "read"
		case "Cancel":
			suffix = "cancel"
		case "Models":
			suffix = "models"
		case "Validate":
			suffix = "validate"
		}
		op.name = "svc." + id + "." + point + ".v1." + suffix
		op.subject = op.name
	}
	return t, nil
}
func generateProvider(emit, pkg, id, goPackage string) ([]byte, error) {
	t, err := providerTarget(pkg, id)
	if err != nil {
		return nil, err
	}
	ms, err := buildModels(t.roots)
	if err != nil {
		return nil, err
	}
	if emit == "json" {
		entries := map[string]map[string]schemaEntry{}
		if err := collectSchemas(entries, ms, t.ops); err != nil {
			return nil, err
		}
		return renderSidecar(entries)
	}
	if emit == "descriptors-js" {
		return emitDescriptorsJS(t, ms)
	}
	if emit != "go" {
		return nil, fmt.Errorf("provider-id requires go, json or descriptors-js output")
	}
	if !token.IsIdentifier(goPackage) || token.Lookup(goPackage).IsKeyword() || goPackage == "_" {
		return nil, fmt.Errorf("invalid Go package")
	}
	digests, err := hashes(ms, t.ops)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n// Regenerate: contractgen -package=%s -provider-id=%s -go-package=%s -emit=go -out <path>\npackage %s\nimport (\ncontract %q\n%q\n)\n", generatedHeader, pkg, id, goPackage, goPackage, "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/"+pkg, "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin")
	for _, op := range t.ops {
		fmt.Fprintf(&out, "var %s = plugin.NewValidatedCommand[contract.%s, contract.%s](%q, %d, %q, %q)\n", op.goVar, op.req.Name(), op.resp.Name(), op.name, op.rev, digests[op.goVar], op.subject)
	}
	return format.Source(out.Bytes())
}
