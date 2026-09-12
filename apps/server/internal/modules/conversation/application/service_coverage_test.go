package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"servify/apps/server/internal/modules/conversation/domain"
)

// scriptedRepo 允许按方法注入错误，用于覆盖 service 的错误分支。
type scriptedRepo struct {
	conversations map[string]*domain.Conversation
	messages      []domain.ConversationMessage

	createErr     error
	getErr        error
	updateErr     error
	appendErr     error
	listErr       error
	listBeforeErr error

	listLimit        int
	listBeforeLimit  int
	listBeforeBefore string
}

func (s *scriptedRepo) ListSessions(ctx context.Context, query OpenSessionListQuery) ([]domain.Conversation, int64, error) {
	return nil, 0, nil
}

func (s *scriptedRepo) CreateConversation(ctx context.Context, conversation *domain.Conversation) error {
	if s.createErr != nil {
		return s.createErr
	}
	if s.conversations == nil {
		s.conversations = map[string]*domain.Conversation{}
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *scriptedRepo) GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	item, ok := s.conversations[conversationID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *item
	return &cp, nil
}

func (s *scriptedRepo) UpdateConversation(ctx context.Context, conversation *domain.Conversation) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.conversations == nil {
		s.conversations = map[string]*domain.Conversation{}
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *scriptedRepo) AppendMessage(ctx context.Context, message *domain.ConversationMessage) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.messages = append(s.messages, *message)
	return nil
}

func (s *scriptedRepo) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]domain.ConversationMessage, error) {
	s.listLimit = limit
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]domain.ConversationMessage, 0, len(s.messages))
	return append(out, s.messages...), nil
}

func (s *scriptedRepo) ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]domain.ConversationMessage, error) {
	s.listBeforeLimit = limit
	s.listBeforeBefore = beforeMessageID
	if s.listBeforeErr != nil {
		return nil, s.listBeforeErr
	}
	out := make([]domain.ConversationMessage, 0, len(s.messages))
	return append(out, s.messages...), nil
}

func TestServiceCreateConversationValidation(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)

	for _, id := range []string{"", "   "} {
		if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{ConversationID: id}); err == nil || err.Error() != "conversation_id required" {
			t.Fatalf("expected conversation_id required error for %q, got %v", id, err)
		}
	}
}

func TestServiceCreateConversationRepoError(t *testing.T) {
	repo := &scriptedRepo{createErr: errors.New("create boom")}
	svc := NewService(repo, nil)

	if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{ConversationID: "conv-1"}); err == nil || err.Error() != "create boom" {
		t.Fatalf("expected repo error, got %v", err)
	}
}

func TestServiceCreateConversationWithoutPublisher(t *testing.T) {
	repo := &scriptedRepo{}
	now := time.Now()
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return now }

	got, err := svc.CreateConversation(context.Background(), CreateConversationCommand{ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != "conv-1" || got.Status != string(domain.ConversationStatusActive) || !got.StartedAt.Equal(now) {
		t.Fatalf("unexpected dto: %+v", got)
	}
	if len(repo.conversations) != 1 {
		t.Fatalf("expected one stored conversation, got %+v", repo.conversations)
	}
}

func TestServiceCreateConversationClonesParticipants(t *testing.T) {
	repo := &scriptedRepo{}
	svc := NewService(repo, &stubConversationPublisher{})
	svc.now = func() time.Time { return time.Now() }

	participants := []domain.Participant{{ID: "u-1", Role: domain.ParticipantRoleCustomer}}
	got, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
		ConversationID: "conv-1",
		Participants:   participants,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got.Participants) != 1 || got.Participants[0].ID != "u-1" {
		t.Fatalf("unexpected participants: %+v", got.Participants)
	}
}

