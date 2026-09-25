// Command contractgen emits the browser's types, each package's Payload union
// and typed wrappers, the generated descriptors, and the schema-id sidecar,
// from the canonical Go JSON model. It uses only the standard library and
// never requires npm or the network.
//
// Regenerate everything:
//
//	go run ./cmd/contractgen -out typescript/contracts.d.ts
//	go run ./cmd/contractgen -emit=json -out typescript/schemas.json
//	go run ./cmd/contractgen -emit=js -out typescript/validators.js
//	go run ./cmd/contractgen -emit=descriptors-js -out typescript/descriptors.js
//	go run ./cmd/contractgen -emit=go -package=messaging -out messaging/payload_gen.go
//	go run ./cmd/contractgen -emit=go -package=consumer -out cmd/contractgen/consumer/payload_gen.go
//
// This is v1's plugin-typescript with three changes: five operation kinds
// instead of two, the operation's ADDRESS inside the schema digest, and two
// more emitters (generated Go descriptors, and their browser counterpart).
//
// One target v1 had is absent. v1's "plugin" target carried the host-exposed
// command contracts — terminal presentation, desktop applications, tmux — whose
// request and response types the SDK owned and classified. v2/plugin declares
// none of them yet: those contracts become bus services in wave 2, and porting
// them here only to re-address them there would be work done twice. The target
// comes back when they move, and the generator needs no change to accept it.
package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/cmd/contractgen/consumer"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/messaging"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/sessioncontext"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/webapps"
)

// target is one package the generator emits for. A union is closed to the
// package that declares it, so every consumer package needs its own union, its
// own wrappers and its own entry here (ADR 0025 §7 and the frozen cross-package
// mechanism).
type target struct {
	pkg string
	// published marks a target whose operations are a shipped contract and
	// therefore belong in typescript/schemas.json. The consumer example is a
	// fixture, so its operations stay out of the published sidecar.
	published bool
	roots     []root
	ops       []operation
}

func targets() map[string]target {
	return map[string]target{
		"messaging":      messagingTarget(),
		"sessioncontext": sessionContextTarget(),
		"webapps":        webAppsTarget(),
		"consumer":       consumerTarget(),
		"aidecision":     aiDecisionTarget(),
		"payloads":       payloadsTarget(),
		"provideraccess": providerAccessTarget(),
		"codingsessions": codingSessionsTarget(),
		"hostsettings":   hostSettingsTarget(),
	}
}

func webAppsTarget() target {
	type op struct {
		name, goVar string
		req, resp   any
	}
	commands := []op{
		{webapps.CommandList, "List", webapps.ListRequest{}, webapps.ListResult{}},
		{webapps.CommandCreate, "Create", webapps.CreateRequest{}, webapps.CreateResult{}},
		{webapps.CommandCreationEligibility, "CheckCreationEligibility", webapps.CreationEligibilityRequest{}, webapps.CreationEligibilityResult{}},
		{webapps.CommandConversationSend, "SendConversationMessage", webapps.ConversationSendRequest{}, webapps.ConversationSendResult{}},
		{webapps.CommandConversationApprove, "ApproveConversationRequest", webapps.ConversationApproveRequest{}, webapps.ConversationApproveResult{}},
		{webapps.CommandConversationAnswer, "AnswerConversationRequest", webapps.ConversationAnswerRequest{}, webapps.ConversationAnswerResult{}},
		{webapps.CommandRunStop, "StopRun", webapps.RunStopRequest{}, webapps.RunStopResult{}},
		{webapps.CommandServicesStart, "StartServices", webapps.ServicesStartRequest{}, webapps.ServicesStartResult{}},
		{webapps.CommandServicesStop, "StopServices", webapps.ServicesStopRequest{}, webapps.ServicesStopResult{}},
		{webapps.CommandServicesLogs, "ReadServiceLogs", webapps.ServicesLogsRequest{}, webapps.ServicesLogsResult{}},
		{webapps.CommandPreviewResolve, "ResolvePreview", webapps.PreviewResolveRequest{}, webapps.PreviewResolveResult{}},
		{webapps.CommandPreviewNavigate, "NavigatePreview", webapps.PreviewNavigateRequest{}, webapps.PreviewNavigateResult{}},
		{webapps.CommandPreviewOpenExternal, "OpenPreviewExternal", webapps.PreviewOpenExternalRequest{}, webapps.PreviewOpenExternalResult{}},
	}
	t := target{pkg: "webapps", published: true}
	for _, c := range commands {
		req, resp := reflect.TypeOf(c.req), reflect.TypeOf(c.resp)
		t.roots = append(t.roots, root{typ: req}, root{typ: resp})
		t.ops = append(t.ops, operation{name: c.name, kind: kindCommand, rev: webapps.ContractRevision, goVar: c.goVar, req: req, resp: resp, subject: c.name})
	}
	event := reflect.TypeOf(webapps.RuntimeEvent{})
	t.roots = append(t.roots, root{typ: event})
	t.ops = append(t.ops,
		operation{name: webapps.EventChanged, kind: kindEvent, rev: webapps.ContractRevision, goVar: "Changed", req: event, subject: webapps.EventChanged},
		operation{name: "stream.web-apps.v1.events", kind: kindStream, rev: webapps.ContractRevision, goVar: "Events", req: event, stream: "WEB_APPS_EVENTS", subjects: []string{webapps.EventChanged}},
	)
	return t
}

