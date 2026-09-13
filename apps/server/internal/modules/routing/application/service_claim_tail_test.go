package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/modules/routing/domain"
)

// claimCapturingRepo 在 stubRoutingRepo 之上注入 claim/release/update 行为，
// 覆盖 Service.ClaimWaitingRecords / ReleaseWaitingClaim / CancelWaiting 的分支。
type claimCapturingRepo struct {
	stubRoutingRepo
	entries    []domain.QueueEntry
	claimErr   error
	releaseErr error
	updateErr  error
}

func (r *claimCapturingRepo) ClaimQueueEntries(ctx context.Context, now time.Time, leaseBefore time.Time, limit int) ([]domain.QueueEntry, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	return r.entries, nil
}

func (r *claimCapturingRepo) ReleaseQueueClaim(ctx context.Context, sessionID string) error {
	return r.releaseErr
}

func (r *claimCapturingRepo) UpdateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.stubRoutingRepo.UpdateQueueEntry(ctx, entry)
}

func TestServiceClaimWaitingRecordsMapsDTOs(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	repo := &claimCapturingRepo{entries: []domain.QueueEntry{
		{SessionID: "s1", Reason: "no_agent", Priority: "high", Status: domain.QueueStatusWaiting, QueuedAt: now, TargetGroupID: 4},
	}}
	svc := NewService(repo, nil)

	entries, err := svc.ClaimWaitingRecords(ctx, now, now, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].SessionID != "s1" || entries[0].Priority != "high" || entries[0].TargetGroupID != 4 {
		t.Fatalf("unexpected mapped entries: %+v", entries)
	}

	repo.claimErr = errors.New("claim down")
	if _, err := svc.ClaimWaitingRecords(ctx, now, now, 5); err == nil || err.Error() != "claim down" {
		t.Fatalf("expected claim error, got %v", err)
	}

	if err := svc.ReleaseWaitingClaim(ctx, "s1"); err != nil {
		t.Fatalf("unexpected release error: %v", err)
	}
	repo.releaseErr = errors.New("release down")
	if err := svc.ReleaseWaitingClaim(ctx, "s1"); err == nil || err.Error() != "release down" {
		t.Fatalf("expected release error, got %v", err)
	}
}

func TestServiceCancelWaitingUpdateError(t *testing.T) {
	ctx := context.Background()
	repo := &claimCapturingRepo{}
	svc := NewService(repo, nil)
	if err := repo.CreateQueueEntry(ctx, &domain.QueueEntry{SessionID: "s2", Status: domain.QueueStatusWaiting}); err != nil {
		t.Fatalf("seed queue: %v", err)
	}

	repo.updateErr = errors.New("update down")
	if _, err := svc.CancelWaiting(ctx, CancelWaitingCommand{SessionID: "s2", Reason: "gone"}); err == nil || err.Error() != "update down" {
		t.Fatalf("expected update error, got %v", err)
	}
}