func TestServiceResumeConversationValidationAndErrors(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)
	if _, err := svc.ResumeConversation(context.Background(), ResumeConversationQuery{ConversationID: " "}); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}

	svc = NewService(&scriptedRepo{getErr: errors.New("get boom")}, nil)
	if _, err := svc.ResumeConversation(context.Background(), ResumeConversationQuery{ConversationID: "conv-1"}); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected repo error, got %v", err)
	}

	svc = NewService(&scriptedRepo{}, nil)
	if _, err := svc.ResumeConversation(context.Background(), ResumeConversationQuery{ConversationID: "missing"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestServiceGetConversationDelegates(t *testing.T) {
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{
		"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive},
	}}
	svc := NewService(repo, nil)

	got, err := svc.GetConversation(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != "conv-1" {
		t.Fatalf("unexpected conversation: %+v", got)
	}

	if _, err := svc.GetConversation(context.Background(), "missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestServiceListRecentMessagesBranches(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)
	if _, err := svc.ListRecentMessages(context.Background(), "", 5); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}

	now := time.Now()
	repo := &scriptedRepo{messages: []domain.ConversationMessage{
		{ID: "1", ConversationID: "conv-1", Sender: domain.ParticipantRoleCustomer, Kind: domain.MessageKindText, Content: "a", CreatedAt: now},
	}}
	svc = NewService(repo, nil)

	got, err := svc.ListRecentMessages(context.Background(), "conv-1", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.listLimit != 10 {
		t.Fatalf("expected default limit 10, got %d", repo.listLimit)
	}
	if len(got) != 1 || got[0].ID != "1" || got[0].Sender != string(domain.ParticipantRoleCustomer) {
		t.Fatalf("unexpected messages: %+v", got)
	}

	repo = &scriptedRepo{messages: []domain.ConversationMessage{{ID: "1"}}}
	svc = NewService(repo, nil)
	if _, err := svc.ListRecentMessages(context.Background(), "conv-1", -3); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.listLimit != 10 {
		t.Fatalf("expected default limit 10 for negative limit, got %d", repo.listLimit)
	}

	repo = &scriptedRepo{listErr: errors.New("list boom")}
	svc = NewService(repo, nil)
	if _, err := svc.ListRecentMessages(context.Background(), "conv-1", 5); err == nil || err.Error() != "list boom" {
		t.Fatalf("expected repo error, got %v", err)
	}
}

func TestServiceListMessagesBeforeBranches(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)
	if _, err := svc.ListMessagesBefore(context.Background(), "  ", "1", 5); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}

	now := time.Now()
	repo := &scriptedRepo{messages: []domain.ConversationMessage{
		{ID: "1", ConversationID: "conv-1", Kind: domain.MessageKindText, Content: "a", CreatedAt: now},
		{ID: "2", ConversationID: "conv-1", Kind: domain.MessageKindText, Content: "b", CreatedAt: now.Add(time.Second)},
	}}
	svc = NewService(repo, nil)

	got, err := svc.ListMessagesBefore(context.Background(), "conv-1", "1", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.listBeforeLimit != 50 {
		t.Fatalf("expected default limit 50, got %d", repo.listBeforeLimit)
	}
	if repo.listBeforeBefore != "1" {
		t.Fatalf("expected before cursor captured, got %q", repo.listBeforeBefore)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected messages: %+v", got)
	}

	repo = &scriptedRepo{listBeforeErr: errors.New("before boom")}
	svc = NewService(repo, nil)
	if _, err := svc.ListMessagesBefore(context.Background(), "conv-1", "1", 5); err == nil || err.Error() != "before boom" {
		t.Fatalf("expected repo error, got %v", err)
	}
}

func TestServiceAssignAgentBranches(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)
	if _, err := svc.AssignAgent(context.Background(), " ", 1); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}

	svc = NewService(&scriptedRepo{getErr: errors.New("get boom")}, nil)
	if _, err := svc.AssignAgent(context.Background(), "conv-1", 1); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected repo error, got %v", err)
	}

	svc = NewService(&scriptedRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Participants: []domain.Participant{{ID: "agent:1", UserID: ptrUint(1), Role: domain.ParticipantRoleAgent}}},
		},
		updateErr: errors.New("update boom"),
	}, nil)
	if _, err := svc.AssignAgent(context.Background(), "conv-1", 2); err == nil || err.Error() != "update boom" {
		t.Fatalf("expected update error, got %v", err)
	}

	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{
		"conv-1": {ID: "conv-1", Status: domain.ConversationStatusWaitingHuman, Participants: []domain.Participant{
			{ID: "agent:1", UserID: ptrUint(1), Role: domain.ParticipantRoleAgent},
		}},
	}}
	svc = NewService(repo, nil)
	got, err := svc.AssignAgent(context.Background(), "conv-1", 3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusActive) {
		t.Fatalf("expected active status, got %q", got.Status)
	}
	if len(got.Participants) != 1 || got.Participants[0].ID != "agent:3" || *got.Participants[0].UserID != 3 {
		t.Fatalf("expected agent participant replaced, got %+v", got.Participants)
	}
}

