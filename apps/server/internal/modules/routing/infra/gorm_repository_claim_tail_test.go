package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/routing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 覆盖 ClaimQueueEntries / ReleaseQueueClaim 的认领租约生命周期。
func TestRoutingRepoClaimAndReleaseLifecycle(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	// TargetGroupID 非零覆盖 nilableGroupID/derefGroupID 的非空分支
	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q-urgent", Priority: "urgent", Status: domain.QueueStatusWaiting,
		TargetGroupID: 9, QueuedAt: now.Add(2 * time.Second),
	}))
	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q-normal", Priority: "normal", Status: domain.QueueStatusWaiting, QueuedAt: now.Add(1 * time.Second),
	}))
	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q-low", Priority: "low", Status: domain.QueueStatusWaiting, QueuedAt: now,
	}))
	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q-done", Priority: "urgent", Status: domain.QueueStatusTransferred, QueuedAt: now,
	}))

	// 认领（limit<=0 归一化为 10）：非 waiting 的 q-done 不参与，按优先级排序
	claimed, err := repo.ClaimQueueEntries(ctx, now, now, 0)
	require.NoError(t, err)
	require.Len(t, claimed, 3)
	assert.Equal(t, []string{"q-urgent", "q-normal", "q-low"},
		[]string{claimed[0].SessionID, claimed[1].SessionID, claimed[2].SessionID})
	for _, entry := range claimed {
		require.NotNil(t, entry.ClaimedAt)
	}

	// 持有效租约（claimed_at >= leaseBefore）的记录不可再次认领
	again, err := repo.ClaimQueueEntries(ctx, now, now, 10)
	require.NoError(t, err)
	assert.Len(t, again, 0)

	// 租约过期后可重新认领，且 limit>200 归一化为 10
	again, err = repo.ClaimQueueEntries(ctx, now, now.Add(time.Hour), 500)
	require.NoError(t, err)
	assert.Len(t, again, 3)

	// 归还租约后 status 保持 waiting，claimed_at 清空
	require.NoError(t, repo.ReleaseQueueClaim(ctx, "q-urgent"))
	released, err := repo.GetQueueEntry(ctx, "q-urgent")
	require.NoError(t, err)
	assert.Nil(t, released.ClaimedAt)
	assert.Equal(t, domain.QueueStatusWaiting, released.Status)

	// 已归还需再次认领
	partial, err := repo.ClaimQueueEntries(ctx, now, now.Add(time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, partial, 1)
	assert.Equal(t, "q-urgent", partial[0].SessionID)

	// 非 waiting 状态的归还为 no-op
	require.NoError(t, repo.ReleaseQueueClaim(ctx, "q-done"))
}

// 覆盖 ClaimQueueEntries 的 SQL 执行失败分支。
func TestRoutingRepoClaimQueueEntriesError(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.WaitingRecord{}))

	_, err := repo.ClaimQueueEntries(context.Background(), time.Now(), time.Now(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim waiting records")
}

// TargetGroupID 非零值经 CreateQueueEntry/GetQueueEntry 往返（nilableGroupID/derefGroupID 非空分支）。
func TestRoutingRepoTargetGroupIDRoundTrip(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q-group", Priority: "high", Status: domain.QueueStatusWaiting,
		TargetGroupID: 42, QueuedAt: time.Now(),
	}))
	got, err := repo.GetQueueEntry(ctx, "q-group")
	require.NoError(t, err)
	assert.Equal(t, uint(42), got.TargetGroupID)

	var raw models.WaitingRecord
	require.NoError(t, db.Where("session_id = ?", "q-group").First(&raw).Error)
	require.NotNil(t, raw.TargetGroupID)
	assert.Equal(t, uint(42), *raw.TargetGroupID)
}

func TestRoutingRepoGroupIDHelpers(t *testing.T) {
	id := uint(7)
	got := nilableGroupID(id)
	require.NotNil(t, got)
	assert.Equal(t, uint(7), *got)
	assert.Nil(t, nilableGroupID(0))
	assert.Equal(t, uint(7), derefGroupID(got))
	assert.Equal(t, uint(0), derefGroupID(nil))
}
