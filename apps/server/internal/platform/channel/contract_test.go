package channel

import (
	"testing"
	"time"
)

func TestContractEventTypeValues(t *testing.T) {
	cases := map[EventType]string{
		EventTypeMessageCreated:  "message.created",
		EventTypeMessageEdited:   "message.edited",
		EventTypeMessageDeleted:  "message.deleted",
		EventTypeReactionChanged: "reaction.changed",
		EventTypeMemberChanged:   "member.changed",
		EventTypeUnknown:         "unknown",
	}
	for et, want := range cases {
		if string(et) != want {
			t.Errorf("EventType %q does not match contract vocabulary %q", string(et), want)
		}
	}
}

func TestContractReceiveModeAndMessageScopeValues(t *testing.T) {
	modes := map[ReceiveMode]string{
		ReceiveModePolling: "polling",
		ReceiveModeWebhook: "webhook",
		ReceiveModeStream:  "stream",
		ReceiveModeUnknown: "unknown",
	}
	for mode, want := range modes {
		if string(mode) != want {
			t.Errorf("ReceiveMode %q does not match contract vocabulary %q", string(mode), want)
		}
	}
	scopes := map[MessageScope]string{
		MessageScopeMentionOnly:         "mention_only",
		MessageScopeAllAuthorizedGroups: "all_authorized_groups",
		MessageScopeUnknown:             "unknown",
	}
	for scope, want := range scopes {
		if string(scope) != want {
			t.Errorf("MessageScope %q does not match contract vocabulary %q", string(scope), want)
		}
	}
}

func TestCapabilitiesIngestionEnabled(t *testing.T) {
	verified := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	base := Capabilities{
		ReceiveMode:  ReceiveModePolling,
		MessageScope: MessageScopeAllAuthorizedGroups,
		VerifiedAt:   verified,
	}
	if !base.IngestionEnabled() {
		t.Fatal("verified snapshot with known mode and scope must enable ingestion")
	}
	mention := base
	mention.MessageScope = MessageScopeMentionOnly
	if !mention.IngestionEnabled() {
		t.Error("mention_only is a known scope and must keep ingestion enabled")
	}
	disablers := []struct {
		name string
		mut  func(*Capabilities)
	}{
		{"never verified", func(c *Capabilities) { c.VerifiedAt = time.Time{} }},
		{"unknown receive mode", func(c *Capabilities) { c.ReceiveMode = ReceiveModeUnknown }},
		{"unset receive mode", func(c *Capabilities) { c.ReceiveMode = "" }},
		{"unknown message scope", func(c *Capabilities) { c.MessageScope = MessageScopeUnknown }},
		{"unset message scope", func(c *Capabilities) { c.MessageScope = "" }},
	}
	for _, tc := range disablers {
		c := base
		tc.mut(&c)
		if c.IngestionEnabled() {
			t.Errorf("%s: any unknown or unobserved permission must keep ingestion disabled", tc.name)
		}
	}
}

func TestAckDecisionValues(t *testing.T) {
	cases := map[AckDecision]string{
		AckCommit:     "commit",
		AckDuplicate:  "duplicate",
		AckQuarantine: "quarantine",
	}
	for decision, want := range cases {
		if string(decision) != want {
			t.Errorf("AckDecision %q does not match contract vocabulary %q", string(decision), want)
		}
	}
}

func TestInboundIdempotencyKey(t *testing.T) {
	if got, want := InboundIdempotencyKey("inst-1", "evt-9"), "inst-1\x1fevt-9"; got != want {
		t.Errorf("InboundIdempotencyKey = %q, want %q", got, want)
	}
	if InboundIdempotencyKey("ab", "c") == InboundIdempotencyKey("a", "bc") {
		t.Error("composition must be unambiguous for split-identical inputs")
	}
}
