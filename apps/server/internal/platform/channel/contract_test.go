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

func TestContractSendResultStatusValues(t *testing.T) {
	cases := map[SendResultStatus]string{
		SendResultAccepted: "accepted",
		SendResultUnknown:  "unknown",
		SendResultRejected: "rejected",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Errorf("SendResultStatus %q does not match contract vocabulary %q", string(status), want)
		}
	}
}

func TestSendRequestValidate(t *testing.T) {
	base := SendRequest{
		ConversationID: "-100123",
		IdempotencyKey: "out-1",
		Content:        SendContent{Text: "hello"},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid send request rejected: %v", err)
	}
	reply := base
	reply.ReplyToMessageID = "11"
	if err := reply.Validate(); err != nil {
		t.Errorf("send request with reply chain rejected: %v", err)
	}
	withParts := base
	withParts.Content = SendContent{ContentParts: []ContentPart{{Kind: "text", Text: "hi"}}}
	if err := withParts.Validate(); err != nil {
		t.Errorf("send request with content parts rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*SendRequest)
	}{
		{"missing conversation", func(r *SendRequest) { r.ConversationID = "" }},
		{"missing idempotency key", func(r *SendRequest) { r.IdempotencyKey = "" }},
		{"empty content", func(r *SendRequest) { r.Content = SendContent{} }},
	}
	for _, tc := range cases {
		r := base
		tc.mut(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: empty envelope must be rejected", tc.name)
		}
	}
}

func TestNotifyRequestValidate(t *testing.T) {
	base := NotifyRequest{
		Target:         "-100123",
		Reason:         "phase-0 announcement",
		IdempotencyKey: "out-2",
		Content:        SendContent{Text: "announcement"},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid notify request rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*NotifyRequest)
	}{
		{"missing target", func(r *NotifyRequest) { r.Target = "" }},
		{"missing idempotency key", func(r *NotifyRequest) { r.IdempotencyKey = "" }},
		{"empty content", func(r *NotifyRequest) { r.Content = SendContent{} }},
	}
	for _, tc := range cases {
		r := base
		tc.mut(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: empty envelope must be rejected", tc.name)
		}
	}
}

func TestSendResultValidate(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	accepted := SendResult{PlatformMessageID: "77", AcceptedAt: now, Status: SendResultAccepted}
	if err := accepted.Validate(); err != nil {
		t.Fatalf("accepted result rejected: %v", err)
	}
	unknown := SendResult{Status: SendResultUnknown}
	if err := unknown.Validate(); err != nil {
		t.Fatalf("unknown result rejected: %v", err)
	}
	rejected := SendResult{Status: SendResultRejected}
	if err := rejected.Validate(); err != nil {
		t.Fatalf("rejected result rejected: %v", err)
	}
	if err := (SendResult{PlatformMessageID: "77", Status: SendResultAccepted}).Validate(); err == nil {
		t.Error("accepted result without accepted_at must be rejected")
	}
	if err := (SendResult{Status: SendResultAccepted, AcceptedAt: now}).Validate(); err == nil {
		t.Error("accepted result without platform message id must be rejected")
	}
	if err := (SendResult{PlatformMessageID: "77", Status: SendResultUnknown}).Validate(); err == nil {
		t.Error("non-accepted result carrying a message id must be rejected")
	}
	if err := (SendResult{Status: SendResultStatus("queued")}).Validate(); err == nil {
		t.Error("unknown status must be rejected")
	}
}
