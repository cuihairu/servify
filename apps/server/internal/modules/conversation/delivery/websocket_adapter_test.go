package delivery

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"

	"gorm.io/gorm"
)

type stubConversationRepo struct {
	conversations map[string]*conversationdomain.Conversation
	messages      map[string][]conversationdomain.ConversationMessage
}

func (s *stubConversationRepo) ListMessagesAfter(ctx context.Context, conversationID string, afterMessageID string, limit int) ([]conversationdomain.ConversationMessage, error) {
	items := s.messages[conversationID]
	out := make([]conversationdomain.ConversationMessage, 0, len(items))
	return append(out, items...), nil
}

func (s *stubConversationRepo) ListSessions(ctx context.Context, query conversationapp.OpenSessionListQuery) ([]conversationdomain.Conversation, int64, error) {
	return nil, 0, nil
}

func (s *stubConversationRepo) CreateConversation(ctx context.Context, conversation *conversationdomain.Conversation) error {
	if s.conversations == nil {
		s.conversations = map[string]*conversationdomain.Conversation{}
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *stubConversationRepo) GetConversation(ctx context.Context, conversationID string) (*conversationdomain.Conversation, error) {
	item, ok := s.conversations[conversationID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *item
	return &cp, nil
}

func (s *stubConversationRepo) UpdateConversation(ctx context.Context, conversation *conversationdomain.Conversation) error {
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *stubConversationRepo) AppendMessage(ctx context.Context, message *conversationdomain.ConversationMessage) error {
	if s.messages == nil {
		s.messages = map[string][]conversationdomain.ConversationMessage{}
	}
	s.messages[message.ConversationID] = append(s.messages[message.ConversationID], *message)
	return nil
}

func (s *stubConversationRepo) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]conversationdomain.ConversationMessage, error) {
	items := s.messages[conversationID]
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]conversationdomain.ConversationMessage, 0, len(items))
	out = append(out, items...)
	return out, nil
}

func (s *stubConversationRepo) ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]conversationdomain.ConversationMessage, error) {
	return s.ListRecentMessages(ctx, conversationID, limit)
}

func TestWebSocketMessageAdapterCreatesConversationOnDemand(t *testing.T) {
	repo := &stubConversationRepo{}
	adapter := NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))

	if err := adapter.PersistTextMessage(context.Background(), "sess-1", "hello"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := repo.conversations["sess-1"]; !ok {
		t.Fatal("expected conversation to be created")
	}
	if len(repo.messages["sess-1"]) != 1 || repo.messages["sess-1"][0].Content != "hello" {
		t.Fatalf("expected one persisted message, got %+v", repo.messages["sess-1"])
	}
}

func TestWebSocketMessageAdapterHasActiveHumanAgent(t *testing.T) {
	agentID := uint(9)
	repo := &stubConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {
				ID:     "sess-1",
				Status: conversationdomain.ConversationStatusActive,
				Participants: []conversationdomain.Participant{
					{ID: "agent:9", UserID: &agentID, Role: conversationdomain.ParticipantRoleAgent},
				},
			},
		},
	}
	adapter := NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))

	assigned, err := adapter.HasActiveHumanAgent(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !assigned {
		t.Fatal("expected active human agent to be detected")
	}
}

func TestWebSocketMessageAdapterListRecentMessages(t *testing.T) {
	now := time.Now()
	repo := &stubConversationRepo{
		messages: map[string][]conversationdomain.ConversationMessage{
			"sess-1": {
				{ID: "1", ConversationID: "sess-1", Sender: conversationdomain.ParticipantRoleCustomer, Kind: conversationdomain.MessageKindText, Content: "hello", CreatedAt: now},
			},
		},
	}
	adapter := NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))

	items, err := adapter.ListRecentMessages(context.Background(), "sess-1", 10)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one message, got %+v", items)
	}
	want := models.Message{
		SessionID: "sess-1",
		Content:   "hello",
		Type:      "text",
		Sender:    "customer",
		CreatedAt: now,
	}
	if items[0].SessionID != want.SessionID || items[0].Content != want.Content || items[0].Type != want.Type || items[0].Sender != want.Sender {
		t.Fatalf("unexpected mapped history: %+v", items[0])
	}
}

func TestHandlerServiceAdapterGetConversationFallsBackToMessageHistory(t *testing.T) {
	now := time.Now()
	repo := &stubConversationRepo{
		messages: map[string][]conversationdomain.ConversationMessage{
			"sess-fallback": {
				{ID: "1", ConversationID: "sess-fallback", Sender: conversationdomain.ParticipantRoleCustomer, Kind: conversationdomain.MessageKindText, Content: "hello", CreatedAt: now},
			},
		},
	}

	handler := NewHandlerService(conversationapp.NewService(repo, nil))
	dto, err := handler.GetConversation(context.Background(), "sess-fallback")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if dto == nil {
		t.Fatal("expected synthesized conversation dto")
	}
	if dto.ID != "sess-fallback" {
		t.Fatalf("conversation id = %q want sess-fallback", dto.ID)
	}
	if dto.Status != string(conversationdomain.ConversationStatusActive) {
		t.Fatalf("status = %q want %q", dto.Status, conversationdomain.ConversationStatusActive)
	}
	if dto.Channel.Channel != "web" || dto.Channel.SessionID != "sess-fallback" {
		t.Fatalf("unexpected channel: %+v", dto.Channel)
	}
	if dto.LastMessageAt == nil || !dto.LastMessageAt.Equal(now) {
		t.Fatalf("last_message_at = %+v want %v", dto.LastMessageAt, now)
	}
}

func TestHandlerServiceAdapterListMessagesReturnsNotFoundForMissingConversation(t *testing.T) {
	handler := NewHandlerService(conversationapp.NewService(&stubConversationRepo{}, nil))

	_, err := handler.ListMessages(context.Background(), "missing", 10)
	if err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}
}

func TestHandlerServiceAdapterListMessagesBeforeReturnsNotFoundForMissingConversation(t *testing.T) {
	handler := NewHandlerService(conversationapp.NewService(&stubConversationRepo{}, nil))

	_, err := handler.ListMessagesBefore(context.Background(), "missing", "1", 10)
	if err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}
}
