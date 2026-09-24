package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"

	"gorm.io/gorm"
)

// scriptedConversationRepo 支持按方法注入错误，用于覆盖 delivery 适配器的错误分支。
type scriptedConversationRepo struct {
	conversations map[string]*conversationdomain.Conversation
	messages      map[string][]conversationdomain.ConversationMessage

	getErr        error
	createErr     error
	updateErr     error
	appendErr     error
	listErr       error
	listBeforeErr error
}

func (s *scriptedConversationRepo) ListSessions(ctx context.Context, query conversationapp.OpenSessionListQuery) ([]conversationdomain.Conversation, int64, error) {
	return nil, 0, nil
}

func (s *scriptedConversationRepo) CreateConversation(ctx context.Context, conversation *conversationdomain.Conversation) error {
	if s.createErr != nil {
		return s.createErr
	}
	if s.conversations == nil {
		s.conversations = map[string]*conversationdomain.Conversation{}
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *scriptedConversationRepo) GetConversation(ctx context.Context, conversationID string) (*conversationdomain.Conversation, error) {
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

func (s *scriptedConversationRepo) UpdateConversation(ctx context.Context, conversation *conversationdomain.Conversation) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.conversations == nil {
		s.conversations = map[string]*conversationdomain.Conversation{}
	}
	cp := *conversation
	s.conversations[conversation.ID] = &cp
	return nil
}

func (s *scriptedConversationRepo) AppendMessage(ctx context.Context, message *conversationdomain.ConversationMessage) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	if s.messages == nil {
		s.messages = map[string][]conversationdomain.ConversationMessage{}
	}
	s.messages[message.ConversationID] = append(s.messages[message.ConversationID], *message)
	return nil
}

func (s *scriptedConversationRepo) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]conversationdomain.ConversationMessage, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	items := s.messages[conversationID]
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]conversationdomain.ConversationMessage, 0, len(items))
	return append(out, items...), nil
}

func (s *scriptedConversationRepo) ListMessagesAfter(ctx context.Context, conversationID string, afterMessageID string, limit int) ([]conversationdomain.ConversationMessage, error) {
	items := s.messages[conversationID]
	out := make([]conversationdomain.ConversationMessage, 0, len(items))
	return append(out, items...), nil
}

func (s *scriptedConversationRepo) ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]conversationdomain.ConversationMessage, error) {
	if s.listBeforeErr != nil {
		return nil, s.listBeforeErr
	}
	items := s.messages[conversationID]
	out := make([]conversationdomain.ConversationMessage, 0, len(items))
	return append(out, items...), nil
}

func TestHandlerServiceAdapterGetConversationBranches(t *testing.T) {
	// 正常路径：命中会话
	repo := &scriptedConversationRepo{conversations: map[string]*conversationdomain.Conversation{
		"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
	}}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))
	dto, err := handler.GetConversation(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if dto.ID != "sess-1" {
		t.Fatalf("unexpected dto: %+v", dto)
	}

	// not found 且消息列表查询失败 → ErrConversationNotFound
	repo = &scriptedConversationRepo{listErr: errors.New("list boom")}
	if _, err := handler.GetConversation(context.Background(), "sess-x"); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	// not found 且无消息 → ErrConversationNotFound
	repo = &scriptedConversationRepo{}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.GetConversation(context.Background(), "sess-x"); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	// 其他错误原样透传
	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.GetConversation(context.Background(), "sess-1"); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}
}

func TestHandlerServiceAdapterListMessagesBranches(t *testing.T) {
	now := time.Now()
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
		messages: map[string][]conversationdomain.ConversationMessage{
			"sess-1": {{ID: "1", ConversationID: "sess-1", Content: "hi", CreatedAt: now}},
		},
	}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))

	items, err := handler.ListMessages(context.Background(), "sess-1", 10)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(items) != 1 || items[0].ID != "1" {
		t.Fatalf("unexpected messages: %+v", items)
	}

	// GetConversation 返回其他错误
	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.ListMessages(context.Background(), "sess-1", 10); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}

	// 列表查询返回 not found → ErrConversationNotFound
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{"sess-1": {ID: "sess-1"}},
		listErr:       gorm.ErrRecordNotFound,
	}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.ListMessages(context.Background(), "sess-1", 10); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	// 列表查询返回其他错误 → 透传
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{"sess-1": {ID: "sess-1"}},
		listErr:       errors.New("list boom"),
	}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.ListMessages(context.Background(), "sess-1", 10); err == nil || err.Error() != "list boom" {
		t.Fatalf("expected list boom, got %v", err)
	}
}

