package channel

import (
	"context"
	"errors"
	"strings"
	"time"
)

// This file pins the chat platform integration contract
// (docs/chat-platform-integration-contract.md) at the connector layer. The
// generic Adapter / InboundEvent transport surface above serves the email
// channel; these declarations model the platform-connector layer on top of it
// and stay free of provider DTOs.

// EventType is the inbound lifecycle event vocabulary of the contract. Edits
// and deletions are ordered events, never implicit overwrites of a chat record.
type EventType string

const (
	EventTypeMessageCreated  EventType = "message.created"
	EventTypeMessageEdited   EventType = "message.edited"
	EventTypeMessageDeleted  EventType = "message.deleted"
	EventTypeReactionChanged EventType = "reaction.changed"
	EventTypeMemberChanged   EventType = "member.changed"
	EventTypeUnknown         EventType = "unknown"
)

// ReceiveMode is how a connector obtains inbound events.
type ReceiveMode string

const (
	ReceiveModePolling ReceiveMode = "polling"
	ReceiveModeWebhook ReceiveMode = "webhook"
	ReceiveModeStream  ReceiveMode = "stream"
	ReceiveModeUnknown ReceiveMode = "unknown"
)

// MessageScope is the effective read scope granted to one bot installation.
type MessageScope string

const (
	MessageScopeMentionOnly         MessageScope = "mention_only"
	MessageScopeAllAuthorizedGroups MessageScope = "all_authorized_groups"
	MessageScopeUnknown             MessageScope = "unknown"
)

// Capabilities is an observed snapshot of a platform grant for one installation
// and group. It comes from the platform's authorization result, never from an
// operator's unverified claim, and any unknown permission keeps ingestion
// disabled.
type Capabilities struct {
	ReceiveMode  ReceiveMode
	MessageScope MessageScope
	HistoryRead  bool
	EventTypes   []EventType
	Threads      bool
	Replies      bool
	Attachments  bool
	Edits        bool
	Deletes      bool
	Reactions    bool
	SendText     bool
	SendRich     bool
	RateLimit    string
	VerifiedAt   time.Time // zero: never observed from the platform
}

// IngestionEnabled reports whether the snapshot is safe to ingest from: it must
// be observed from a live grant (non-zero VerifiedAt) and carry no unknown
// receive mode or message scope. An unset or unknown permission keeps ingestion
// off until an operator records the effective grant.
func (c Capabilities) IngestionEnabled() bool {
	if c.VerifiedAt.IsZero() {
		return false
	}
	if c.ReceiveMode == "" || c.ReceiveMode == ReceiveModeUnknown {
		return false
	}
	if c.MessageScope == "" || c.MessageScope == MessageScopeUnknown {
		return false
	}
	return true
}

// AckDecision is the connector inbox verdict for one inbound event. An inbound
// ACK is allowed only after durable inbox persistence or a recognized duplicate.
type AckDecision string

const (
	AckCommit     AckDecision = "commit"     // durably persisted; safe to acknowledge the platform
	AckDuplicate  AckDecision = "duplicate"  // idempotency key already accepted; acknowledge as a no-op
	AckQuarantine AckDecision = "quarantine" // unknown/unsupported payload stored for triage; acknowledge
)

// InboundIdempotencyKey composes the contract's dedup key from the installation
// id and the platform event id. The unit separator keeps the composition
// unambiguous (("ab","c") and ("a","bc") never collide).
func InboundIdempotencyKey(installationID, eventID string) string {
	return installationID + "\x1f" + eventID
}

// SendResultStatus is the contract's outbound delivery verdict. accepted means
// the platform acknowledged the message; unknown means the platform may or may
// not have accepted (timeout after request left, no safe dedup) and the caller
// must not retry blindly; rejected means the platform refused it. The adapter
// maps permanent/invalid failures to rejected and ambiguous delivery to
// unknown — a caller escalates neither without an operator policy.
type SendResultStatus string

const (
	SendResultAccepted SendResultStatus = "accepted"
	SendResultUnknown  SendResultStatus = "unknown"
	SendResultRejected SendResultStatus = "rejected"
)

// SendContent is the structured outbound body. Text is the plain rendering;
// ContentParts carries non-text parts (attachments, file references) for
// adapters that observe SendRich. The platform-facing adapter always renders
// the text it actually supports and never silently drops a part it cannot send.
type SendContent struct {
	Text         string
	ContentParts []ContentPart
}

