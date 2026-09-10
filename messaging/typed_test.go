package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
)

type typedTestHost struct {
	registrar *plugin.Registrar
	requested bus.Envelope
	published []bus.Envelope
	subs      map[string][]func(bus.Envelope)
}

func newTypedTestHost() *typedTestHost {
	return &typedTestHost{subs: map[string][]func(bus.Envelope){}}
}

func (h *typedTestHost) Publish(env bus.Envelope) error {
	h.published = append(h.published, env)
	for _, fn := range h.subs[env.Type] {
		fn(env)
	}
	return nil
}

func (h *typedTestHost) Subscribe(eventType string, fn func(bus.Envelope)) func() {
	h.subs[eventType] = append(h.subs[eventType], fn)
	return func() { delete(h.subs, eventType) }
}

func (h *typedTestHost) Request(ctx context.Context, env bus.Envelope) (bus.Envelope, error) {
	h.requested = env
	if h.registrar != nil {
		return h.registrar.HandleCommand(ctx, env)
	}
	return bus.Envelope{Payload: json.RawMessage(`{}`)}, nil
}

func (*typedTestHost) Logger() plugin.Logger              { return nil }
func (*typedTestHost) StateDir(string) string             { return "" }
func (*typedTestHost) Every(time.Duration, func()) func() { return func() {} }
func (*typedTestHost) BumpContributions()                 {}

var _ plugin.Host = (*typedTestHost)(nil)

func TestGeneratedCommandDescriptorsUseCanonicalSchemaIDs(t *testing.T) {
	host := newTypedTestHost()
	assertCommandSchema(t, host, UpsertEndpoint, CommandEndpointUpsert, EndpointUpsertRequest{})
	assertCommandSchema(t, host, RemoveEndpoint, CommandEndpointRemove, EndpointRemoveRequest{})
	assertCommandSchema(t, host, OpenConversation, CommandConversationOpen, ConversationOpenRequest{})
	assertCommandSchema(t, host, SendMessage, CommandMessageSend, MessageSendRequest{})
	assertCommandSchema(t, host, ListMessages, CommandMessageList, MessageListRequest{})
	assertCommandSchema(t, host, AckReceipt, CommandReceiptAck, ReceiptAckRequest{})
	assertCommandSchema(t, host, UpdateDelivery, CommandDeliveryUpdate, DeliveryUpdateRequest{})
	assertCommandSchema(t, host, GetInboxSummary, CommandInboxSummary, InboxSummaryRequest{})
}

func assertCommandSchema[Req, Resp Payload](t *testing.T, host *typedTestHost, command Command[Req, Resp], name string, req Req) {
	t.Helper()
	if command.Name() != name {
		t.Fatalf("descriptor name = %q, want %q", command.Name(), name)
	}
	if _, err := Call(context.Background(), host, command, req); err != nil {
		t.Fatalf("Call(%s): %v", name, err)
	}
	want := operationSchemaHash("command", name, ContractRevision, reflect.TypeOf(req), reflect.TypeOf(*new(Resp)))
	if got := host.requested.Headers[plugin.HeaderSchema]; got != want {
		t.Errorf("%s schema = %q, want %q", name, got, want)
	}
}

func TestGeneratedChangedEventDescriptorUsesCanonicalSchemaID(t *testing.T) {
	host := newTypedTestHost()
	if Changed.Name() != EventChanged {
		t.Fatalf("descriptor name = %q, want %q", Changed.Name(), EventChanged)
	}
	if err := Emit(host, Changed, ChangedEvent{}); err != nil {
		t.Fatal(err)
	}
	want := operationSchemaHash("event", EventChanged, ContractRevision, reflect.TypeOf(ChangedEvent{}))
	if got := host.published[0].Headers[plugin.HeaderSchema]; got != want {
		t.Fatalf("%s schema = %q, want %q", EventChanged, got, want)
	}
}