// messagingTarget is the first real consumer: its DTOs live in messaging, its
// union and wrappers are generated there, and plugin imports none of it.
func messagingTarget() target {
	type op struct {
		name, goVar string
		req, resp   any
	}
	commands := []op{
		{messaging.CommandEndpointUpsert, "UpsertEndpoint", messaging.EndpointUpsertRequest{}, messaging.EndpointUpsertResult{}},
		{messaging.CommandEndpointRemove, "RemoveEndpoint", messaging.EndpointRemoveRequest{}, messaging.EndpointRemoveResult{}},
		{messaging.CommandConversationOpen, "OpenConversation", messaging.ConversationOpenRequest{}, messaging.ConversationOpenResult{}},
		{messaging.CommandMessageSend, "SendMessage", messaging.MessageSendRequest{}, messaging.MessageSendResult{}},
		{messaging.CommandMessageList, "ListMessages", messaging.MessageListRequest{}, messaging.MessageListResult{}},
		{messaging.CommandReceiptAck, "AckReceipt", messaging.ReceiptAckRequest{}, messaging.ReceiptAckResult{}},
		{messaging.CommandDeliveryUpdate, "UpdateDelivery", messaging.DeliveryUpdateRequest{}, messaging.DeliveryUpdateResult{}},
		{messaging.CommandInboxSummary, "GetInboxSummary", messaging.InboxSummaryRequest{}, messaging.InboxSummaryResult{}},
	}
	t := target{pkg: "messaging", published: true}
	// A classified payload type the package publishes but no operation
	// carries. It is in the union because membership is API surface, not
	// because it is reachable from an operation.
	t.roots = append(t.roots, root{typ: reflect.TypeOf(messaging.IdempotencyRecord{})})
	for _, c := range commands {
		req, resp := reflect.TypeOf(c.req), reflect.TypeOf(c.resp)
		t.roots = append(t.roots, root{typ: req}, root{typ: resp})
		t.ops = append(t.ops, operation{
			name: c.name, kind: kindCommand, rev: messaging.ContractRevision,
			goVar: c.goVar, req: req, resp: resp,
			// R1: the descriptor name IS the caller subject. The event and
			// cmd namespaces are unchanged in v2 precisely so this stays
			// true and no v1 -> v2 mapping table is needed.
			subject: c.name,
		})
	}
	changed := reflect.TypeOf(messaging.ChangedEvent{})
	t.roots = append(t.roots, root{typ: changed})
	t.ops = append(t.ops, operation{
		name: messaging.EventChanged, kind: kindEvent, rev: messaging.ContractRevision,
		goVar: "Changed", req: changed, subject: messaging.EventChanged,
	})
	return t
}

// sessionContextTarget publishes the generic host-owned interaction boundary.
// The payloads contain only opaque context references and bounded derived state;
// resolving a terminal, process, route, or proxy is the serving host's job.
func sessionContextTarget() target {
	type op struct {
		name, goVar string
		req, resp   any
	}
	commands := []op{
		{sessioncontext.CommandOpen, "Open", sessioncontext.OpenRequest{}, sessioncontext.OpenResult{}},
		{sessioncontext.CommandRefresh, "Refresh", sessioncontext.RefreshRequest{}, sessioncontext.RefreshResult{}},
		{sessioncontext.CommandAction, "Action", sessioncontext.ActionRequest{}, sessioncontext.ActionResult{}},
	}
	t := target{pkg: "sessioncontext", published: true}
	for _, c := range commands {
		req, resp := reflect.TypeOf(c.req), reflect.TypeOf(c.resp)
		t.roots = append(t.roots, root{typ: req}, root{typ: resp})
		t.ops = append(t.ops, operation{
			name: c.name, kind: kindCommand, rev: sessioncontext.ContractRevision,
			goVar: c.goVar, req: req, resp: resp, subject: c.name,
		})
	}
	return t
}