// SendRequest is one outbound message to a conversation, optionally in a
// thread and replying to a platform message (the reply chain). IdempotencyKey
// is the caller's stable retry anchor — outbound retries MUST reuse it — and
// Deadline bounds the delivery attempt.
type SendRequest struct {
	ConversationID   string // platform conversation id
	ThreadID         string // platform thread/topic id (forum topics); empty = none
	ReplyToMessageID string // platform message being replied to; empty = new message
	IdempotencyKey   string
	Deadline         time.Time // zero: adapter default
	Content          SendContent
}

// Validate enforces the send envelope: a conversation and stable dedup key are
// required and the body must carry text or content parts. A missing content or
// key is a caller bug, not a platform error.
func (r SendRequest) Validate() error {
	if strings.TrimSpace(r.ConversationID) == "" {
		return errors.New("send request requires conversation id")
	}
	if r.IdempotencyKey == "" {
		return errors.New("send request requires idempotency key")
	}
	return validateSendContent(r.Content)
}

// NotifyRequest is one operator/system announcement with an explicit target
// scope and an audit reason. Target is pinned to the storage-scoped recipient
// (the installation's conversation); the adapter never broadens the audience
// implicitly, and Reason is the audit trail entry for why the announcement ran.
type NotifyRequest struct {
	Target         string // explicit recipient scope (platform chat id)
	Reason         string // audit reason for the announcement
	IdempotencyKey string
	Deadline       time.Time // zero: adapter default
	Content        SendContent
}

// Validate enforces the notification envelope: an explicit target, a stable
// dedup key and a body are required.
func (r NotifyRequest) Validate() error {
	if strings.TrimSpace(r.Target) == "" {
		return errors.New("notify request requires target")
	}
	if r.IdempotencyKey == "" {
		return errors.New("notify request requires idempotency key")
	}
	return validateSendContent(r.Content)
}

func validateSendContent(c SendContent) error {
	if c.Text == "" && len(c.ContentParts) == 0 {
		return errors.New("send content requires text or content parts")
	}
	return nil
}

// SendResult is the contract's outbound response: the platform message id (for
// accepted sends; empty otherwise), the acceptance observation time, and the
// delivery verdict. Unknown keeps the caller's idempotency key usable for a
// later reconciliation, never a blind immediate retry.
type SendResult struct {
	PlatformMessageID string
	AcceptedAt        time.Time
	Status            SendResultStatus
}

// Validate enforces the result envelope against the contract vocabulary.
func (r SendResult) Validate() error {
	switch r.Status {
	case SendResultAccepted:
		if r.PlatformMessageID == "" {
			return errors.New("accepted send result requires platform message id")
		}
		if r.AcceptedAt.IsZero() {
			return errors.New("accepted send result requires accepted_at")
		}
	case SendResultUnknown, SendResultRejected:
		if r.PlatformMessageID != "" {
			return errors.New("non-accepted send result must not carry platform message id")
		}
	default:
		return errors.New("send result requires a known status")
	}
	return nil
}

// Connector is the contract's platform-independent connector surface
// (docs/chat-platform-integration-contract.md). Capabilities observes a live
// grant; Receive streams/polls into a sink that must persist before ACK/cursor
// advance; Send addresses a conversation with optional thread/reply; Notify is
// an operator/system announcement with explicit scope and audit reason. Both
// outbound calls accept a caller idempotency key, deadline and structured
// content, and return the platform message id, accepted time and delivery
// status.
type Connector interface {
	Capabilities(ctx context.Context, installationID string) (Capabilities, error)
	Receive(ctx context.Context, sink InboundSink) error
	Send(ctx context.Context, req SendRequest) (SendResult, error)
	Notify(ctx context.Context, req NotifyRequest) (SendResult, error)
}

// InboundSink is the contract's durable inbound acceptance seam. A connector
// must not ACK or advance the platform cursor before Accept returns a verdict
// after durable persistence (AckCommit / AckQuarantine) or after a recognized
// duplicate (AckDuplicate).
type InboundSink interface {
	Accept(ctx context.Context, event ConnectorEvent) (AckDecision, error)
}
