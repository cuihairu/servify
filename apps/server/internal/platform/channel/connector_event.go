package channel

import (
	"errors"
	"fmt"
	"time"
)

// ConnectorEvent is the contract's normalized inbound event
// (docs/chat-platform-integration-contract.md). The contract names this shape
// InboundEvent; the package-level generic InboundEvent predates the contract
// and serves the email transport surface, so the connector-layer event carries
// a distinct Go name. Connectors translate provider payloads into this shape
// and hand it to an inbox sink; nothing platform-specific survives the
// translation.

// ConversationRef identifies the chat the event belongs to, resolved from the
// platform's conversation object.
type ConversationRef struct {
	PlatformID string
	Kind       string // "dm" | "group" | "channel" ...
	Name       string
	IsGroup    bool
	ThreadID   string
}

// SenderRef identifies who produced the event on the platform. ServifyActorID
// stays empty until the sender is linked to a local actor.
type SenderRef struct {
	PlatformID     string
	DisplayName    string
	IsBot          bool
	ServifyActorID string
}

// ContentPart is one part of a message body: inline text or a restricted
// reference to platform-held file content. DownloadRef is a connector-scoped
// handle, never a public URL.
type ContentPart struct {
	Kind           string // "text" | "file" | ...
	Text           string
	MimeType       string
	PlatformFileID string
	DownloadRef    string
	SizeBytes      int64
	Checksum       string
}

// MessageContent is the message body carried by message lifecycle events. It
// is a value snapshot of the platform state at OccurredAt; edits and deletions
// arrive as their own events instead of rewriting this snapshot.
type MessageContent struct {
	PlatformID   string
	Kind         string // "text" | "rich" | ...
	Text         string
	ContentParts []ContentPart
	EditedAt     time.Time
	DeletedAt    time.Time
}

// RawPayloadRef points at the locally encrypted copy of the raw platform
// payload, kept for triage within the contract's retention window.
type RawPayloadRef struct {
	Store     string
	ObjectID  string
	SHA256    string
	ExpiresAt time.Time
}

// ConnectorEvent is one platform event normalized into contract vocabulary.
type ConnectorEvent struct {
	Platform           string
	InstallationID     string
	TenantID           string
	WorkspaceID        string
	EventID            string
	IdempotencyKey     string // must equal InboundIdempotencyKey(InstallationID, EventID)
	EventType          EventType
	OccurredAt         time.Time
	ReceivedAt         time.Time
	Conversation       ConversationRef
	Sender             SenderRef
	Message            *MessageContent // required for message.* events, nil otherwise
	ParentMessageID    string
	ReplyToMessageID   string
	RawPayloadRef      *RawPayloadRef    // optional; set when the raw payload is retained
	PlatformExtensions map[string]string // namespaced platform-specific fields
}

// Validate enforces the contract's envelope rules: identifiers and scopes are
// present, the idempotency key matches its composition, timestamps are
// observed, and message lifecycle events carry a message payload. Unknown
// event types are structurally valid — they are routed to quarantine by
// QuarantineRequired, not rejected here.
func (ev ConnectorEvent) Validate() error {
	if ev.Platform == "" {
		return errors.New("connector event requires platform")
	}
	if ev.InstallationID == "" {
		return errors.New("connector event requires installation id")
	}
	if ev.TenantID == "" {
		return errors.New("connector event requires tenant scope")
	}
	if ev.WorkspaceID == "" {
		return errors.New("connector event requires workspace scope")
	}
	if ev.EventID == "" {
		return errors.New("connector event requires event id")
	}
	if want := InboundIdempotencyKey(ev.InstallationID, ev.EventID); ev.IdempotencyKey != want {
		return fmt.Errorf("idempotency key mismatch: got %q, want installation+event composition", ev.IdempotencyKey)
	}
	if ev.EventType == "" {
		return errors.New("connector event requires event type")
	}
	if ev.OccurredAt.IsZero() {
		return errors.New("connector event requires occurred_at")
	}
	if ev.ReceivedAt.IsZero() {
		return errors.New("connector event requires received_at")
	}
	if ev.Conversation.PlatformID == "" {
		return errors.New("connector event requires conversation platform id")
	}
	if ev.Sender.PlatformID == "" {
		return errors.New("connector event requires sender platform id")
	}
	switch ev.EventType {
	case EventTypeMessageCreated, EventTypeMessageEdited, EventTypeMessageDeleted:
		if ev.Message == nil {
			return fmt.Errorf("%s requires a message payload", ev.EventType)
		}
		if ev.Message.PlatformID == "" {
			return errors.New("message payload requires platform id")
		}
	}
	if ref := ev.RawPayloadRef; ref != nil {
		if ref.Store == "" {
			return errors.New("raw payload ref requires store")
		}
		if ref.ObjectID == "" {
			return errors.New("raw payload ref requires object id")
		}
	}
	return nil
}

// QuarantineRequired reports whether the event must bypass normal processing
// and land in the triage quarantine: unknown event types are stored and
// acknowledged, never silently dropped or force-interpreted.
func (ev ConnectorEvent) QuarantineRequired() bool {
	return ev.EventType == EventTypeUnknown
}

// AsQuarantined rewrites a structurally valid event into the quarantine lane
// without losing its identity or source evidence: the event type becomes
// unknown (so the inbox sink stores it as quarantined), the message payload is
// dropped (unknown events carry no message), and everything else — scope,
// timestamps, conversation/sender references and the raw payload ref — is
// preserved so triage can still tell what the platform sent and from where.
// Callers use it for the per-conversation authorization gate: an event from a
// chat with no approved grant must never reach normal processing, but it also
// must not be silently dropped.
func (ev ConnectorEvent) AsQuarantined() ConnectorEvent {
	ev.EventType = EventTypeUnknown
	ev.Message = nil
	return ev
}
