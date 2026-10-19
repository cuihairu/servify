package channel

import "time"

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
