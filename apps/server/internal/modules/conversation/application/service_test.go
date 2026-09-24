package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/modules/conversation/domain"
	"servify/apps/server/internal/platform/eventbus"
)

type stubConversationRepo struct {
	conversations map[string]*domain.Conversation
	messages      map[string][]domain.ConversationMessage
	sessions      []domain.Conversation
	err           error
}

func (s *stubConversationRepo) CreateConversation(ctx context.Context, conversation *domain.Conversation) error {
	if s.err != nil {
		return s.err
	}
	if s.conversations == nil {
		s.conversations = map[string]*domain.Conversation{}
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *stubConversationRepo) GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	if s.err != nil {
		return nil, s.err
	}
	item, ok := s.conversations[conversationID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *item
	return &cp, nil
}

func (s *stubConversationRepo) UpdateConversation(ctx context.Context, conversation *domain.Conversation) error {
	if s.err != nil {
		return s.err
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *stubConversationRepo) AppendMessage(ctx context.Context, message *domain.ConversationMessage) error {
	if s.err != nil {
		return s.err
	}
	if s.messages == nil {
		s.messages = map[string][]domain.ConversationMessage{}
	}
	s.messages[message.ConversationID] = append(s.messages[message.ConversationID], *message)
	return nil
}

func (s *stubConversationRepo) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]domain.ConversationMessage, error) {
	items := s.messages[conversationID]
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]domain.ConversationMessage, 0, len(items))
	out = append(out, items...)
	return out, nil
}

func (s *stubConversationRepo) ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]domain.ConversationMessage, error) {
	items := s.messages[conversationID]
	if beforeMessageID != "" {
		cutoff := -1
		for i, m := range items {
			if m.ID == beforeMessageID {
				cutoff = i
				break
			}
		}
		if cutoff > 0 {
			items = items[:cutoff]
		} else if cutoff == 0 {
			return []domain.ConversationMessage{}, nil
		}
	}
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	out := make([]domain.ConversationMessage, 0, len(items))
	out = append(out, items...)
	return out, nil
}

func (s *stubConversationRepo) ListMessagesAfter(ctx context.Context, conversationID string, afterMessageID string, limit int) ([]domain.ConversationMessage, error) {
	items := s.messages[conversationID]
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]domain.ConversationMessage, 0, len(items))
	out = append(out, items...)
	return out, nil
}

func (s *stubConversationRepo) ListSessions(ctx context.Context, query OpenSessionListQuery) ([]domain.Conversation, int64, error) {
	return s.sessions, int64(len(s.sessions)), nil
}

func (s *stubConversationRepo) MarkVisitorRead(ctx context.Context, conversationID, messageID string) error {
	return s.err
}

func (s *stubConversationRepo) VisitorUnreadCount(ctx context.Context, conversationID string) (int64, string, error) {
	return 0, "0", s.err
}

type stubConversationPublisher struct {
	events []eventbus.Event
	err    error
}

func (s *stubConversationPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func TestServiceCreateConversationPublishesEvent(t *testing.T) {
	repo := &stubConversationRepo{}
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)
	now := time.Now()
	svc.now = func() time.Time { return now }

	customerID := uint(7)
	got, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
		ConversationID: "conv-1",
		CustomerID:     &customerID,
		Subject:        " billing ",
		Channel: domain.ChannelBinding{
			Channel:   "web",
			SessionID: "sess-1",
		},
		Participants: []domain.Participant{{ID: "u-1", UserID: &customerID, Role: domain.ParticipantRoleCustomer}},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != "conv-1" || got.Status != string(domain.ConversationStatusActive) || got.Subject != "billing" {
		t.Fatalf("unexpected conversation dto: %+v", got)
	}
	if len(publisher.events) != 1 || publisher.events[0].Name() != ConversationCreatedEventName {
		t.Fatalf("expected conversation created event, got %+v", publisher.events)
	}
}

