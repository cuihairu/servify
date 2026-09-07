package delivery

import (
	"context"
	"errors"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
)

func TestWebSocketMessageAdapterPersistTextMessageBranches(t *testing.T) {
	// 会话已存在：直接写入，无需重建
	repo := &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
	}
	adapter := NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	if err := adapter.PersistTextMessage(context.Background(), "sess-1", "hi"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(repo.messages["sess-1"]) != 1 || repo.messages["sess-1"][0].Content != "hi" {
		t.Fatalf("unexpected messages: %+v", repo.messages["sess-1"])
	}

	// Resume 返回非 not found 错误 → 透传
	repo = &scriptedConversationRepo{getErr: errors.New("resume boom")}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	if err := adapter.PersistTextMessage(context.Background(), "sess-1", "hi"); err == nil || err.Error() != "resume boom" {
		t.Fatalf("expected resume boom, got %v", err)
	}

	// 会话缺失但创建失败 → 透传
	repo = &scriptedConversationRepo{createErr: errors.New("create boom")}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	if err := adapter.PersistTextMessage(context.Background(), "sess-1", "hi"); err == nil || err.Error() != "create boom" {
		t.Fatalf("expected create boom, got %v", err)
	}

	// 消息写入失败 → 透传
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusActive},
		},
		appendErr: errors.New("append boom"),
	}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	if err := adapter.PersistTextMessage(context.Background(), "sess-1", "hi"); err == nil || err.Error() != "append boom" {
		t.Fatalf("expected append boom, got %v", err)
	}
}

func TestWebSocketMessageAdapterHasActiveHumanAgentBranches(t *testing.T) {
	// 会话不存在 → (false, nil)
	adapter := NewWebSocketMessageAdapter(conversationapp.NewService(&scriptedConversationRepo{}, nil))
	assigned, err := adapter.HasActiveHumanAgent(context.Background(), "missing")
	if err != nil || assigned {
		t.Fatalf("expected (false, nil), got (%v, %v)", assigned, err)
	}

	// 查询错误 → (false, err)
	repo := &scriptedConversationRepo{getErr: errors.New("get boom")}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	if _, err := adapter.HasActiveHumanAgent(context.Background(), "sess-1"); err == nil || err.Error() != "get boom" {
		t.Fatalf("expected get boom, got %v", err)
	}

	// 会话已关闭 → false
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {ID: "sess-1", Status: conversationdomain.ConversationStatusClosed},
		},
	}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	assigned, err = adapter.HasActiveHumanAgent(context.Background(), "sess-1")
	if err != nil || assigned {
		t.Fatalf("expected closed conversation to have no agent, got (%v, %v)", assigned, err)
	}

	// agent 参与者无 UserID → false
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {
				ID:     "sess-1",
				Status: conversationdomain.ConversationStatusActive,
				Participants: []conversationdomain.Participant{
					{ID: "agent:0", Role: conversationdomain.ParticipantRoleAgent},
				},
			},
		},
	}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	assigned, err = adapter.HasActiveHumanAgent(context.Background(), "sess-1")
	if err != nil || assigned {
		t.Fatalf("expected agent without user id to be ignored, got (%v, %v)", assigned, err)
	}

	// 仅 customer 参与者 → false
	repo = &scriptedConversationRepo{
		conversations: map[string]*conversationdomain.Conversation{
			"sess-1": {
				ID:     "sess-1",
				Status: conversationdomain.ConversationStatusActive,
				Participants: []conversationdomain.Participant{
					{ID: "customer:7", Role: conversationdomain.ParticipantRoleCustomer},
				},
			},
		},
	}
	adapter = NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))
	assigned, err = adapter.HasActiveHumanAgent(context.Background(), "sess-1")
	if err != nil || assigned {
		t.Fatalf("expected no agent for customer-only conversation, got (%v, %v)", assigned, err)
	}
}

func TestWebSocketMessageAdapterListRecentMessagesError(t *testing.T) {
	repo := &scriptedConversationRepo{listErr: errors.New("list boom")}
	adapter := NewWebSocketMessageAdapter(conversationapp.NewService(repo, nil))

	if _, err := adapter.ListRecentMessages(context.Background(), "sess-1", 10); err == nil || err.Error() != "list boom" {
		t.Fatalf("expected list boom, got %v", err)
	}
}
