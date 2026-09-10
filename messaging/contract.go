// Package messaging defines the public wire contract owned by the
// agent-messaging service. It contains data and validation only; storage,
// authorization, delivery, and event publication belong to the service and host.
package messaging

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ServiceID        = "agent-messaging"
	ContractRevision = 1

	CommandEndpointUpsert   = "cmd.messaging.v1.endpoint.upsert"
	CommandEndpointRemove   = "cmd.messaging.v1.endpoint.remove"
	CommandConversationOpen = "cmd.messaging.v1.conversation.open"
	CommandMessageSend      = "cmd.messaging.v1.message.send"
	CommandMessageList      = "cmd.messaging.v1.message.list"
	CommandReceiptAck       = "cmd.messaging.v1.receipt.ack"
	CommandDeliveryUpdate   = "cmd.messaging.v1.delivery.update"
	CommandInboxSummary     = "cmd.messaging.v1.inbox.summary"
	EventChanged            = "event.messaging.v1.changed"

	MaxBodyBytes             = 32 << 10
	MaxMetadataBytes         = 8 << 10
	MaxListItems             = 50
	MaxListResultBytes       = 48 << 10
	MaxEnvelopeBytes         = 64 << 10
	OperationTimeoutSeconds  = 5
	MaxInFlight              = 4
	IdempotencyRetentionDays = 7
)

// DecimalInt64 and DecimalUint64 name 64-bit wire integers. Every JSON-visible
// field using one must carry json:",string" so browsers never round it through
// a JavaScript number.
type (
	DecimalInt64  = int64
	DecimalUint64 = uint64
)

type FaultCode string

const (
	FaultInvalidArgument FaultCode = "invalid_argument"
	FaultDenied          FaultCode = "denied"
	FaultNotFound        FaultCode = "not_found"
	FaultConflict        FaultCode = "conflict"
	FaultCapacity        FaultCode = "capacity"
	FaultUnavailable     FaultCode = "unavailable"
)

// Fault is a stable machine code plus an optional safe explanation. Callers
// branch on Code, never Message.
type Fault struct {
	Code    FaultCode `json:"code" bd:"public"`
	Message string    `json:"message,omitempty" bd:"subject"`
}

func (f Fault) Validate() error {
	switch f.Code {
	case FaultInvalidArgument, FaultDenied, FaultNotFound, FaultConflict, FaultCapacity, FaultUnavailable:
	default:
		return fmt.Errorf("unknown fault code %q", f.Code)
	}
	return optionalText("fault.message", f.Message)
}

// OperationStatus is present on every command result. A successful status has
// no fault; a failed status has exactly one stable fault.
type OperationStatus struct {
	OK    bool   `json:"ok" bd:"public"`
	Fault *Fault `json:"fault,omitempty" bd:"subject"`
}

func (s OperationStatus) Validate() error {
	if s.OK {
		if s.Fault != nil {
			return fmt.Errorf("successful status cannot contain a fault")
		}
		return nil
	}
	if s.Fault == nil {
		return fmt.Errorf("failed status requires a fault")
	}
	return s.Fault.Validate()
}

// EndpointIdentity is copied into each message. EndpointID and ActorID remain
// stable while display fields may be refreshed by endpoint.upsert.
type EndpointIdentity struct {
	EndpointID   string `json:"endpointId" bd:"subject"`
	ActorID      string `json:"actorId" bd:"subject"`
	DisplayName  string `json:"displayName" bd:"subject"`
	Title        string `json:"title" bd:"subject"`
	Provider     string `json:"provider" bd:"subject"`
	SourcePlugin string `json:"sourcePlugin" bd:"subject"`
}

func (e EndpointIdentity) Validate() error {
	for name, value := range map[string]string{
		"endpoint.endpointId":   e.EndpointID,
		"endpoint.actorId":      e.ActorID,
		"endpoint.displayName":  e.DisplayName,
		"endpoint.title":        e.Title,
		"endpoint.provider":     e.Provider,
		"endpoint.sourcePlugin": e.SourcePlugin,
	} {
		if err := requiredText(name, value); err != nil {
			return err
		}
	}
	return validateEncodedSize("endpoint", e, MaxEnvelopeBytes)
}

