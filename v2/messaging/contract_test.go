package messaging

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validEndpoint() EndpointIdentity {
	return EndpointIdentity{
		EndpointID:   "endpoint-codex-1",
		ActorID:      "actor-ryan",
		DisplayName:  "Codex",
		Title:        "Implementation agent",
		Provider:     "openai",
		SourcePlugin: "orchestration-terminals",
	}
}

func validConversation() Conversation {
	return Conversation{
		ConversationID:         "project:gateway",
		OwnerEndpointID:        "endpoint-codex-1",
		ParticipantEndpointIDs: []string{"endpoint-codex-1", "endpoint-claude-1"},
		CreatedAt:              "2026-09-10T12:00:00Z",
		UpdatedAt:              "2026-09-10T12:00:01.123456789Z",
	}
}

func validMessage() Message {
	return Message{
		MessageID:      "message-1",
		ConversationID: "project:gateway",
		Sender:         validEndpoint(),
		Body:           "Review the terminal input.",
		Kind:           "note",
		Metadata:       []MetadataEntry{{Key: "trace", Value: "abc"}},
		References:     []MessageReference{{Kind: "task", ID: "TM-223", Label: "Agent messaging"}},
		Sequence:       DecimalUint64(math.MaxUint64),
		CreatedAt:      "2026-09-10T12:00:01Z",
		Delivery: MessageDelivery{
			State:     DeliveryPending,
			Attempt:   DecimalUint64(0),
			UpdatedAt: "2026-09-10T12:00:01Z",
		},
	}
}

func TestCanonicalOperationNamesAndLimits(t *testing.T) {
	if ServiceID != "agent-messaging" || ContractRevision != 1 {
		t.Fatalf("service identity = %q revision %d", ServiceID, ContractRevision)
	}
	commands := map[string]string{
		"endpoint upsert":   CommandEndpointUpsert,
		"endpoint remove":   CommandEndpointRemove,
		"conversation open": CommandConversationOpen,
		"message send":      CommandMessageSend,
		"message list":      CommandMessageList,
		"receipt ack":       CommandReceiptAck,
		"delivery update":   CommandDeliveryUpdate,
		"inbox summary":     CommandInboxSummary,
	}
	want := map[string]string{
		"endpoint upsert":   "cmd.messaging.v1.endpoint.upsert",
		"endpoint remove":   "cmd.messaging.v1.endpoint.remove",
		"conversation open": "cmd.messaging.v1.conversation.open",
		"message send":      "cmd.messaging.v1.message.send",
		"message list":      "cmd.messaging.v1.message.list",
		"receipt ack":       "cmd.messaging.v1.receipt.ack",
		"delivery update":   "cmd.messaging.v1.delivery.update",
		"inbox summary":     "cmd.messaging.v1.inbox.summary",
	}
	for name, got := range commands {
		if got != want[name] {
			t.Errorf("%s = %q, want %q", name, got, want[name])
		}
	}
	if EventChanged != "event.messaging.v1.changed" {
		t.Fatalf("changed event = %q", EventChanged)
	}
	if MaxBodyBytes != 32<<10 || MaxMetadataBytes != 8<<10 || MaxListItems != 50 || MaxListResultBytes != 48<<10 || MaxEnvelopeBytes != 64<<10 || OperationTimeoutSeconds != 5 || MaxInFlight != 4 || IdempotencyRetentionDays != 7 {
		t.Fatal("published messaging limits drifted")
	}
}

func TestDecimalIntegersMarshalAsStringsPastJavaScriptPrecision(t *testing.T) {
	message := validMessage()
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"sequence":"18446744073709551615"`) || !strings.Contains(string(raw), `"attempt":"0"`) {
		t.Fatalf("uint64 values were not decimal strings: %s", raw)
	}

	wire := struct {
		Signed DecimalInt64 `json:"signed,string"`
	}{Signed: DecimalInt64(math.MaxInt64)}
	raw, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"signed":"9223372036854775807"`) {
		t.Fatalf("int64 value was not a decimal string: %s", raw)
	}
}

