package infra

import (
	"testing"

	"servify/apps/server/internal/modules/conversation/domain"
)

func TestMapMessageModelSetsResolvedUserID(t *testing.T) {
	orig := resolveMessageUserID
	resolveMessageUserID = func(domain.ConversationMessage) uint { return 42 }
	defer func() { resolveMessageUserID = orig }()

	model := mapMessageModel(domain.ConversationMessage{ConversationID: "sess-1", Content: "hi"})
	if model.UserID != 42 {
		t.Fatalf("expected resolved user id 42, got %d", model.UserID)
	}
	if model.SessionID != "sess-1" || model.Content != "hi" {
		t.Fatalf("unexpected message model: %+v", model)
	}
}

func TestMapMessageModelKeepsZeroUserIDByDefault(t *testing.T) {
	// seam 默认路径：存根恒返 0，不设置 UserID。
	model := mapMessageModel(domain.ConversationMessage{ConversationID: "sess-2", Content: "hello"})
	if model.UserID != 0 {
		t.Fatalf("expected zero user id from default stub, got %d", model.UserID)
	}
	if model.SessionID != "sess-2" || model.Content != "hello" {
		t.Fatalf("unexpected message model: %+v", model)
	}
}