func TestServiceResumeConversation(t *testing.T) {
	now := time.Now()
	repo := &stubConversationRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive, StartedAt: now},
		},
	}
	svc := NewService(repo, nil)

	got, err := svc.ResumeConversation(context.Background(), ResumeConversationQuery{ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != "conv-1" {
		t.Fatalf("unexpected conversation: %+v", got)
	}
}

func TestServiceIngestTextMessageUpdatesConversationAndPublishesEvent(t *testing.T) {
	now := time.Now()
	repo := &stubConversationRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive, StartedAt: now.Add(-time.Minute)},
		},
	}
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)
	svc.now = func() time.Time { return now }

	got, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{
		ConversationID: "conv-1",
		Sender:         domain.ParticipantRoleCustomer,
		Content:        " hello ",
		Metadata:       map[string]string{"source": "web"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Kind != string(domain.MessageKindText) || got.Content != "hello" {
		t.Fatalf("unexpected message dto: %+v", got)
	}
	if len(repo.messages["conv-1"]) != 1 {
		t.Fatalf("expected one persisted message, got %+v", repo.messages["conv-1"])
	}
	if repo.conversations["conv-1"].LastMessageAt == nil || !repo.conversations["conv-1"].LastMessageAt.Equal(now) {
		t.Fatalf("expected conversation last_message_at to be updated, got %+v", repo.conversations["conv-1"])
	}
	if len(publisher.events) != 1 || publisher.events[0].Name() != ConversationMessageReceivedEventName {
		t.Fatalf("expected message received event, got %+v", publisher.events)
	}
}

func TestServiceIngestSystemEventUsesSystemSender(t *testing.T) {
	now := time.Now()
	repo := &stubConversationRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive, StartedAt: now},
		},
	}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return now }

	got, err := svc.IngestSystemEvent(context.Background(), IngestSystemEventCommand{
		ConversationID: "conv-1",
		Content:        "agent assigned",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Sender != string(domain.ParticipantRoleSystem) || got.Kind != string(domain.MessageKindSystem) {
		t.Fatalf("unexpected system event dto: %+v", got)
	}
}

func TestServiceListRecentMessages(t *testing.T) {
	now := time.Now()
	repo := &stubConversationRepo{
		messages: map[string][]domain.ConversationMessage{
			"conv-1": {
				{ID: "1", ConversationID: "conv-1", Sender: domain.ParticipantRoleCustomer, Kind: domain.MessageKindText, Content: "a", CreatedAt: now},
				{ID: "2", ConversationID: "conv-1", Sender: domain.ParticipantRoleAgent, Kind: domain.MessageKindText, Content: "b", CreatedAt: now},
			},
		},
	}
	svc := NewService(repo, nil)

	got, err := svc.ListRecentMessages(context.Background(), "conv-1", 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("unexpected messages: %+v", got)
	}
}

func TestServiceAssignAgent(t *testing.T) {
	repo := &stubConversationRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Status: domain.ConversationStatusWaitingHuman, Participants: []domain.Participant{}},
		},
	}
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)
	svc.now = func() time.Time { return time.Now() }

	got, err := svc.AssignAgent(context.Background(), "conv-1", 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusActive) {
		t.Fatalf("expected active after assign, got %q", got.Status)
	}
	// 验证参与者中包含 agent
	found := false
	for _, p := range got.Participants {
		if p.Role == domain.ParticipantRoleAgent && p.UserID != nil && *p.UserID == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected agent participant, got %+v", got.Participants)
	}
}

func TestServiceTransfer(t *testing.T) {
	repo := &stubConversationRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive, Participants: []domain.Participant{
				{ID: "agent:1", UserID: ptrUint(1), Role: domain.ParticipantRoleAgent},
			}},
		},
	}
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)
	svc.now = func() time.Time { return time.Now() }

	got, err := svc.Transfer(context.Background(), "conv-1", 2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusTransferred) {
		t.Fatalf("expected transferred, got %q", got.Status)
	}
	// 应有系统消息记录转派
	if len(repo.messages["conv-1"]) == 0 {
		t.Fatalf("expected system message for transfer")
	}
}

func ptrUint(v uint) *uint { return &v }