type Conversation struct {
	ConversationID         string   `json:"conversationId" bd:"subject"`
	OwnerEndpointID        string   `json:"ownerEndpointId" bd:"subject"`
	ParticipantEndpointIDs []string `json:"participantEndpointIds" bd:"subject"`
	CreatedAt              string   `json:"createdAt" bd:"subject"`
	UpdatedAt              string   `json:"updatedAt" bd:"subject"`
}

func (c Conversation) Validate() error {
	if err := validateConversationIdentity(c.ConversationID, c.OwnerEndpointID, c.ParticipantEndpointIDs); err != nil {
		return err
	}
	if err := rfc3339("conversation.createdAt", c.CreatedAt); err != nil {
		return err
	}
	if err := rfc3339("conversation.updatedAt", c.UpdatedAt); err != nil {
		return err
	}
	return validateEncodedSize("conversation", c, MaxEnvelopeBytes)
}

type MetadataEntry struct {
	Key   string `json:"key" bd:"subject"`
	Value string `json:"value" bd:"subject"`
}

type MessageReference struct {
	Kind  string `json:"kind" bd:"subject"`
	ID    string `json:"id" bd:"subject"`
	Label string `json:"label,omitempty" bd:"subject"`
}

type DeliveryState string

const (
	DeliveryPending     DeliveryState = "pending"
	DeliveryDispatching DeliveryState = "dispatching"
	DeliveryDelivered   DeliveryState = "delivered"
	DeliveryFailed      DeliveryState = "failed"
	DeliveryUnknown     DeliveryState = "unknown"
)

type MessageDelivery struct {
	State     DeliveryState `json:"state" bd:"subject"`
	AdapterID string        `json:"adapterId,omitempty" bd:"subject"`
	Attempt   DecimalUint64 `json:"attempt,string" bd:"subject"`
	UpdatedAt string        `json:"updatedAt" bd:"subject"`
	Fault     *Fault        `json:"fault,omitempty" bd:"subject"`
}

func (d MessageDelivery) Validate() error {
	switch d.State {
	case DeliveryPending:
		if d.AdapterID != "" || d.Attempt != 0 || d.Fault != nil {
			return fmt.Errorf("pending delivery cannot have adapter, attempt, or fault")
		}
	case DeliveryDispatching, DeliveryDelivered, DeliveryUnknown:
		if err := requiredText("delivery.adapterId", d.AdapterID); err != nil {
			return err
		}
		if d.Attempt == 0 {
			return fmt.Errorf("delivery.attempt must be positive after dispatch starts")
		}
		if d.Fault != nil {
			return fmt.Errorf("delivery state %q cannot contain a fault", d.State)
		}
	case DeliveryFailed:
		if err := requiredText("delivery.adapterId", d.AdapterID); err != nil {
			return err
		}
		if d.Attempt == 0 {
			return fmt.Errorf("delivery.attempt must be positive after dispatch starts")
		}
		if d.Fault == nil {
			return fmt.Errorf("failed delivery requires a fault")
		}
		if err := d.Fault.Validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown delivery state %q", d.State)
	}
	return rfc3339("delivery.updatedAt", d.UpdatedAt)
}

type Message struct {
	MessageID      string             `json:"messageId" bd:"subject"`
	ConversationID string             `json:"conversationId" bd:"subject"`
	Sender         EndpointIdentity   `json:"sender" bd:"subject"`
	Body           string             `json:"body" bd:"subject"`
	Kind           string             `json:"kind" bd:"subject"`
	Metadata       []MetadataEntry    `json:"metadata,omitempty" bd:"subject"`
	References     []MessageReference `json:"references,omitempty" bd:"subject"`
	Sequence       DecimalUint64      `json:"sequence,string" bd:"subject"`
	CreatedAt      string             `json:"createdAt" bd:"subject"`
	Delivery       MessageDelivery    `json:"delivery" bd:"subject"`
}

