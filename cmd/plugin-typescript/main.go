// Command plugin-typescript emits the browser's types, each package's Payload
// union and typed wrappers, and the schema-id sidecar from the canonical Go
// JSON model. It uses only the standard library and never requires npm/network.
//
// Regenerate everything:
//
//	go run ./cmd/plugin-typescript -out typescript/contracts.d.ts
//	go run ./cmd/plugin-typescript -emit=json -out typescript/schemas.json
//	go run ./cmd/plugin-typescript -emit=js -out typescript/validators.js
//	go run ./cmd/plugin-typescript -emit=go -out plugin/payload_gen.go
//	go run ./cmd/plugin-typescript -emit=go -package=consumer -out cmd/plugin-typescript/consumer/payload_gen.go
//	go run ./cmd/plugin-typescript -emit=go -package=messaging -out messaging/payload_gen.go
//
// With no arguments it writes the plugin package's .d.ts to stdout, which is the
// form the gateway plugin SDK's drift test compares against.
package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/cmd/plugin-typescript/consumer"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/messaging"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
)

// target is one package the generator emits for. A union is closed to the
// package that declares it, so every consumer package needs its own union, its
// own wrappers and its own entry here (ADR 0025 §7 and the frozen cross-package
// mechanism). plugin is the only one whose wrappers are hand-written, because
// they are the host-exposed contract.
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
		"plugin": {
			pkg:       "plugin",
			published: true,
			// readonly marks the roots whose whole reachable subgraph the
			// browser treats as immutable; it replaces the name prefix the
			// emitter used to guess from, so a renamed presentation type keeps
			// its contract and an unrelated type named Presentation* does not
			// acquire one.
			roots: []root{
				{typ: reflect.TypeOf(plugin.Manifest{})},
				{typ: reflect.TypeOf(plugin.RuntimeSnapshot{})},
				{typ: reflect.TypeOf(plugin.HostCapabilities{})},
				{typ: reflect.TypeOf(plugin.ProtocolRequirements{})},
				{typ: reflect.TypeOf(plugin.LifecycleOperation{})},
				{typ: reflect.TypeOf(plugin.PresentationRequest{}), readonly: true},
				{typ: reflect.TypeOf(plugin.PresentationResult{}), readonly: true},
			},
			// The host-exposed contract table: the operations whose request and
			// response types this SDK owns and classifies.
			ops: []operation{{
				name:  plugin.TerminalPresentationCommand,
				kind:  kindCommand,
				rev:   2,
				goVar: "CmdTerminalPresentationProject",
				req:   reflect.TypeOf(plugin.PresentationRequest{}),
				resp:  reflect.TypeOf(plugin.PresentationResult{}),
			}},
		},
		"messaging": messagingTarget(),
		"consumer": {
			pkg: "consumer",
			roots: []root{
				{typ: reflect.TypeOf(consumer.Ping{})},
				{typ: reflect.TypeOf(consumer.Pong{})},
			},
			ops: []operation{{
				name:  "cmd.consumer.v1.ping",
				kind:  kindCommand,
				rev:   1,
				goVar: "PingCommand",
				req:   reflect.TypeOf(consumer.Ping{}),
				resp:  reflect.TypeOf(consumer.Pong{}),
			}, {
				name:  "event.consumer.v1.ponged",
				kind:  kindEvent,
				rev:   1,
				goVar: "PongedEvent",
				req:   reflect.TypeOf(consumer.Pong{}),
			}},
		},
	}
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
	// A classified payload type the package publishes but no operation carries.
	// It is in the union because membership is API surface, not because it is
	// reachable from an operation.
	t.roots = append(t.roots, root{typ: reflect.TypeOf(messaging.IdempotencyRecord{})})
	for _, c := range commands {
		req, resp := reflect.TypeOf(c.req), reflect.TypeOf(c.resp)
		t.roots = append(t.roots, root{typ: req}, root{typ: resp})
		t.ops = append(t.ops, operation{
			name: c.name, kind: kindCommand, rev: messaging.ContractRevision,
			goVar: c.goVar, req: req, resp: resp,
		})
	}
	changed := reflect.TypeOf(messaging.ChangedEvent{})
	t.roots = append(t.roots, root{typ: changed})
	t.ops = append(t.ops, operation{
		name: messaging.EventChanged, kind: kindEvent, rev: messaging.ContractRevision,
		goVar: "Changed", req: changed,
	})
	return t
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

// declarations is the default emission: the plugin package's browser .d.ts.
func declarations() ([]byte, error) { return generate("dts", "plugin") }

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
	default:
		return nil, fmt.Errorf("unknown -emit %q (dts|js|go|json)", emit)
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
	emit := flag.String("emit", "dts", "what to emit: dts | js | go | json")
	pkg := flag.String("package", "plugin", "which package to emit for")
	flag.Parse()
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := generate(*emit, *pkg)
	if err != nil {
		fail(err)
	}
	if err := write(*out, data); err != nil {
		fail(err)
	}
}
