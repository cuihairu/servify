package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/modules/routing/domain"
	"servify/apps/server/internal/platform/eventbus"
)

func TestServiceValidationErrors(t *testing.T) {
	svc := NewService(&stubRoutingRepo{}, &stubRoutingPublisher{})

	if _, err := svc.AssignAgent(context.Background(), AssignAgentCommand{}); err == nil || err.Error() != "session_id required" {
		t.Fatalf("AssignAgent empty session error = %v", err)
	}
	if _, err := svc.AssignAgent(context.Background(), AssignAgentCommand{SessionID: "s"}); err == nil || err.Error() != "agent_id required" {
		t.Fatalf("AssignAgent zero agent error = %v", err)
	}

	if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{}); err == nil || err.Error() != "session_id required" {
		t.Fatalf("AddToWaitingQueue empty session error = %v", err)
	}

	if _, err := svc.CancelWaiting(context.Background(), CancelWaitingCommand{}); err == nil || err.Error() != "session_id required" {
		t.Fatalf("CancelWaiting empty session error = %v", err)
	}

	if _, err := svc.GetWaitingEntry(context.Background(), "  "); err == nil || err.Error() != "session_id required" {
		t.Fatalf("GetWaitingEntry blank session error = %v", err)
	}

	if _, err := svc.GetTransferHistory(context.Background(), ""); err == nil || err.Error() != "session_id required" {
		t.Fatalf("GetTransferHistory empty session error = %v", err)
	}

	if _, err := svc.MarkWaitingTransferred(context.Background(), MarkWaitingTransferredCommand{}); err == nil || err.Error() != "session_id required" {
		t.Fatalf("MarkWaitingTransferred empty session error = %v", err)
	}
	if _, err := svc.MarkWaitingTransferred(context.Background(), MarkWaitingTransferredCommand{SessionID: "s"}); err == nil || err.Error() != "assigned_to required" {
		t.Fatalf("MarkWaitingTransferred zero assignee error = %v", err)
	}
}

func TestServiceRepositoryErrorsPropagate(t *testing.T) {
	repo := &stubRoutingRepo{err: errors.New("repo down")}
	svc := NewService(repo, nil)

	ctx := context.Background()
	if _, err := svc.AssignAgent(ctx, AssignAgentCommand{SessionID: "s", AgentID: 1}); err == nil {
		t.Fatal("AssignAgent expected repo error")
	}
	if _, err := svc.GetTransferHistory(ctx, "s"); err == nil {
		t.Fatal("GetTransferHistory expected repo error")
	}
	if _, err := svc.ListRecentTransferHistory(ctx, 10); err == nil {
		t.Fatal("ListRecentTransferHistory expected repo error")
	}
	if _, err := svc.AddToWaitingQueue(ctx, AddToWaitingQueueCommand{SessionID: "s"}); err == nil {
		t.Fatal("AddToWaitingQueue expected repo error")
	}
	if _, err := svc.CancelWaiting(ctx, CancelWaitingCommand{SessionID: "s"}); err == nil {
		t.Fatal("CancelWaiting expected repo error")
	}
	if _, err := svc.ListWaitingEntries(ctx, "", 10); err == nil {
		t.Fatal("ListWaitingEntries expected repo error")
	}
	if _, err := svc.GetWaitingEntry(ctx, "s"); err == nil {
		t.Fatal("GetWaitingEntry expected repo error")
	}
	if _, err := svc.MarkWaitingTransferred(ctx, MarkWaitingTransferredCommand{SessionID: "s", AssignedTo: 1}); err == nil {
		t.Fatal("MarkWaitingTransferred expected repo error")
	}
}

func TestServiceAssignAgentDefaultsAssignedAt(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)
	now := time.Now()
	svc.now = func() time.Time { return now }

	got, err := svc.AssignAgent(context.Background(), AssignAgentCommand{SessionID: "sess-default", AgentID: 5})
	if err != nil {
		t.Fatalf("AssignAgent() error = %v", err)
	}
	if !got.AssignedAt.Equal(now) {
		t.Fatalf("AssignedAt = %v, want %v", got.AssignedAt, now)
	}
}

func TestServiceGetWaitingEntry(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	created, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: "sess-q"})
	if err != nil {
		t.Fatalf("AddToWaitingQueue() error = %v", err)
	}

	got, err := svc.GetWaitingEntry(context.Background(), "sess-q")
	if err != nil {
		t.Fatalf("GetWaitingEntry() error = %v", err)
	}
	if got.SessionID != created.SessionID || got.Status != string(domain.QueueStatusWaiting) {
		t.Fatalf("entry = %+v", got)
	}
}