func (m Message) Validate() error {
	if err := requiredText("message.messageId", m.MessageID); err != nil {
		return err
	}
	if err := requiredText("message.conversationId", m.ConversationID); err != nil {
		return err
	}
	if err := m.Sender.Validate(); err != nil {
		return err
	}
	if err := messageBody(m.Body); err != nil {
		return err
	}
	if err := requiredText("message.kind", m.Kind); err != nil {
		return err
	}
	if m.Sequence == 0 {
		return fmt.Errorf("message.sequence must be positive")
	}
	if err := validateMetadata(m.Metadata); err != nil {
		return err
	}
	if err := validateReferences(m.References); err != nil {
		return err
	}
	if err := rfc3339("message.createdAt", m.CreatedAt); err != nil {
		return err
	}
	if err := m.Delivery.Validate(); err != nil {
		return err
	}
	return validateEncodedSize("message", m, MaxEnvelopeBytes)
}

type Receipt struct {
	EndpointID      string        `json:"endpointId" bd:"subject"`
	ConversationID  string        `json:"conversationId" bd:"subject"`
	ThroughSequence DecimalUint64 `json:"throughSequence,string" bd:"subject"`
	AcknowledgedAt  string        `json:"acknowledgedAt" bd:"subject"`
}

func (r Receipt) Validate() error {
	if err := requiredText("receipt.endpointId", r.EndpointID); err != nil {
		return err
	}
	if err := requiredText("receipt.conversationId", r.ConversationID); err != nil {
		return err
	}
	if r.ThroughSequence == 0 {
		return fmt.Errorf("receipt.throughSequence must be positive")
	}
	if err := rfc3339("receipt.acknowledgedAt", r.AcknowledgedAt); err != nil {
		return err
	}
	return validateEncodedSize("receipt", r, MaxEnvelopeBytes)
}

// IdempotencyRecord binds one sender-owned key to the canonical request hash
// and original result for seven days. PayloadHash is lowercase SHA-256 hex.
type IdempotencyRecord struct {
	OwnerEndpointID string `json:"ownerEndpointId" bd:"subject"`
	Key             string `json:"key" bd:"subject"`
	PayloadHash     string `json:"payloadHash" bd:"subject"`
	MessageID       string `json:"messageId" bd:"subject"`
	CreatedAt       string `json:"createdAt" bd:"subject"`
	ExpiresAt       string `json:"expiresAt" bd:"subject"`
}

func (r IdempotencyRecord) Validate() error {
	for name, value := range map[string]string{
		"idempotency.ownerEndpointId": r.OwnerEndpointID,
		"idempotency.key":             r.Key,
		"idempotency.messageId":       r.MessageID,
	} {
		if err := requiredText(name, value); err != nil {
			return err
		}
	}
	if len(r.PayloadHash) != 64 {
		return fmt.Errorf("idempotency.payloadHash must be lowercase SHA-256 hex")
	}
	decoded, err := hex.DecodeString(r.PayloadHash)
	if err != nil || len(decoded) != 32 || strings.ToLower(r.PayloadHash) != r.PayloadHash {
		return fmt.Errorf("idempotency.payloadHash must be lowercase SHA-256 hex")
	}
	createdAt, err := parseRFC3339("idempotency.createdAt", r.CreatedAt)
	if err != nil {
		return err
	}
	expiresAt, err := parseRFC3339("idempotency.expiresAt", r.ExpiresAt)
	if err != nil {
		return err
	}
	if expiresAt.Sub(createdAt) != time.Duration(IdempotencyRetentionDays)*24*time.Hour {
		return fmt.Errorf("idempotency.expiresAt must be exactly %d days after createdAt", IdempotencyRetentionDays)
	}
	return validateEncodedSize("idempotency", r, MaxEnvelopeBytes)
}

