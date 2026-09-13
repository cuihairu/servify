package delivery

import (
	"context"
	"errors"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
)

// sessionsScriptedRepo 为 ListSessions 提供数据与错误注入。
type sessionsScriptedRepo struct {
	*scriptedConversationRepo

	sessions []conversationdomain.Conversation
	total    int64
	sessErr  error
}

func (s *sessionsScriptedRepo) ListSessions(ctx context.Context, query conversationapp.OpenSessionListQuery) ([]conversationdomain.Conversation, int64, error) {
	if s.sessErr != nil {
		return nil, 0, s.sessErr
	}
	return s.sessions, s.total, nil
}

func TestOpenConversationAdapter(t *testing.T) {
	ctx := context.Background()
	repo := &sessionsScriptedRepo{
		scriptedConversationRepo: &scriptedConversationRepo{
			conversations: map[string]*conversationdomain.Conversation{
				"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
			},
			messages: map[string][]conversationdomain.ConversationMessage{
				"sess-1": {{ID: "m-1", ConversationID: "sess-1"}},
			},
		},
		sessions: []conversationdomain.Conversation{
			{ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
			{ID: "sess-2", Status: conversationdomain.ConversationStatusClosed},
		},
		total: 2,
	}
	adapter := NewOpenConversationAdapter(conversationapp.NewService(repo, nil))
	if adapter == nil {
		t.Fatal("NewOpenConversationAdapter() = nil")
	}

	items, total, err := adapter.ListConversations(ctx, conversationapp.OpenSessionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if total != 2 || len(items) != 2 || items[1].ID != "sess-2" {
		t.Fatalf("unexpected list result: total=%d items=%+v", total, items)
	}

	got, err := adapter.GetConversation(ctx, "sess-1")
	if err != nil || got == nil || got.ID != "sess-1" {
		t.Fatalf("GetConversation() = %+v, %v", got, err)
	}

	msgs, err := adapter.ListRecentMessages(ctx, "sess-1", 10)
	if err != nil || len(msgs) != 1 || msgs[0].ID != "m-1" {
		t.Fatalf("ListRecentMessages() = %+v, %v", msgs, err)
	}

	// 各方法错误透传。
	repo.sessErr = errors.New("list boom")
	if _, _, err := adapter.ListConversations(ctx, conversationapp.OpenSessionListQuery{}); err == nil || err.Error() != "list boom" {
		t.Fatalf("want list boom, got %v", err)
	}
	repo.sessErr = nil
	repo.getErr = errors.New("get boom")
	if _, err := adapter.GetConversation(ctx, "sess-1"); err == nil || err.Error() != "get boom" {
		t.Fatalf("want get boom, got %v", err)
	}
	repo.getErr = nil
	repo.listErr = errors.New("messages boom")
	if _, err := adapter.ListRecentMessages(ctx, "sess-1", 10); err == nil || err.Error() != "messages boom" {
		t.Fatalf("want messages boom, got %v", err)
	}
}