func TestServiceTransferBranches(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)
	if _, err := svc.Transfer(context.Background(), "", 1); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}

	svc = NewService(&scriptedRepo{getErr: gorm.ErrRecordNotFound}, nil)
	if _, err := svc.Transfer(context.Background(), "conv-1", 1); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	svc = NewService(&scriptedRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1"},
		},
		updateErr: errors.New("update boom"),
	}, nil)
	if _, err := svc.Transfer(context.Background(), "conv-1", 2); err == nil || err.Error() != "update boom" {
		t.Fatalf("expected update error, got %v", err)
	}

	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{
		"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive},
	}}
	svc = NewService(repo, nil)
	got, err := svc.Transfer(context.Background(), "conv-1", 4)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusTransferred) {
		t.Fatalf("expected transferred status, got %q", got.Status)
	}
	if len(got.Participants) != 1 || got.Participants[0].ID != "agent:4" {
		t.Fatalf("expected agent participant appended, got %+v", got.Participants)
	}
	if len(repo.messages) == 0 || !strings.Contains(repo.messages[0].Content, "#4") {
		t.Fatalf("expected transfer system message, got %+v", repo.messages)
	}
}

func TestServiceTransferIgnoresSystemMessageFailure(t *testing.T) {
	repo := &scriptedRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1"},
		},
		appendErr: errors.New("append boom"),
	}
	svc := NewService(repo, nil)

	got, err := svc.Transfer(context.Background(), "conv-1", 5)
	if err != nil {
		t.Fatalf("expected no error despite system message failure, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusTransferred) {
		t.Fatalf("unexpected status %q", got.Status)
	}
}

func TestServiceCloseBranches(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)
	if _, err := svc.Close(context.Background(), "  "); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}

	svc = NewService(&scriptedRepo{getErr: errors.New("get boom")}, nil)
	if _, err := svc.Close(context.Background(), "conv-1"); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected repo error, got %v", err)
	}

	svc = NewService(&scriptedRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1"},
		},
		updateErr: errors.New("update boom"),
	}, nil)
	if _, err := svc.Close(context.Background(), "conv-1"); err == nil || err.Error() != "update boom" {
		t.Fatalf("expected update error, got %v", err)
	}

	now := time.Now()
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{
		"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive},
	}}
	svc = NewService(repo, nil)
	svc.now = func() time.Time { return now }

	got, err := svc.Close(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusClosed) {
		t.Fatalf("expected closed status, got %q", got.Status)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(now) {
		t.Fatalf("expected ended_at set, got %+v", got.EndedAt)
	}
	if len(repo.messages) != 1 || repo.messages[0].Sender != domain.ParticipantRoleSystem {
		t.Fatalf("expected close system message, got %+v", repo.messages)
	}
}

func TestServiceIngestMessageValidation(t *testing.T) {
	svc := NewService(&scriptedRepo{
		conversations: map[string]*domain.Conversation{"conv-1": {ID: "conv-1"}},
	}, nil)

	if _, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{ConversationID: "", Content: "hi"}); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}
	if _, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{ConversationID: "conv-1", Content: "  "}); err == nil || err.Error() != "content required" {
		t.Fatalf("expected content required, got %v", err)
	}
}

func TestServiceIngestMessageRepoErrors(t *testing.T) {
	now := time.Now()

	repo := &scriptedRepo{getErr: errors.New("get boom")}
	svc := NewService(repo, nil)
	if _, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{ConversationID: "conv-1", Content: "hi"}); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get error, got %v", err)
	}

	repo = &scriptedRepo{conversations: map[string]*domain.Conversation{"conv-1": {ID: "conv-1"}}, appendErr: errors.New("append boom")}
	svc = NewService(repo, nil)
	svc.now = func() time.Time { return now }
	if _, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{ConversationID: "conv-1", Content: "hi"}); err == nil || err.Error() != "append boom" {
		t.Fatalf("expected append error, got %v", err)
	}

	repo = &scriptedRepo{conversations: map[string]*domain.Conversation{"conv-1": {ID: "conv-1"}}, updateErr: errors.New("update boom")}
	svc = NewService(repo, nil)
	svc.now = func() time.Time { return now }
	if _, err := svc.IngestSystemEvent(context.Background(), IngestSystemEventCommand{ConversationID: "conv-1", Content: "evt"}); err == nil || err.Error() != "update boom" {
		t.Fatalf("expected update error, got %v", err)
	}
}