type EndpointUpsertRequest struct {
	Endpoint EndpointIdentity `json:"endpoint" bd:"subject"`
}

func (r EndpointUpsertRequest) Validate() error { return validateRequest(r.Endpoint.Validate(), r) }

type EndpointUpsertResult struct {
	Status   OperationStatus  `json:"status" bd:"public"`
	Endpoint EndpointIdentity `json:"endpoint,omitempty" bd:"subject"`
}

func (r EndpointUpsertResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "endpoint upsert result", MaxEnvelopeBytes); done {
		return err
	}
	return validateRequest(r.Endpoint.Validate(), r)
}

type EndpointRemoveRequest struct {
	EndpointID string `json:"endpointId" bd:"subject"`
}

func (r EndpointRemoveRequest) Validate() error {
	return validateRequest(requiredText("endpointId", r.EndpointID), r)
}

type EndpointRemoveResult struct {
	Status     OperationStatus `json:"status" bd:"public"`
	EndpointID string          `json:"endpointId,omitempty" bd:"subject"`
}

func (r EndpointRemoveResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "endpoint remove result", MaxEnvelopeBytes); done {
		return err
	}
	return validateRequest(requiredText("endpointId", r.EndpointID), r)
}

type ConversationOpenRequest struct {
	ConversationID         string   `json:"conversationId" bd:"subject"`
	OwnerEndpointID        string   `json:"ownerEndpointId" bd:"subject"`
	ParticipantEndpointIDs []string `json:"participantEndpointIds" bd:"subject"`
}

func (r ConversationOpenRequest) Validate() error {
	return validateRequest(validateConversationIdentity(r.ConversationID, r.OwnerEndpointID, r.ParticipantEndpointIDs), r)
}

type ConversationOpenResult struct {
	Status       OperationStatus `json:"status" bd:"public"`
	Conversation Conversation    `json:"conversation,omitempty" bd:"subject"`
}

func (r ConversationOpenResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "conversation open result", MaxEnvelopeBytes); done {
		return err
	}
	return validateRequest(r.Conversation.Validate(), r)
}

type MessageSendRequest struct {
	ConversationID   string             `json:"conversationId" bd:"subject"`
	SenderEndpointID string             `json:"senderEndpointId" bd:"subject"`
	Body             string             `json:"body" bd:"subject"`
	Kind             string             `json:"kind" bd:"subject"`
	Metadata         []MetadataEntry    `json:"metadata,omitempty" bd:"subject"`
	References       []MessageReference `json:"references,omitempty" bd:"subject"`
	IdempotencyKey   string             `json:"idempotencyKey" bd:"subject"`
}

func (r MessageSendRequest) Validate() error {
	for name, value := range map[string]string{
		"conversationId":   r.ConversationID,
		"senderEndpointId": r.SenderEndpointID,
		"kind":             r.Kind,
		"idempotencyKey":   r.IdempotencyKey,
	} {
		if err := requiredText(name, value); err != nil {
			return err
		}
	}
	if err := messageBody(r.Body); err != nil {
		return err
	}
	if err := validateMetadata(r.Metadata); err != nil {
		return err
	}
	if err := validateReferences(r.References); err != nil {
		return err
	}
	return validateEncodedSize("message send request", r, MaxEnvelopeBytes)
}

type MessageSendResult struct {
	Status  OperationStatus `json:"status" bd:"public"`
	Message Message         `json:"message,omitempty" bd:"subject"`
}

func (r MessageSendResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "message send result", MaxEnvelopeBytes); done {
		return err
	}
	return validateRequest(r.Message.Validate(), r)
}

type MessageListRequest struct {
	ConversationID      string `json:"conversationId" bd:"subject"`
	RequesterEndpointID string `json:"requesterEndpointId" bd:"subject"`
	AfterCursor         string `json:"afterCursor,omitempty" bd:"subject"`
	Limit               uint32 `json:"limit,omitempty" bd:"subject"`
}