// consumerTarget is the fixture. It exercises the cross-package mechanism — a
// consumer declaring its own closed union — and it is the only place all five
// kinds appear together, so the three new ones are covered by something rather
// than merely declared.
func consumerTarget() target {
	ping := reflect.TypeOf(consumer.Ping{})
	pong := reflect.TypeOf(consumer.Pong{})
	return target{
		pkg: "consumer",
		roots: []root{
			{typ: ping},
			{typ: pong},
		},
		ops: []operation{{
			name: "cmd.consumer.v1.ping", kind: kindCommand, rev: 1,
			goVar: "PingCommand", req: ping, resp: pong,
			subject: "cmd.consumer.v1.ping",
		}, {
			name: "event.consumer.v1.ponged", kind: kindEvent, rev: 1,
			goVar: "PongedEvent", req: pong,
			subject: "event.consumer.v1.ponged",
		}, {
			name: "stream.consumer.v1.pongs", kind: kindStream, rev: 1,
			goVar: "PongsStream", req: pong,
			stream: "CONSUMER_PONGS", subjects: []string{"event.consumer.v1.ponged"},
		}, {
			name: "bucket.consumer.v1.pings", kind: kindBucket, rev: 1,
			goVar: "PingsBucket", req: ping,
			bucket: "consumer_pings",
		}, {
			name: "svc.consumer.v1", kind: kindService, rev: 1,
			goVar: "ConsumerService", version: "1.0.0",
			endpoints: []endpoint{{
				name: "ping", subject: "svc.consumer.v1.ping", req: ping, resp: pong,
			}},
		}},
	}
}

func lookup(pkg string) (target, error) {
	t, ok := targets()[pkg]
	if !ok {
		names := make([]string, 0, len(targets()))
		for name := range targets() {
			names = append(names, name)
		}
		sort.Strings(names)
		return target{}, fmt.Errorf("unknown -package %q (%s)", pkg, strings.Join(names, "|"))
	}
	return t, nil
}

// declarations is the default emission: the browser .d.ts for the default
// target. v1 defaulted to the plugin package; with that target absent until
// wave 2, messaging is the shipped contract a consumer wants.
func declarations() ([]byte, error) { return generate("dts", defaultTarget) }

const defaultTarget = "messaging"

func generate(emit, pkg string) ([]byte, error) {
	// The sidecar is one document covering every published operation, so it
	// ignores -package: a per-package sidecar would leave a hash with nothing
	// to diff against, which is the whole reason the sidecar exists.
	if emit == "json" {
		return publishedSidecar()
	}
	t, err := lookup(pkg)
	if err != nil {
		return nil, err
	}
	ms, err := buildModels(t.roots)
	if err != nil {
		return nil, err
	}
	switch emit {
	case "dts":
		return emitDTS(ms), nil
	case "js":
		return emitJS(ms), nil
	case "go":
		return emitGo(t, ms)
	case "descriptors-go":
		return emitDescriptorsGo(t, ms)
	case "descriptors-js":
		return emitDescriptorsJS(t, ms)
	default:
		return nil, fmt.Errorf("unknown -emit %q (dts|js|go|json|descriptors-go|descriptors-js)", emit)
	}
}

// publishedSidecar renders schemas.json for every published target, so each
// generated hash has its canonical AST recorded beside it and a mismatch is a
// diff rather than two opaque hex strings.
func publishedSidecar() ([]byte, error) {
	all := targets()
	names := make([]string, 0, len(all))
	for name := range all {
		if all[name].published {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	entries := map[string]map[string]schemaEntry{}
	for _, name := range names {
		t := all[name]
		ms, err := buildModels(t.roots)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := collectSchemas(entries, ms, t.ops); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return renderSidecar(entries)
}

func write(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func main() {
	out := flag.String("out", "-", "output file, or - for stdout")
	emit := flag.String("emit", "dts", "what to emit: dts | js | go | json | descriptors-go | descriptors-js")
	pkg := flag.String("package", defaultTarget, "which package to emit for")
	providerID := flag.String("provider-id", "", "generate concrete provider service descriptors (aidecision or hostsettings)")
	goPackage := flag.String("go-package", "contracts", "Go package name for provider descriptor output")
	flag.Parse()
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var data []byte
	var err error
	if *providerID != "" {
		data, err = generateProvider(*emit, *pkg, *providerID, *goPackage)
	} else {
		data, err = generate(*emit, *pkg)
	}
	if err != nil {
		fail(err)
	}
	if err := write(*out, data); err != nil {
		fail(err)
	}
}
