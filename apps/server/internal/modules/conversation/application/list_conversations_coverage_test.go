package application

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/modules/conversation/domain"
)

// failingSessionRepo 只为注入 ListSessions 错误。
type failingSessionRepo struct {
	*stubConversationRepo
	listErr error
}

func (f *failingSessionRepo) ListSessions(ctx context.Context, query OpenSessionListQuery) ([]domain.Conversation, int64, error) {
	return nil, 0, f.listErr
}

func TestServiceListConversations(t *testing.T) {
	svc := NewService(&stubConversationRepo{sessions: []domain.Conversation{
		{ID: "sess-1", Status: domain.ConversationStatusActive},
		{ID: "sess-2", Status: domain.ConversationStatusClosed},
	}}, nil)

	items, total, err := svc.ListConversations(context.Background(), OpenSessionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(items))
	}
	if items[0].ID != "sess-1" || items[1].Status != string(domain.ConversationStatusClosed) {
		t.Fatalf("unexpected items: %+v", items)
	}

	// 空结果也要返回非 nil 切片与零 total。
	items, total, err = NewService(&stubConversationRepo{}, nil).ListConversations(context.Background(), OpenSessionListQuery{})
	if err != nil || total != 0 || items == nil || len(items) != 0 {
		t.Fatalf("empty list: items=%v total=%d err=%v", items, total, err)
	}

	// 仓库错误原样透传。
	boom := errors.New("list boom")
	failing := &failingSessionRepo{stubConversationRepo: &stubConversationRepo{}, listErr: boom}
	_, _, err = NewService(failing, nil).ListConversations(context.Background(), OpenSessionListQuery{Status: "open"})
	if !errors.Is(err, boom) {
		t.Fatalf("want raw repo error, got %v", err)
	}
}