func (r MessageListRequest) Validate() error {
	if err := requiredText("conversationId", r.ConversationID); err != nil {
		return err
	}
	if err := requiredText("requesterEndpointId", r.RequesterEndpointID); err != nil {
		return err
	}
	if err := optionalText("afterCursor", r.AfterCursor); err != nil {
		return err
	}
	if r.Limit > MaxListItems {
		return fmt.Errorf("limit must be at most %d", MaxListItems)
	}
	return validateEncodedSize("message list request", r, MaxEnvelopeBytes)
}

type MessageListResult struct {
	Status     OperationStatus `json:"status" bd:"public"`
	Messages   []Message       `json:"messages,omitempty" bd:"subject"`
	NextCursor string          `json:"nextCursor,omitempty" bd:"subject"`
	HasMore    bool            `json:"hasMore" bd:"subject"`
}

func (r MessageListResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "message list result", MaxListResultBytes); done {
		return err
	}
	if len(r.Messages) > MaxListItems {
		return fmt.Errorf("messages must contain at most %d items", MaxListItems)
	}
	for i := range r.Messages {
		if err := r.Messages[i].Validate(); err != nil {
			return fmt.Errorf("messages[%d]: %w", i, err)
		}
	}
	if err := optionalText("nextCursor", r.NextCursor); err != nil {
		return err
	}
	if r.HasMore && r.NextCursor == "" {
		return fmt.Errorf("hasMore requires nextCursor")
	}
	return validateEncodedSize("message list result", r, MaxListResultBytes)
}

type ReceiptAckRequest struct {
	EndpointID      string        `json:"endpointId" bd:"subject"`
	ConversationID  string        `json:"conversationId" bd:"subject"`
	ThroughSequence DecimalUint64 `json:"throughSequence,string" bd:"subject"`
}

func (r ReceiptAckRequest) Validate() error {
	if err := requiredText("endpointId", r.EndpointID); err != nil {
		return err
	}
	if err := requiredText("conversationId", r.ConversationID); err != nil {
		return err
	}
	if r.ThroughSequence == 0 {
		return fmt.Errorf("throughSequence must be positive")
	}
	return validateEncodedSize("receipt ack request", r, MaxEnvelopeBytes)
}

type ReceiptAckResult struct {
	Status  OperationStatus `json:"status" bd:"public"`
	Receipt Receipt         `json:"receipt,omitempty" bd:"subject"`
}

func (r ReceiptAckResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "receipt ack result", MaxEnvelopeBytes); done {
		return err
	}
	return validateRequest(r.Receipt.Validate(), r)
}

type DeliveryUpdateRequest struct {
	MessageID string        `json:"messageId" bd:"subject"`
	AdapterID string        `json:"adapterId" bd:"subject"`
	FromState DeliveryState `json:"fromState" bd:"subject"`
	ToState   DeliveryState `json:"toState" bd:"subject"`
	Attempt   DecimalUint64 `json:"attempt,string" bd:"subject"`
	UpdatedAt string        `json:"updatedAt" bd:"subject"`
	Fault     *Fault        `json:"fault,omitempty" bd:"subject"`
}

func (r DeliveryUpdateRequest) Validate() error {
	if err := requiredText("messageId", r.MessageID); err != nil {
		return err
	}
	if err := requiredText("adapterId", r.AdapterID); err != nil {
		return err
	}
	validTransition := r.FromState == DeliveryPending && r.ToState == DeliveryDispatching ||
		r.FromState == DeliveryDispatching && (r.ToState == DeliveryDelivered || r.ToState == DeliveryFailed || r.ToState == DeliveryUnknown)
	if !validTransition {
		return fmt.Errorf("invalid delivery transition %q -> %q", r.FromState, r.ToState)
	}
	if r.Attempt == 0 {
		return fmt.Errorf("attempt must be positive")
	}
	if err := rfc3339("updatedAt", r.UpdatedAt); err != nil {
		return err
	}
	if r.ToState == DeliveryFailed {
		if r.Fault == nil {
			return fmt.Errorf("failed delivery update requires a fault")
		}
		if err := r.Fault.Validate(); err != nil {
			return err
		}
	} else if r.Fault != nil {
		return fmt.Errorf("delivery update to %q cannot contain a fault", r.ToState)
	}
	return validateEncodedSize("delivery update request", r, MaxEnvelopeBytes)
}