func TestServiceAddToWaitingQueueTrimsAndCopiesInput(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	skills := []string{"billing"}
	got, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{
		SessionID:    "sess-trim",
		Reason:       "  need human  ",
		TargetSkills: skills,
		Priority:     "  high  ",
		Notes:        "  note  ",
	})
	if err != nil {
		t.Fatalf("AddToWaitingQueue() error = %v", err)
	}
	if got.SessionID != "sess-trim" {
		t.Fatalf("session id = %q", got.SessionID)
	}
	if got.Reason != "need human" || got.Priority != "high" || got.Notes != "note" {
		t.Fatalf("trimmed fields = %+v", got)
	}
	skills[0] = "mutated"
	if got.TargetSkills[0] != "billing" {
		t.Fatalf("target skills should be copied, got %+v", got.TargetSkills)
	}
}

func TestServiceCancelWaitingOverridesNotesWithReason(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: "sess-cancel"}); err != nil {
		t.Fatalf("AddToWaitingQueue() error = %v", err)
	}

	got, err := svc.CancelWaiting(context.Background(), CancelWaitingCommand{SessionID: "sess-cancel", Reason: " customer resolved "})
	if err != nil {
		t.Fatalf("CancelWaiting() error = %v", err)
	}
	if got.Status != string(domain.QueueStatusCancelled) {
		t.Fatalf("status = %q", got.Status)
	}
	if got.Notes != "customer resolved" {
		t.Fatalf("notes = %q", got.Notes)
	}
}

func TestServiceListWaitingEntriesAppliesDefaults(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	for _, s := range []string{"sess-a", "sess-b"} {
		if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: s}); err != nil {
			t.Fatalf("AddToWaitingQueue() error = %v", err)
		}
	}

	// Empty status defaults to waiting; limit is clamped to 50 by the stub query below.
	got, err := svc.ListWaitingEntries(context.Background(), "  ", 0)
	if err != nil {
		t.Fatalf("ListWaitingEntries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	for _, entry := range got {
		if entry.Status != string(domain.QueueStatusWaiting) {
			t.Fatalf("status = %q, want waiting", entry.Status)
		}
	}
}

func TestServiceListRecentTransferHistoryClampsLimit(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	if _, err := svc.AssignAgent(context.Background(), AssignAgentCommand{SessionID: "sess-1", AgentID: 1}); err != nil {
		t.Fatalf("AssignAgent() error = %v", err)
	}

	// limit <= 0 and limit > 200 both clamp to the default 50.
	for _, limit := range []int{0, -1, 500} {
		got, err := svc.ListRecentTransferHistory(context.Background(), limit)
		if err != nil {
			t.Fatalf("ListRecentTransferHistory(%d) error = %v", limit, err)
		}
		if len(got) != 1 {
			t.Fatalf("ListRecentTransferHistory(%d) entries = %d, want 1", limit, len(got))
		}
	}
}

func TestServiceListWaitingEntriesInvalidStatusReturnsEmpty(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: "sess-w"}); err != nil {
		t.Fatalf("AddToWaitingQueue() error = %v", err)
	}

	got, err := svc.ListWaitingEntries(context.Background(), "transferred", 10)
	if err != nil {
		t.Fatalf("ListWaitingEntries() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("entries = %d, want 0 for non-matching status", len(got))
	}
}

type failingRoutingPublisher struct{}

func (failingRoutingPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	return errors.New("publish failed")
}

func TestServiceAssignAgentToleratesPublishFailure(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, failingRoutingPublisher{})

	got, err := svc.AssignAgent(context.Background(), AssignAgentCommand{SessionID: "sess-pub", AgentID: 3})
	if err != nil {
		t.Fatalf("AssignAgent() error = %v", err)
	}
	if got.SessionID != "sess-pub" {
		t.Fatalf("session id = %q", got.SessionID)
	}
}

func TestServiceMarkWaitingTransferredDefaultsTime(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)
	now := time.Now()
	svc.now = func() time.Time { return now }

	if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: "sess-t"}); err != nil {
		t.Fatalf("AddToWaitingQueue() error = %v", err)
	}

	got, err := svc.MarkWaitingTransferred(context.Background(), MarkWaitingTransferredCommand{SessionID: "sess-t", AssignedTo: 7})
	if err != nil {
		t.Fatalf("MarkWaitingTransferred() error = %v", err)
	}
	if got.Status != string(domain.QueueStatusTransferred) {
		t.Fatalf("status = %q", got.Status)
	}
	if got.AssignedTo == nil || *got.AssignedTo != 7 {
		t.Fatalf("assigned to = %+v", got.AssignedTo)
	}
	if got.AssignedAt == nil || !got.AssignedAt.Equal(now) {
		t.Fatalf("assigned at = %+v, want %v", got.AssignedAt, now)
	}
}
