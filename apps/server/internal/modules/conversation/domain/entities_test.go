package domain

import (
	"testing"
	"time"
)

func TestConversationConstants(t *testing.T) {
	if ConversationStatusActive != "active" || ConversationStatusWaitingHuman != "waiting_human" ||
		ConversationStatusTransferred != "transferred" || ConversationStatusClosed != "closed" {
		t.Fatalf("unexpected conversation status constants")
	}
	if ParticipantRoleCustomer != "customer" || ParticipantRoleAgent != "agent" ||
		ParticipantRoleAI != "ai" || ParticipantRoleSystem != "system" {
		t.Fatalf("unexpected participant role constants")
	}
	if MessageKindText != "text" || MessageKindSystem != "system" {
		t.Fatalf("unexpected message kind constants")
	}
}

func TestConversationStructFields(t *testing.T) {
	userID := uint(7)
	now := time.Now()
	lastMessageAt := now.Add(time.Minute)
	endedAt := now.Add(2 * time.Minute)

	binding := ChannelBinding{
		Channel:     "web",
		ExternalID:  "ext-1",
		SessionID:   "sess-1",
		WorkspaceID: "ws-1",
		Protocol:    "telegram",
		ProtocolRef: "ref-1",
	}
	participant := Participant{ID: "u-1", UserID: &userID, Role: ParticipantRoleCustomer, DisplayName: "Alice"}
	conversation := Conversation{
		ID:            "conv-1",
		CustomerID:    &userID,
		Status:        ConversationStatusWaitingHuman,
		Subject:       "billing",
		Channel:       binding,
		Participants:  []Participant{participant},
		StartedAt:     now,
		LastMessageAt: &lastMessageAt,
		EndedAt:       &endedAt,
	}

	if conversation.ID != "conv-1" || conversation.CustomerID == nil || *conversation.CustomerID != userID {
		t.Fatalf("unexpected conversation header: %+v", conversation)
	}
	if conversation.Status != ConversationStatusWaitingHuman || conversation.Subject != "billing" {
		t.Fatalf("unexpected conversation state: %+v", conversation)
	}
	if conversation.Channel.ExternalID != "ext-1" || conversation.Channel.Protocol != "telegram" || conversation.Channel.ProtocolRef != "ref-1" {
		t.Fatalf("unexpected channel binding: %+v", conversation.Channel)
	}
	if conversation.Participants[0].ID != "u-1" || conversation.Participants[0].DisplayName != "Alice" {
		t.Fatalf("unexpected participants: %+v", conversation.Participants)
	}
	if !conversation.StartedAt.Equal(now) || !conversation.LastMessageAt.Equal(lastMessageAt) || !conversation.EndedAt.Equal(endedAt) {
		t.Fatalf("unexpected conversation timestamps: %+v", conversation)
	}
}

func TestConversationMessageStructFields(t *testing.T) {
	now := time.Now()
	message := ConversationMessage{
		ID:             "msg-1",
		ConversationID: "conv-1",
		Sender:         ParticipantRoleAgent,
		Kind:           MessageKindSystem,
		Content:        "hello",
		Metadata:       map[string]string{"source": "test"},
		CreatedAt:      now,
	}

	if message.ID != "msg-1" || message.ConversationID != "conv-1" {
		t.Fatalf("unexpected message header: %+v", message)
	}
	if message.Sender != ParticipantRoleAgent || message.Kind != MessageKindSystem || message.Content != "hello" {
		t.Fatalf("unexpected message body: %+v", message)
	}
	if message.Metadata["source"] != "test" || !message.CreatedAt.Equal(now) {
		t.Fatalf("unexpected message metadata/timestamp: %+v", message)
	}
}