func TestCoreRecordValidation(t *testing.T) {
	endpoint := validEndpoint()
	conversation := validConversation()
	message := validMessage()
	receipt := Receipt{
		EndpointID:      "endpoint-claude-1",
		ConversationID:  conversation.ConversationID,
		ThroughSequence: DecimalUint64(1),
		AcknowledgedAt:  "2026-09-10T12:01:00Z",
	}
	idempotency := IdempotencyRecord{
		OwnerEndpointID: endpoint.EndpointID,
		Key:             "send-1",
		PayloadHash:     strings.Repeat("a", 64),
		MessageID:       message.MessageID,
		CreatedAt:       "2026-09-10T12:00:01Z",
		ExpiresAt:       "2026-09-17T12:00:01Z",
	}

	for name, validate := range map[string]func() error{
		"endpoint":     endpoint.Validate,
		"conversation": conversation.Validate,
		"message":      message.Validate,
		"receipt":      receipt.Validate,
		"idempotency":  idempotency.Validate,
	} {
		if err := validate(); err != nil {
			t.Errorf("valid %s: %v", name, err)
		}
	}

	badConversation := conversation
	badConversation.ParticipantEndpointIDs = []string{"endpoint-claude-1"}
	if err := badConversation.Validate(); err == nil {
		t.Fatal("conversation accepted an owner absent from participants")
	}
	badMessage := message
	badMessage.Body = strings.Repeat("x", MaxBodyBytes+1)
	if err := badMessage.Validate(); err == nil {
		t.Fatal("message accepted an oversized body")
	}
	badMessage = message
	badMessage.CreatedAt = "yesterday"
	if err := badMessage.Validate(); err == nil {
		t.Fatal("message accepted a non-RFC3339 timestamp")
	}
	badIdempotency := idempotency
	badIdempotency.PayloadHash = "not-sha256"
	if err := badIdempotency.Validate(); err == nil {
		t.Fatal("idempotency record accepted an invalid payload hash")
	}
	badReceipt := receipt
	badReceipt.ThroughSequence = 0
	if err := badReceipt.Validate(); err == nil {
		t.Fatal("receipt accepted a zero high-water sequence")
	}
}

func TestStatusAndStableFaults(t *testing.T) {
	for _, code := range []FaultCode{FaultInvalidArgument, FaultDenied, FaultNotFound, FaultConflict, FaultCapacity, FaultUnavailable} {
		status := OperationStatus{OK: false, Fault: &Fault{Code: code, Message: "failed"}}
		if err := status.Validate(); err != nil {
			t.Errorf("fault %q: %v", code, err)
		}
	}
	if err := (OperationStatus{OK: true, Fault: &Fault{Code: FaultConflict}}).Validate(); err == nil {
		t.Fatal("success accepted a fault")
	}
	if err := (OperationStatus{OK: false}).Validate(); err == nil {
		t.Fatal("failure accepted no fault")
	}
	if err := (OperationStatus{OK: false, Fault: &Fault{Code: "surprise"}}).Validate(); err == nil {
		t.Fatal("unknown fault code accepted")
	}
	failure := MessageSendResult{Status: OperationStatus{Fault: &Fault{Code: FaultDenied, Message: "not permitted"}}}
	if err := failure.Validate(); err != nil {
		t.Fatalf("valid failed result: %v", err)
	}
	failure.Status.Fault.Message = strings.Repeat("x", MaxEnvelopeBytes)
	if err := failure.Validate(); err == nil {
		t.Fatal("failed result accepted an oversized fault payload")
	}
}

