// Code generated from contract.go using the frozen bd.schema-id.v1 model. DO NOT EDIT.

package messaging

import (
	"context"
	"encoding/json"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
)

// Payload is the exact set of request, result, and event roots accepted by the
// agent-messaging typed API. The union intentionally omits ~ so a named wrapper
// with an unclassified field cannot silently join the contract.
type Payload interface {
	Fault | OperationStatus |
		EndpointIdentity | Conversation |
		MetadataEntry | MessageReference | MessageDelivery | Message |
		Receipt | IdempotencyRecord |
		EndpointUpsertRequest | EndpointUpsertResult |
		EndpointRemoveRequest | EndpointRemoveResult |
		ConversationOpenRequest | ConversationOpenResult |
		MessageSendRequest | MessageSendResult |
		MessageListRequest | MessageListResult |
		ReceiptAckRequest | ReceiptAckResult |
		DeliveryUpdateRequest | DeliveryUpdateResult |
		InboxSummaryRequest | InboxConversationSummary | InboxSummaryResult |
		ChangedEvent
}

// Command pairs one request type with its only valid response type.
type Command[Req, Resp Payload] struct{ descriptor plugin.Descriptor }

// Name returns the canonical operation name used for registration and logs.
func (c Command[Req, Resp]) Name() string { return c.descriptor.Name() }

// Event pairs one event descriptor with its only valid payload type.
type Event[T Payload] struct{ descriptor plugin.Descriptor }

// Name returns the canonical event name used for subscriptions and logs.
func (e Event[T]) Name() string { return e.descriptor.Name() }

var (
	UpsertEndpoint = Command[EndpointUpsertRequest, EndpointUpsertResult]{
		descriptor: plugin.NewDescriptor(CommandEndpointUpsert, ContractRevision, "5417e52a31fb7c85330e3904e077b597d4fc6becc377e6703e836aed96af4f46"),
	}
	RemoveEndpoint = Command[EndpointRemoveRequest, EndpointRemoveResult]{
		descriptor: plugin.NewDescriptor(CommandEndpointRemove, ContractRevision, "23dcc2946ed0b5ebfdce8c49d40d08936e354316fe212fe36b9fc472c36c519f"),
	}
	OpenConversation = Command[ConversationOpenRequest, ConversationOpenResult]{
		descriptor: plugin.NewDescriptor(CommandConversationOpen, ContractRevision, "e2a057abb4e3da355643232dd955c3cc0479525e61b86b4935d142b57fd0614c"),
	}
	SendMessage = Command[MessageSendRequest, MessageSendResult]{
		descriptor: plugin.NewDescriptor(CommandMessageSend, ContractRevision, "3ba7b86d3ea71468beeb0c3707ac25d0029d30cc292fc79b58f9671a68a0030e"),
	}
	ListMessages = Command[MessageListRequest, MessageListResult]{
		descriptor: plugin.NewDescriptor(CommandMessageList, ContractRevision, "1d5405f976e928d210dde14e48a44d6b6bb1c4c8d60602cb5a6b62031e22c969"),
	}
	AckReceipt = Command[ReceiptAckRequest, ReceiptAckResult]{
		descriptor: plugin.NewDescriptor(CommandReceiptAck, ContractRevision, "f69abb5188e12c2f6161f76edf31e7d748aac0e13ab027b80e4d6cd5b9206641"),
	}
	UpdateDelivery = Command[DeliveryUpdateRequest, DeliveryUpdateResult]{
		descriptor: plugin.NewDescriptor(CommandDeliveryUpdate, ContractRevision, "c5849b61d938212dd30359f4de6da4379a7fc2372ccba3b97f7300ba0188dcd8"),
	}
	GetInboxSummary = Command[InboxSummaryRequest, InboxSummaryResult]{
		descriptor: plugin.NewDescriptor(CommandInboxSummary, ContractRevision, "d2d2fe607678ed1fae0af6146016dc461bdf93eda7e2a73bf42ec9d8a347a9c5"),
	}
	Changed = Event[ChangedEvent]{
		descriptor: plugin.NewDescriptor(EventChanged, ContractRevision, "6490e9913e2cdd41406a9f0024762c121a4ec52168fa660c6be1c5fd8c74d8db"),
	}
)

// Call invokes an agent-messaging command through the host.
func Call[Req, Resp Payload](ctx context.Context, host plugin.Host, command Command[Req, Resp], req Req) (Resp, error) {
	var resp Resp
	if err := plugin.Invoke(ctx, host, command.descriptor, req, &resp); err != nil {
		var zero Resp
		return zero, err
	}
	return resp, nil
}

// Handle registers an agent-messaging command handler on the shared registrar.
func Handle[Req, Resp Payload](registrar *plugin.Registrar, command Command[Req, Resp], fn func(context.Context, plugin.Caller, Req) (Resp, error)) {
	if fn == nil {
		panic("messaging: Handle needs a handler")
	}
	plugin.HandleRaw(registrar, command.descriptor, func(ctx context.Context, caller plugin.Caller, raw json.RawMessage) (json.RawMessage, error) {
		var req Req
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, plugin.Fault{Code: plugin.FaultSchema, Op: command.Name(), Message: "request does not decode", Err: err}
		}
		resp, err := fn(ctx, caller, req)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(resp)
		if err != nil {
			return nil, plugin.Fault{Code: plugin.FaultSchema, Op: command.Name(), Message: "response does not marshal", Err: err}
		}
		return body, nil
	})
}

// On subscribes to the lossy changed event. Consumers must reconcile by cursor
// after subscription loss and periodically on legacy hosts.
func On[T Payload](host plugin.Host, event Event[T], fn func(T)) (plugin.Subscription, error) {
	if fn == nil {
		return nil, plugin.Fault{Code: plugin.FaultUnavailable, Op: event.Name(), Message: "nil handler"}
	}
	return plugin.Observe(host, event.descriptor, func(raw json.RawMessage) {
		var value T
		if json.Unmarshal(raw, &value) != nil {
			return
		}
		fn(value)
	})
}

// Emit publishes an agent-messaging event through the host.
func Emit[T Payload](host plugin.Host, event Event[T], value T) error {
	return plugin.Publish(host, event.descriptor, value)
}
