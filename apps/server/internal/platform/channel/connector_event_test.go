package channel

import (
	"testing"
	"time"
)

// validBaseEvent returns an event that satisfies every contract rule, so each
// failure case can break exactly one of them.
func validBaseEvent() ConnectorEvent {
	occurred := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	return ConnectorEvent{
		Platform:       "memex",
		InstallationID: "inst-1",
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
		EventID:        "evt-9",
		IdempotencyKey: InboundIdempotencyKey("inst-1", "evt-9"),
		EventType:      EventTypeMessageCreated,
		OccurredAt:     occurred,
		ReceivedAt:     occurred.Add(time.Second),
		Conversation: ConversationRef{
			PlatformID: "grp-1",
			Kind:       "group",
			IsGroup:    true,
		},
		Sender: SenderRef{
			PlatformID:  "user-1",
			DisplayName: "Ada",
		},
		Message: &MessageContent{
			PlatformID: "msg-1",
			Kind:       "text",
			Text:       "hi",
		},
	}
}

func TestConnectorEventValidates(t *testing.T) {
	for _, eventType := range []EventType{EventTypeMessageCreated, EventTypeMessageEdited, EventTypeMessageDeleted} {
		ev := validBaseEvent()
		ev.EventType = eventType
		if err := ev.Validate(); err != nil {
			t.Errorf("%s with a message payload must validate: %v", eventType, err)
		}
	}
	for _, eventType := range []EventType{EventTypeMemberChanged, EventTypeReactionChanged, EventTypeUnknown} {
		ev := validBaseEvent()
		ev.EventType = eventType
		ev.Message = nil // lifecycle events without a message body are legal
		if err := ev.Validate(); err != nil {
			t.Errorf("%s without a message payload must validate: %v", eventType, err)
		}
	}
}

func TestConnectorEventValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*ConnectorEvent)
	}{
		{"missing platform", func(ev *ConnectorEvent) { ev.Platform = "" }},
		{"missing installation id", func(ev *ConnectorEvent) { ev.InstallationID = "" }},
		{"missing tenant scope", func(ev *ConnectorEvent) { ev.TenantID = "" }},
		{"missing workspace scope", func(ev *ConnectorEvent) { ev.WorkspaceID = "" }},
		{"missing event id", func(ev *ConnectorEvent) { ev.EventID = "" }},
		{"idempotency key mismatch", func(ev *ConnectorEvent) { ev.IdempotencyKey = "stale-key" }},
		{"missing event type", func(ev *ConnectorEvent) { ev.EventType = "" }},
		{"zero occurred at", func(ev *ConnectorEvent) { ev.OccurredAt = time.Time{} }},
		{"zero received at", func(ev *ConnectorEvent) { ev.ReceivedAt = time.Time{} }},
		{"missing conversation id", func(ev *ConnectorEvent) { ev.Conversation.PlatformID = "" }},
		{"missing sender id", func(ev *ConnectorEvent) { ev.Sender.PlatformID = "" }},
		{"created without message", func(ev *ConnectorEvent) { ev.Message = nil }},
		{"edited without message", func(ev *ConnectorEvent) { ev.Message = nil; ev.EventType = EventTypeMessageEdited }},
		{"deleted without message", func(ev *ConnectorEvent) { ev.Message = nil; ev.EventType = EventTypeMessageDeleted }},
		{"message without platform id", func(ev *ConnectorEvent) { ev.Message.PlatformID = "" }},
		{"raw payload ref without store", func(ev *ConnectorEvent) {
			ev.RawPayloadRef = &RawPayloadRef{ObjectID: "obj-1"}
		}},
		{"raw payload ref without object id", func(ev *ConnectorEvent) {
			ev.RawPayloadRef = &RawPayloadRef{Store: "local"}
		}},
	}
	for _, tc := range cases {
		ev := validBaseEvent()
		tc.mut(&ev)
		if err := ev.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", tc.name)
		}
	}
}

func TestConnectorEventQuarantineRequired(t *testing.T) {
	unknown := validBaseEvent()
	unknown.EventType = EventTypeUnknown
	unknown.Message = nil
	if !unknown.QuarantineRequired() {
		t.Error("unknown event types must be flagged for quarantine")
	}
	for _, eventType := range []EventType{
		EventTypeMessageCreated,
		EventTypeMemberChanged,
		EventTypeReactionChanged,
	} {
		ev := validBaseEvent()
		ev.EventType = eventType
		if ev.QuarantineRequired() {
			t.Errorf("%s must not be flagged for quarantine", eventType)
		}
	}
}