func TestOperationPayloadValidation(t *testing.T) {
	endpoint := validEndpoint()
	conversation := validConversation()
	message := validMessage()
	receipt := Receipt{EndpointID: "endpoint-claude-1", ConversationID: conversation.ConversationID, ThroughSequence: 1, AcknowledgedAt: "2026-09-10T12:01:00Z"}
	success := OperationStatus{OK: true}

	valid := map[string]func() error{
		"endpoint upsert request":   (EndpointUpsertRequest{Endpoint: endpoint}).Validate,
		"endpoint upsert result":    (EndpointUpsertResult{Status: success, Endpoint: endpoint}).Validate,
		"endpoint remove request":   (EndpointRemoveRequest{EndpointID: endpoint.EndpointID}).Validate,
		"endpoint remove result":    (EndpointRemoveResult{Status: success, EndpointID: endpoint.EndpointID}).Validate,
		"conversation open request": (ConversationOpenRequest{ConversationID: conversation.ConversationID, OwnerEndpointID: conversation.OwnerEndpointID, ParticipantEndpointIDs: conversation.ParticipantEndpointIDs}).Validate,
		"conversation open result":  (ConversationOpenResult{Status: success, Conversation: conversation}).Validate,
		"message send request":      (MessageSendRequest{ConversationID: conversation.ConversationID, SenderEndpointID: endpoint.EndpointID, Body: message.Body, Kind: message.Kind, Metadata: message.Metadata, References: message.References, IdempotencyKey: "send-1"}).Validate,
		"message send result":       (MessageSendResult{Status: success, Message: message}).Validate,
		"message list request":      (MessageListRequest{ConversationID: conversation.ConversationID, RequesterEndpointID: endpoint.EndpointID, AfterCursor: "opaque:1", Limit: MaxListItems}).Validate,
		"message list result":       (MessageListResult{Status: success, Messages: []Message{message}, NextCursor: "opaque:2", HasMore: true}).Validate,
		"receipt ack request":       (ReceiptAckRequest{EndpointID: receipt.EndpointID, ConversationID: receipt.ConversationID, ThroughSequence: receipt.ThroughSequence}).Validate,
		"receipt ack result":        (ReceiptAckResult{Status: success, Receipt: receipt}).Validate,
		"delivery update request":   (DeliveryUpdateRequest{MessageID: message.MessageID, AdapterID: "pty-delivery", FromState: DeliveryPending, ToState: DeliveryDispatching, Attempt: 1, UpdatedAt: "2026-09-10T12:00:02Z"}).Validate,
		"delivery update result":    (DeliveryUpdateResult{Status: success, Delivery: MessageDelivery{State: DeliveryDispatching, AdapterID: "pty-delivery", Attempt: 1, UpdatedAt: "2026-09-10T12:00:02Z"}}).Validate,
		"inbox summary request":     (InboxSummaryRequest{EndpointID: endpoint.EndpointID}).Validate,
		"inbox summary result":      (InboxSummaryResult{Status: success, EndpointID: endpoint.EndpointID, UnreadCount: 1, Conversations: []InboxConversationSummary{{ConversationID: conversation.ConversationID, UnreadCount: 1, LatestMessageID: message.MessageID, LatestSequence: message.Sequence, UpdatedAt: message.CreatedAt}}}).Validate,
		"changed event":             (ChangedEvent{Cursor: "opaque:2", ConversationID: conversation.ConversationID, EndpointIDs: []string{endpoint.EndpointID}, Sequence: message.Sequence, ChangedAt: message.CreatedAt}).Validate,
	}
	for name, validate := range valid {
		if err := validate(); err != nil {
			t.Errorf("valid %s: %v", name, err)
		}
	}

	if err := (MessageSendRequest{ConversationID: conversation.ConversationID, SenderEndpointID: endpoint.EndpointID, Body: "hello", Kind: "note"}).Validate(); err == nil {
		t.Fatal("message send accepted no idempotency key")
	}
	if err := (MessageListRequest{ConversationID: conversation.ConversationID, RequesterEndpointID: endpoint.EndpointID, Limit: MaxListItems + 1}).Validate(); err == nil {
		t.Fatal("message list accepted a limit above the contract maximum")
	}
	if err := (DeliveryUpdateRequest{MessageID: message.MessageID, AdapterID: "pty-delivery", FromState: DeliveryPending, ToState: DeliveryDelivered, Attempt: 1, UpdatedAt: message.CreatedAt}).Validate(); err == nil {
		t.Fatal("delivery update accepted an invalid state transition")
	}
	if err := (ReceiptAckRequest{EndpointID: receipt.EndpointID, ConversationID: receipt.ConversationID}).Validate(); err == nil {
		t.Fatal("receipt ack accepted a zero high-water sequence")
	}
}

func TestListResultHonorsEncodedSizeLimit(t *testing.T) {
	message := validMessage()
	message.Body = strings.Repeat("x", MaxBodyBytes)
	result := MessageListResult{Status: OperationStatus{OK: true}, Messages: []Message{message, message}}
	if err := result.Validate(); err == nil {
		t.Fatal("list result accepted an encoded payload above its limit")
	}
}