func TestHandlerServiceAdapterListMessagesBeforeBranches(t *testing.T) {
	now := time.Now()
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
		messages: map[string][]conversationdomain.ConversationMessage{
			"sess-1": {
				{ID: "1", ConversationID: "sess-1", Content: "old", CreatedAt: now.Add(-time.Minute)},
				{ID: "2", ConversationID: "sess-1", Content: "new", CreatedAt: now},
			},
		},
	}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))

	items, err := handler.ListMessagesBefore(context.Background(), "sess-1", "2", 10)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected messages: %+v", items)
	}

	// GetConversation 其他错误
	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.ListMessagesBefore(context.Background(), "sess-1", "2", 10); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}

	// 查询返回 not found → ErrConversationNotFound
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{"sess-1": {ID: "sess-1"}},
		listBeforeErr: gorm.ErrRecordNotFound,
	}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.ListMessagesBefore(context.Background(), "sess-1", "2", 10); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	// 查询返回其他错误 → 透传
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{"sess-1": {ID: "sess-1"}},
		listBeforeErr: errors.New("before boom"),
	}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.ListMessagesBefore(context.Background(), "sess-1", "2", 10); err == nil || err.Error() != "before boom" {
		t.Fatalf("expected before boom, got %v", err)
	}
}

func TestHandlerServiceAdapterSendAgentMessageBranches(t *testing.T) {
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
	}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))

	got, err := handler.SendAgentMessage(context.Background(), "sess-1", "hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Sender != string(conversationdomain.ParticipantRoleAgent) || got.Content != "hello" {
		t.Fatalf("unexpected message dto: %+v", got)
	}
	if len(repo.messages["sess-1"]) != 1 {
		t.Fatalf("expected one persisted message, got %+v", repo.messages["sess-1"])
	}

	// 会话不存在 → ErrConversationNotFound
	handler = NewHandlerService(conversationapp.NewService(&scriptedConversationRepo{}, nil))
	if _, err := handler.SendAgentMessage(context.Background(), "missing", "hi"); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	// GetConversation 其他错误
	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.SendAgentMessage(context.Background(), "sess-1", "hi"); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}

	// 写入消息失败 → 透传
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{"sess-1": {ID: "sess-1"}},
		appendErr:     errors.New("append boom"),
	}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.SendAgentMessage(context.Background(), "sess-1", "hi"); err == nil || err.Error() != "append boom" {
		t.Fatalf("expected append boom, got %v", err)
	}
}

func TestHandlerServiceAdapterAssignAgentBranches(t *testing.T) {
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusWaitingHuman},
		},
	}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))

	got, err := handler.AssignAgent(context.Background(), "sess-1", 3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(conversationdomain.ConversationStatusActive) {
		t.Fatalf("unexpected status %q", got.Status)
	}

	handler = NewHandlerService(conversationapp.NewService(&scriptedConversationRepo{}, nil))
	if _, err := handler.AssignAgent(context.Background(), "missing", 3); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.AssignAgent(context.Background(), "sess-1", 3); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}
}

func TestHandlerServiceAdapterTransferBranches(t *testing.T) {
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
	}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))

	got, err := handler.Transfer(context.Background(), "sess-1", 4)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(conversationdomain.ConversationStatusTransferred) {
		t.Fatalf("unexpected status %q", got.Status)
	}
	if len(repo.messages["sess-1"]) != 1 {
		t.Fatalf("expected transfer system message, got %+v", repo.messages["sess-1"])
	}

	handler = NewHandlerService(conversationapp.NewService(&scriptedConversationRepo{}, nil))
	if _, err := handler.Transfer(context.Background(), "missing", 4); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.Transfer(context.Background(), "sess-1", 4); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}
}

func TestHandlerServiceAdapterCloseBranches(t *testing.T) {
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
	}
	handler := NewHandlerService(conversationapp.NewService(repo, nil))

	got, err := handler.Close(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(conversationdomain.ConversationStatusClosed) || got.EndedAt == nil {
		t.Fatalf("unexpected dto: %+v", got)
	}
	if len(repo.messages["sess-1"]) != 1 {
		t.Fatalf("expected close system message, got %+v", repo.messages["sess-1"])
	}

	handler = NewHandlerService(conversationapp.NewService(&scriptedConversationRepo{}, nil))
	if _, err := handler.Close(context.Background(), "missing"); err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	repo = &scriptedConversationRepo{getErr: errors.New("get boom")}
	handler = NewHandlerService(conversationapp.NewService(repo, nil))
	if _, err := handler.Close(context.Background(), "sess-1"); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}
}