type DeliveryUpdateResult struct {
	Status   OperationStatus `json:"status" bd:"public"`
	Delivery MessageDelivery `json:"delivery,omitempty" bd:"subject"`
}

func (r DeliveryUpdateResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "delivery update result", MaxEnvelopeBytes); done {
		return err
	}
	return validateRequest(r.Delivery.Validate(), r)
}

type InboxSummaryRequest struct {
	EndpointID string `json:"endpointId" bd:"subject"`
}

func (r InboxSummaryRequest) Validate() error {
	return validateRequest(requiredText("endpointId", r.EndpointID), r)
}

type InboxConversationSummary struct {
	ConversationID  string        `json:"conversationId" bd:"subject"`
	UnreadCount     DecimalUint64 `json:"unreadCount,string" bd:"subject"`
	LatestMessageID string        `json:"latestMessageId,omitempty" bd:"subject"`
	LatestSequence  DecimalUint64 `json:"latestSequence,string" bd:"subject"`
	UpdatedAt       string        `json:"updatedAt" bd:"subject"`
}

func (s InboxConversationSummary) Validate() error {
	if err := requiredText("summary.conversationId", s.ConversationID); err != nil {
		return err
	}
	if err := optionalText("summary.latestMessageId", s.LatestMessageID); err != nil {
		return err
	}
	if (s.LatestMessageID == "") != (s.LatestSequence == 0) {
		return fmt.Errorf("latestMessageId and latestSequence must be present together")
	}
	return rfc3339("summary.updatedAt", s.UpdatedAt)
}

type InboxSummaryResult struct {
	Status        OperationStatus            `json:"status" bd:"public"`
	EndpointID    string                     `json:"endpointId,omitempty" bd:"subject"`
	UnreadCount   DecimalUint64              `json:"unreadCount,string" bd:"subject"`
	Conversations []InboxConversationSummary `json:"conversations,omitempty" bd:"subject"`
}

func (r InboxSummaryResult) Validate() error {
	if done, err := validateResultStatus(r.Status, r, "inbox summary result", MaxListResultBytes); done {
		return err
	}
	if err := requiredText("endpointId", r.EndpointID); err != nil {
		return err
	}
	var total DecimalUint64
	seen := map[string]bool{}
	for i := range r.Conversations {
		summary := r.Conversations[i]
		if err := summary.Validate(); err != nil {
			return fmt.Errorf("conversations[%d]: %w", i, err)
		}
		if seen[summary.ConversationID] {
			return fmt.Errorf("duplicate conversationId %q", summary.ConversationID)
		}
		seen[summary.ConversationID] = true
		if ^DecimalUint64(0)-total < summary.UnreadCount {
			return fmt.Errorf("conversation unread count overflow")
		}
		total += summary.UnreadCount
	}
	if total != r.UnreadCount {
		return fmt.Errorf("unreadCount does not equal conversation totals")
	}
	return validateEncodedSize("inbox summary result", r, MaxListResultBytes)
}

// ChangedEvent is a lossy wake-up emitted only after the durable commit. It
// intentionally carries no message body. Consumers reconcile from Cursor.
type ChangedEvent struct {
	Cursor         string        `json:"cursor" bd:"subject"`
	ConversationID string        `json:"conversationId,omitempty" bd:"subject"`
	EndpointIDs    []string      `json:"endpointIds,omitempty" bd:"subject"`
	Sequence       DecimalUint64 `json:"sequence,string" bd:"subject"`
	ChangedAt      string        `json:"changedAt" bd:"subject"`
}