func TestPayloadUnionAcceptsEveryOperationPayload(t *testing.T) {
	acceptPayload(Fault{})
	acceptPayload(OperationStatus{})
	acceptPayload(EndpointIdentity{})
	acceptPayload(Conversation{})
	acceptPayload(MetadataEntry{})
	acceptPayload(MessageReference{})
	acceptPayload(MessageDelivery{})
	acceptPayload(Message{})
	acceptPayload(Receipt{})
	acceptPayload(IdempotencyRecord{})
	acceptPayload(EndpointUpsertRequest{})
	acceptPayload(EndpointUpsertResult{})
	acceptPayload(EndpointRemoveRequest{})
	acceptPayload(EndpointRemoveResult{})
	acceptPayload(ConversationOpenRequest{})
	acceptPayload(ConversationOpenResult{})
	acceptPayload(MessageSendRequest{})
	acceptPayload(MessageSendResult{})
	acceptPayload(MessageListRequest{})
	acceptPayload(MessageListResult{})
	acceptPayload(ReceiptAckRequest{})
	acceptPayload(ReceiptAckResult{})
	acceptPayload(DeliveryUpdateRequest{})
	acceptPayload(DeliveryUpdateResult{})
	acceptPayload(InboxSummaryRequest{})
	acceptPayload(InboxConversationSummary{})
	acceptPayload(InboxSummaryResult{})
	acceptPayload(ChangedEvent{})
}

func acceptPayload[T Payload](T) {}

func TestPayloadTypesUsePortableClassifiedJSONFields(t *testing.T) {
	roots := []any{
		Fault{}, OperationStatus{}, EndpointIdentity{}, Conversation{}, MetadataEntry{}, MessageReference{}, MessageDelivery{}, Message{}, Receipt{}, IdempotencyRecord{},
		EndpointUpsertRequest{}, EndpointUpsertResult{}, EndpointRemoveRequest{}, EndpointRemoveResult{}, ConversationOpenRequest{}, ConversationOpenResult{},
		MessageSendRequest{}, MessageSendResult{}, MessageListRequest{}, MessageListResult{}, ReceiptAckRequest{}, ReceiptAckResult{}, DeliveryUpdateRequest{}, DeliveryUpdateResult{},
		InboxSummaryRequest{}, InboxConversationSummary{}, InboxSummaryResult{}, ChangedEvent{},
	}
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, path string) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if !field.IsExported() || jsonTag == "-" {
				continue
			}
			class := field.Tag.Get("bd")
			if class != "public" && class != "subject" && class != "secret" {
				t.Errorf("%s.%s has invalid bd classification %q", path, field.Name, class)
			}
			kind := field.Type.Kind()
			if kind == reflect.Int64 || kind == reflect.Uint64 {
				if !strings.Contains(jsonTag, ",string") {
					t.Errorf("%s.%s is 64-bit without json ,string", path, field.Name)
				}
			}
			switch kind {
			case reflect.Map, reflect.Interface, reflect.Float32, reflect.Float64, reflect.Array:
				t.Errorf("%s.%s uses unsupported wire kind %s", path, field.Name, kind)
			}
			walk(field.Type, path+"."+field.Name)
		}
	}
	for _, root := range roots {
		typ := reflect.TypeOf(root)
		walk(typ, typ.Name())
	}
}

func TestIdempotencyRetentionIsExactlySevenDays(t *testing.T) {
	record := IdempotencyRecord{
		OwnerEndpointID: "endpoint-codex-1",
		Key:             "send-1",
		PayloadHash:     strings.Repeat("a", 64),
		MessageID:       "message-1",
		CreatedAt:       "2026-09-10T12:00:00Z",
		ExpiresAt:       "2026-09-17T12:00:00Z",
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("seven-day record: %v", err)
	}
	created, _ := time.Parse(time.RFC3339, record.CreatedAt)
	record.ExpiresAt = created.Add(6 * 24 * time.Hour).Format(time.RFC3339)
	if err := record.Validate(); err == nil {
		t.Fatal("idempotency record accepted a non-seven-day retention")
	}
}
