package application

import (
	"context"
	"time"

	"servify/apps/server/internal/modules/routing/domain"
)

type RoutingRepository interface {
	CreateAssignment(ctx context.Context, assignment *domain.Assignment) error
	ListAssignments(ctx context.Context, sessionID string) ([]domain.TransferRecord, error)
	ListRecentAssignments(ctx context.Context, limit int) ([]domain.TransferRecord, error)
	CreateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error
	GetQueueEntry(ctx context.Context, sessionID string) (*domain.QueueEntry, error)
	ListQueueEntries(ctx context.Context, status string, limit int) ([]domain.QueueEntry, error)
	UpdateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error
	MarkQueueEntryTransferred(ctx context.Context, sessionID string, agentID uint, assignedAt time.Time) (*domain.QueueEntry, error)
	// ClaimQueueEntries 原子认领到期等待记录（claimed_at=now；仅命中无租约或租约过期者）。
	ClaimQueueEntries(ctx context.Context, now, leaseBefore time.Time, limit int) ([]domain.QueueEntry, error)
	// ReleaseQueueClaim 归还租约（claimed_at 清空），下一轮可再认领。
	ReleaseQueueClaim(ctx context.Context, sessionID string) error
}