func TestServiceIngestMessageExplicitIDAndEmptyMetadata(t *testing.T) {
	now := time.Now()
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{"conv-1": {ID: "conv-1"}}}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return now }

	got, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{
		ConversationID: "conv-1",
		MessageID:      " msg-1 ",
		Content:        "hi",
		Metadata:       map[string]string{},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != "msg-1" {
		t.Fatalf("expected trimmed explicit message id, got %q", got.ID)
	}
	if got.Metadata != nil {
		t.Fatalf("expected nil metadata for empty map, got %+v", got.Metadata)
	}
	if !repo.conversations["conv-1"].LastMessageAt.Equal(now) {
		t.Fatalf("expected last_message_at updated, got %+v", repo.conversations["conv-1"].LastMessageAt)
	}
}

func TestServicePublishIgnoresPublisherError(t *testing.T) {
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{
		"conv-1": {ID: "conv-1"},
	}}
	svc := NewService(repo, &stubConversationPublisher{err: errors.New("publish boom")})
	svc.now = func() time.Time { return time.Now() }

	if _, err := svc.IngestTextMessage(context.Background(), IngestTextMessageCommand{ConversationID: "conv-1", Content: "hi"}); err != nil {
		t.Fatalf("expected publish error to be ignored, got %v", err)
	}
	if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{ConversationID: "conv-2"}); err != nil {
		t.Fatalf("expected publish error to be ignored on create, got %v", err)
	}
}

func TestNewConversationEventFields(t *testing.T) {
	payload := map[string]string{"k": "v"}
	event := NewConversationEvent(ConversationCreatedEventName, "conv-1", payload)

	if event.Name() != ConversationCreatedEventName || event.ConversationID != "conv-1" {
		t.Fatalf("unexpected event header: %+v", event)
	}
	if event.ID() == "" || event.AggregateID() != "conversation:conv-1" {
		t.Fatalf("unexpected event id/aggregate: %q %q", event.ID(), event.AggregateID())
	}
	payloadMap, ok := event.Payload.(map[string]string)
	if !ok || payloadMap["k"] != "v" || event.OccurredAt().IsZero() {
		t.Fatalf("unexpected event payload/time: %+v", event)
	}
}

func TestMapConversationFullFields(t *testing.T) {
	now := time.Now()
	lastMessageAt := now.Add(time.Minute)
	endedAt := now.Add(2 * time.Minute)
	customerID := uint(3)

	dto := MapConversation(domain.Conversation{
		ID:            "conv-1",
		CustomerID:    &customerID,
		Status:        domain.ConversationStatusClosed,
		Subject:       "s",
		Channel:       domain.ChannelBinding{Channel: "web"},
		Participants:  []domain.Participant{{ID: "u-1"}},
		StartedAt:     now,
		LastMessageAt: &lastMessageAt,
		EndedAt:       &endedAt,
	})

	if dto.ID != "conv-1" || dto.CustomerID == nil || *dto.CustomerID != 3 || dto.Status != string(domain.ConversationStatusClosed) {
		t.Fatalf("unexpected dto header: %+v", dto)
	}
	if dto.Subject != "s" || dto.Channel.Channel != "web" || len(dto.Participants) != 1 {
		t.Fatalf("unexpected dto body: %+v", dto)
	}
	if !dto.StartedAt.Equal(now) || !dto.LastMessageAt.Equal(lastMessageAt) || !dto.EndedAt.Equal(endedAt) {
		t.Fatalf("unexpected dto timestamps: %+v", dto)
	}
}

func TestMapMessageFullFields(t *testing.T) {
	now := time.Now()
	dto := MapMessage(domain.ConversationMessage{
		ID:             "msg-1",
		ConversationID: "conv-1",
		Sender:         domain.ParticipantRoleAI,
		Kind:           domain.MessageKindSystem,
		Content:        "hi",
		Metadata:       map[string]string{"a": "b"},
		CreatedAt:      now,
	})

	if dto.ID != "msg-1" || dto.ConversationID != "conv-1" || dto.Sender != string(domain.ParticipantRoleAI) {
		t.Fatalf("unexpected dto header: %+v", dto)
	}
	if dto.Kind != string(domain.MessageKindSystem) || dto.Content != "hi" || dto.Metadata["a"] != "b" || !dto.CreatedAt.Equal(now) {
		t.Fatalf("unexpected dto body: %+v", dto)
	}
}
