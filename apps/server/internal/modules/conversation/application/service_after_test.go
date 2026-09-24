package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/modules/conversation/domain"
)

// TestServiceListMessagesAfter 锚定访客增量补拉服务面的参数校验与映射。
func TestServiceListMessagesAfter(t *testing.T) {
	svc := NewService(&scriptedRepo{}, nil)

	if _, err := svc.ListMessagesAfter(context.Background(), "  ", "1", 5); err == nil || err.Error() != "conversation_id required" {
		t.Fatalf("expected conversation_id required, got %v", err)
	}
	if _, err := svc.ListMessagesAfter(context.Background(), "conv-1", "", 0); err != nil {
		t.Fatalf("default limit path: %v", err)
	}

	now := time.Now()
	repo := &scriptedRepo{messages: []domain.ConversationMessage{
		{ID: "3", ConversationID: "conv-1", Kind: domain.MessageKindText, Sender: domain.ParticipantRoleAgent, Content: "c", CreatedAt: now},
	}}
	svc = NewService(repo, nil)

	got, err := svc.ListMessagesAfter(context.Background(), "conv-1", "2", 10)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(got) != 1 || got[0].ID != "3" || got[0].Sender != "agent" || got[0].Content != "c" {
		t.Fatalf("unexpected dto: %+v", got)
	}
	if repo.listAfterCursor != "2" || repo.listAfterLimit != 10 {
		t.Fatalf("repo args: cursor=%s limit=%d", repo.listAfterCursor, repo.listAfterLimit)
	}

	repoErr := &scriptedRepo{listAfterErr: errServiceDown}
	if _, err := NewService(repoErr, nil).ListMessagesAfter(context.Background(), "conv-1", "2", 10); err == nil {
		t.Fatal("expected repo error passthrough")
	}
}

var errServiceDown = &serviceError{"down"}

type serviceError struct{ msg string }

func (e *serviceError) Error() string { return e.msg }