func (e ChangedEvent) Validate() error {
	if err := requiredText("cursor", e.Cursor); err != nil {
		return err
	}
	if err := optionalText("conversationId", e.ConversationID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, endpointID := range e.EndpointIDs {
		if err := requiredText(fmt.Sprintf("endpointIds[%d]", i), endpointID); err != nil {
			return err
		}
		if seen[endpointID] {
			return fmt.Errorf("duplicate endpointId %q", endpointID)
		}
		seen[endpointID] = true
	}
	if err := rfc3339("changedAt", e.ChangedAt); err != nil {
		return err
	}
	return validateEncodedSize("changed event", e, MaxEnvelopeBytes)
}

func validateConversationIdentity(conversationID, ownerEndpointID string, participants []string) error {
	if err := requiredText("conversationId", conversationID); err != nil {
		return err
	}
	if err := requiredText("ownerEndpointId", ownerEndpointID); err != nil {
		return err
	}
	if len(participants) == 0 {
		return fmt.Errorf("participantEndpointIds must not be empty")
	}
	seen := map[string]bool{}
	for i, id := range participants {
		if err := requiredText(fmt.Sprintf("participantEndpointIds[%d]", i), id); err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("duplicate participant endpoint %q", id)
		}
		seen[id] = true
	}
	if !seen[ownerEndpointID] {
		return fmt.Errorf("ownerEndpointId must be a participant")
	}
	return nil
}

func validateMetadata(entries []MetadataEntry) error {
	seen := map[string]bool{}
	for i, entry := range entries {
		if err := requiredText(fmt.Sprintf("metadata[%d].key", i), entry.Key); err != nil {
			return err
		}
		if err := optionalText(fmt.Sprintf("metadata[%d].value", i), entry.Value); err != nil {
			return err
		}
		if seen[entry.Key] {
			return fmt.Errorf("duplicate metadata key %q", entry.Key)
		}
		seen[entry.Key] = true
	}
	return validateEncodedSize("metadata", entries, MaxMetadataBytes)
}

func validateReferences(refs []MessageReference) error {
	seen := map[string]bool{}
	for i, ref := range refs {
		if err := requiredText(fmt.Sprintf("references[%d].kind", i), ref.Kind); err != nil {
			return err
		}
		if err := requiredText(fmt.Sprintf("references[%d].id", i), ref.ID); err != nil {
			return err
		}
		if err := optionalText(fmt.Sprintf("references[%d].label", i), ref.Label); err != nil {
			return err
		}
		key := ref.Kind + "\x00" + ref.ID
		if seen[key] {
			return fmt.Errorf("duplicate message reference %q", ref.ID)
		}
		seen[key] = true
	}
	return nil
}

func messageBody(value string) error {
	if !utf8.ValidString(value) || len(value) > MaxBodyBytes || strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
		return fmt.Errorf("body must contain valid nonempty UTF-8 text of at most %d bytes", MaxBodyBytes)
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return fmt.Errorf("body contains an unsupported control character")
		}
	}
	return nil
}

func requiredText(name, value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s is required and must not have surrounding whitespace", name)
	}
	return optionalText(name, value)
}

func optionalText(name, value string) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s must be valid UTF-8 text without control characters", name)
	}
	return nil
}

func rfc3339(name, value string) error {
	_, err := parseRFC3339(name, value)
	return err
}

func parseRFC3339(name, value string) (time.Time, error) {
	if err := requiredText(name, value); err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", name, err)
	}
	return parsed, nil
}

func validateEncodedSize(name string, value any, max int) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	if len(raw) > max {
		return fmt.Errorf("%s exceeds %d encoded bytes", name, max)
	}
	return nil
}

func validateRequest(err error, value any) error {
	if err != nil {
		return err
	}
	return validateEncodedSize("payload", value, MaxEnvelopeBytes)
}

func validateResultStatus(status OperationStatus, value any, name string, max int) (bool, error) {
	if err := status.Validate(); err != nil {
		return true, err
	}
	if !status.OK {
		return true, validateEncodedSize(name, value, max)
	}
	return false, nil
}
