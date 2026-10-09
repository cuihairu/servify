package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/modules/conversation/domain"
)

// stubOpenTicketChecker 拦截查询面 stub：open>0 时返回固定明细。
type stubOpenTicketChecker struct {
	open int64
	ids  []uint
	err  error
}

func (s stubOpenTicketChecker) CountOpenBySession(ctx context.Context, sessionID string) (int64, []uint, error) {
	if s.err != nil {
		return 0, nil, s.err
	}
	return s.open, s.ids, nil
}

func newCloseTestService(t *testing.T, checker OpenTicketChecker) (*Service, *stubConversationRepo, *stubConversationPublisher) {
	t.Helper()
	now := time.Now()
	repo := &stubConversationRepo{
		conversations: map[string]*domain.Conversation{
			"conv-1": {ID: "conv-1", Status: domain.ConversationStatusActive, StartedAt: now},
		},
	}
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)
	svc.now = func() time.Time { return now }
	if checker != nil {
		svc.AttachOpenTicketChecker(checker)
	}
	return svc, repo, publisher
}

func closedEventPayloads(publisher *stubConversationPublisher) []map[string]interface{} {
	var out []map[string]interface{}
	for _, event := range publisher.events {
		if event.Name() != ConversationClosedEventName {
			continue
		}
		if ce, ok := event.(ConversationEvent); ok {
			if payload, ok := ce.Payload.(map[string]interface{}); ok {
				out = append(out, payload)
			}
		}
	}
	return out
}

func TestServiceCloseBlockedByOpenTickets(t *testing.T) {
	svc, repo, publisher := newCloseTestService(t, stubOpenTicketChecker{open: 2, ids: []uint{7, 9}})

	_, err := svc.Close(context.Background(), "conv-1")
	if !errors.Is(err, ErrOpenTicketsRemain) {
		t.Fatalf("expected ErrOpenTicketsRemain, got %v", err)
	}
	// 拦截路径不改状态、不发关闭事件。
	if repo.conversations["conv-1"].Status != domain.ConversationStatusActive {
		t.Fatalf("conversation should stay active after blocked close, got %s", repo.conversations["conv-1"].Status)
	}
	if events := closedEventPayloads(publisher); len(events) != 0 {
		t.Fatalf("expected no close event on blocked close, got %+v", events)
	}
}

func TestServiceCloseForceBypassesOpenTickets(t *testing.T) {
	svc, repo, publisher := newCloseTestService(t, stubOpenTicketChecker{open: 1, ids: []uint{7}})

	got, err := svc.CloseWithOptions(context.Background(), "conv-1", CloseOptions{AllowOpenTickets: true})
	if err != nil {
		t.Fatalf("expected forced close to succeed, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusClosed) {
		t.Fatalf("expected closed status, got %s", got.Status)
	}
	if repo.conversations["conv-1"].Status != domain.ConversationStatusClosed {
		t.Fatalf("repo should reflect closed conversation")
	}
	events := closedEventPayloads(publisher)
	if len(events) != 1 || events[0]["forced"] != true {
		t.Fatalf("expected one forced close event, got %+v", events)
	}
}

func TestServiceCloseSucceedsWithoutOpenTickets(t *testing.T) {
	svc, _, publisher := newCloseTestService(t, stubOpenTicketChecker{})

	got, err := svc.Close(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("expected close to succeed, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusClosed) {
		t.Fatalf("expected closed status, got %s", got.Status)
	}
	events := closedEventPayloads(publisher)
	if len(events) != 1 || events[0]["forced"] != false {
		t.Fatalf("expected one normal close event, got %+v", events)
	}
	if events[0]["conversation_id"] != "conv-1" {
		t.Fatalf("expected conversation_id in payload, got %+v", events[0])
	}
}

func TestServiceCloseWithoutCheckerKeepsLegacyBehavior(t *testing.T) {
	// 未注入 checker（既有组装路径）不拦截：行为与 B 批次前一致。
	svc, _, _ := newCloseTestService(t, nil)

	got, err := svc.Close(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("expected close without checker to succeed, got %v", err)
	}
	if got.Status != string(domain.ConversationStatusClosed) {
		t.Fatalf("expected closed status, got %s", got.Status)
	}
}

func TestServiceClosePropagatesCheckerError(t *testing.T) {
	// 查询面失败按错误返回（fail-closed：不放行关闭）。
	cause := errors.New("db down")
	svc, repo, _ := newCloseTestService(t, stubOpenTicketChecker{err: cause})

	_, err := svc.Close(context.Background(), "conv-1")
	if !errors.Is(err, cause) {
		t.Fatalf("expected checker error to propagate, got %v", err)
	}
	if repo.conversations["conv-1"].Status != domain.ConversationStatusActive {
		t.Fatalf("conversation should stay active on checker error")
	}
}