func TestTypedCallAndHandleRoundTrip(t *testing.T) {
	host := newTypedTestHost()
	host.registrar = plugin.NewRegistrar()
	var seen plugin.Caller
	Handle(host.registrar, SendMessage, func(_ context.Context, caller plugin.Caller, req MessageSendRequest) (MessageSendResult, error) {
		seen = caller
		message := validMessage()
		message.Body = req.Body
		return MessageSendResult{Status: OperationStatus{OK: true}, Message: message}, nil
	})

	req := MessageSendRequest{
		ConversationID:   "project:gateway",
		SenderEndpointID: "endpoint-codex-1",
		Body:             "Mailbox restored.",
		Kind:             "note",
		IdempotencyKey:   "send-round-trip",
	}
	result, err := Call(context.Background(), host, SendMessage, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Message.Body != req.Body || !result.Status.OK {
		t.Fatalf("round trip result = %+v", result)
	}
	if !seen.Autonomous() {
		t.Fatalf("caller without a subject lease must be autonomous: %+v", seen)
	}
	if got := host.registrar.Handles(); len(got) != 1 || got[0] != CommandMessageSend {
		t.Fatalf("registered handlers = %v", got)
	}
}

func TestTypedOnAndEmitRoundTrip(t *testing.T) {
	host := newTypedTestHost()
	want := ChangedEvent{Cursor: "opaque:4", ConversationID: "project:gateway", EndpointIDs: []string{"endpoint-codex-1"}, Sequence: 4, ChangedAt: "2026-09-10T12:00:00Z"}
	var got ChangedEvent
	sub, err := On(host, Changed, func(event ChangedEvent) { got = event })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel()
	if err := Emit(host, Changed, want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
}

// operationSchemaHash independently applies the frozen bd.schema-id.v1
// algorithm so descriptor hashes drift whenever a wire shape, classification,
// operation name, or revision changes.
func operationSchemaHash(kind, name string, revision uint32, roots ...reflect.Type) string {
	atoms := []string{}
	atom := func(value string) { atoms = append(atoms, strconv.Itoa(len(value))+":"+value+";") }
	atom("bd.schema-id.v1")
	atom(kind)
	atom(name)
	atom(strconv.FormatUint(uint64(revision), 10))
	path := map[reflect.Type]bool{}
	for _, root := range roots {
		appendSchemaAtoms(atom, root, path)
	}
	sum := sha256.Sum256([]byte(strings.Join(atoms, "")))
	return hex.EncodeToString(sum[:])
}

func appendSchemaAtoms(atom func(string), typ reflect.Type, path map[reflect.Type]bool) {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if path[typ] {
		panic("cyclic test schema")
	}
	path[typ] = true
	defer delete(path, typ)

	type fieldModel struct {
		jsonName string
		optional bool
		typ      reflect.Type
		wire     string
		class    string
		nested   bool
	}
	fields := make([]fieldModel, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		parts := strings.Split(field.Tag.Get("json"), ",")
		if parts[0] == "-" {
			continue
		}
		model := fieldModel{jsonName: parts[0], typ: field.Type, class: field.Tag.Get("bd")}
		if model.jsonName == "" {
			model.jsonName = field.Name
		}
		asString := false
		for _, option := range parts[1:] {
			switch option {
			case "omitempty", "omitzero":
				model.optional = true
			case "string":
				asString = true
			}
		}
		base := field.Type
		for base.Kind() == reflect.Pointer || base.Kind() == reflect.Slice {
			base = base.Elem()
		}
		switch base.Kind() {
		case reflect.Struct:
			model.wire, model.nested = "object", true
		case reflect.String:
			model.wire = "string"
		case reflect.Bool:
			model.wire = "bool"
		case reflect.Int, reflect.Int32:
			model.wire = "int32"
		case reflect.Uint32:
			model.wire = "uint32"
		case reflect.Int64:
			model.wire = "int64.string"
		case reflect.Uint64:
			model.wire = "uint64.string"
		default:
			panic("unsupported test wire kind " + base.Kind().String())
		}
		if asString && !strings.HasSuffix(model.wire, ".string") && model.wire != "object" {
			model.wire += ".string"
		}
		fields = append(fields, model)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].jsonName < fields[j].jsonName })
	atom("struct")
	atom(strconv.Itoa(len(fields)))
	for _, field := range fields {
		atom(field.jsonName)
		if field.optional {
			atom("optional")
		} else {
			atom("required")
		}
		if !field.optional && (field.typ.Kind() == reflect.Pointer || field.typ.Kind() == reflect.Slice) {
			atom("nullable")
		} else {
			atom("nonnull")
		}
		if field.typ.Kind() == reflect.Slice {
			atom("list")
		} else {
			atom("single")
		}
		atom(field.wire)
		atom(field.class)
		if field.nested {
			appendSchemaAtoms(atom, field.typ, path)
		} else {
			atom("-")
		}
	}
}
