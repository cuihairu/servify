package infra

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/routing/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var routingRepoCovSeq atomic.Uint64

func newRoutingRepoCoverageDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:routrepo_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(routingRepoCovSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.TransferRecord{}, &models.WaitingRecord{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func routingCovScope(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestRoutingRepoAssignmentLifecycle(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	ctx := routingCovScope("t1", "w1")
	now := time.Now()

	from := uint(2)
	assignment := &domain.Assignment{
		SessionID: "sess-1", FromAgentID: &from, ToAgentID: 9,
		Reason: "handoff", Notes: "vip", SessionSummary: "s", AssignedAt: now,
	}
	require.NoError(t, repo.CreateAssignment(ctx, assignment))
	assert.False(t, assignment.AssignedAt.IsZero())

	var stored models.TransferRecord
	require.NoError(t, db.First(&stored, "session_id = ?", "sess-1").Error)
	assert.Equal(t, "t1", stored.TenantID)
	assert.Equal(t, "w1", stored.WorkspaceID)
	require.NotNil(t, stored.ToAgentID)
	assert.Equal(t, uint(9), *stored.ToAgentID)

	err := repo.CreateAssignment(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assignment required")

	bare := &domain.Assignment{SessionID: "sess-bare", ToAgentID: 3, AssignedAt: now}
	require.NoError(t, repo.CreateAssignment(context.Background(), bare))
	var bareModel models.TransferRecord
	require.NoError(t, db.First(&bareModel, "session_id = ?", "sess-bare").Error)
	assert.Empty(t, bareModel.TenantID)
	assert.Empty(t, bareModel.WorkspaceID)

	items, err := repo.ListAssignments(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].ToAgentID)
	assert.Equal(t, uint(9), *items[0].ToAgentID)
	assert.Equal(t, "handoff", items[0].Reason)

	later := &domain.Assignment{SessionID: "sess-1", ToAgentID: 10, AssignedAt: now.Add(time.Minute)}
	require.NoError(t, repo.CreateAssignment(ctx, later))
	items, err = repo.ListAssignments(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.NotNil(t, items[0].ToAgentID)
	assert.Equal(t, uint(10), *items[0].ToAgentID) // transferred_at DESC

	recent, err := repo.ListRecentAssignments(ctx, 1)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	assert.Equal(t, "sess-1", recent[0].SessionID)
}

func TestRoutingRepoListRecentAssignmentsLimitClamp(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	rows := make([]models.TransferRecord, 0, 60)
	for i := 0; i < 60; i++ {
		rows = append(rows, models.TransferRecord{
			SessionID: "sess", ToAgentID: uintPtr(uint(i + 1)),
			TransferredAt: now.Add(time.Duration(i) * time.Second), CreatedAt: now,
		})
	}
	require.NoError(t, db.Create(&rows).Error)

	items, err := repo.ListRecentAssignments(context.Background(), 0)
	require.NoError(t, err)
	assert.Len(t, items, 50) // limit<=0 归一化为 50

	items, err = repo.ListRecentAssignments(context.Background(), 500)
	require.NoError(t, err)
	assert.Len(t, items, 50) // limit>200 归一化为 50
}

func TestRoutingRepoQueueEntryLifecycle(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q1", Reason: "no_agent", TargetSkills: []string{"billing"},
		Priority: "urgent", Status: domain.QueueStatusWaiting, QueuedAt: now,
	}))
	require.NoError(t, repo.CreateQueueEntry(ctx, &domain.QueueEntry{
		SessionID: "q2", Priority: "low", Status: domain.QueueStatusWaiting, QueuedAt: now.Add(time.Second),
	}))

	err := repo.CreateQueueEntry(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue entry required")

	got, err := repo.GetQueueEntry(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, "q1", got.SessionID)
	assert.Equal(t, []string{"billing"}, got.TargetSkills)
	assert.Equal(t, domain.QueueStatusWaiting, got.Status)

	_, err = repo.GetQueueEntry(ctx, "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))

	entries, err := repo.ListQueueEntries(ctx, "waiting", 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "q1", entries[0].SessionID) // priority DESC, queued_at ASC

	clamped, err := repo.ListQueueEntries(ctx, "waiting", 0) // limit<=0 归一化为 50
	require.NoError(t, err)
	assert.Len(t, clamped, 2)

	got.Reason = "updated"
	got.Notes = "n2"
	got.Status = domain.QueueStatusCancelled
	require.NoError(t, repo.UpdateQueueEntry(ctx, got))
	refreshed, err := repo.GetQueueEntry(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, "updated", refreshed.Reason)
	assert.Equal(t, "n2", refreshed.Notes)
	assert.Equal(t, domain.QueueStatusCancelled, refreshed.Status)

	err = repo.UpdateQueueEntry(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue entry required")

	marked, err := repo.MarkQueueEntryTransferred(ctx, "q2", 7, now)
	require.NoError(t, err)
	assert.Equal(t, domain.QueueStatusTransferred, marked.Status)
	require.NotNil(t, marked.AssignedTo)
	assert.Equal(t, uint(7), *marked.AssignedTo)
	require.NotNil(t, marked.AssignedAt)

	_, err = repo.MarkQueueEntryTransferred(ctx, "missing", 7, now)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestRoutingRepoScopeFilters(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	require.NoError(t, repo.CreateQueueEntry(routingCovScope("t1", ""), &domain.QueueEntry{
		SessionID: "q-tenant", Status: domain.QueueStatusWaiting, QueuedAt: now,
	}))
	require.NoError(t, repo.CreateQueueEntry(platformauth.ContextWithScope(context.Background(), "", "w1"), &domain.QueueEntry{
		SessionID: "q-workspace", Status: domain.QueueStatusWaiting, QueuedAt: now,
	}))

	entries, err := repo.ListQueueEntries(routingCovScope("t1", ""), "waiting", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "q-tenant", entries[0].SessionID)

	entries, err = repo.ListQueueEntries(platformauth.ContextWithScope(context.Background(), "", "w1"), "waiting", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "q-workspace", entries[0].SessionID)

	// scope 不匹配的更新影响 0 行
	entry := &domain.QueueEntry{SessionID: "q-tenant", Reason: "nope", Status: domain.QueueStatusCancelled, QueuedAt: now}
	require.NoError(t, repo.UpdateQueueEntry(routingCovScope("t2", "w2"), entry))
	kept, err := repo.GetQueueEntry(routingCovScope("t1", ""), "q-tenant")
	require.NoError(t, err)
	assert.Equal(t, domain.QueueStatusWaiting, kept.Status)

	// 无 scope 查询可见全部
	all, err := repo.ListQueueEntries(context.Background(), "waiting", 10)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestRoutingRepoQueryFailures(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.TransferRecord{}))
	require.NoError(t, db.Migrator().DropTable(&models.WaitingRecord{}))
	ctx := context.Background()

	_, err := repo.ListAssignments(ctx, "s")
	require.Error(t, err)

	_, err = repo.ListRecentAssignments(ctx, 10)
	require.Error(t, err)

	err = repo.CreateAssignment(ctx, &domain.Assignment{SessionID: "s", ToAgentID: 1})
	require.Error(t, err)

	err = repo.CreateQueueEntry(ctx, &domain.QueueEntry{SessionID: "s"})
	require.Error(t, err)

	_, err = repo.GetQueueEntry(ctx, "s")
	require.Error(t, err)

	_, err = repo.ListQueueEntries(ctx, "waiting", 10)
	require.Error(t, err)

	err = repo.UpdateQueueEntry(ctx, &domain.QueueEntry{SessionID: "s"})
	require.Error(t, err)

	_, err = repo.MarkQueueEntryTransferred(ctx, "s", 1, time.Now())
	require.Error(t, err)
}

func TestRoutingRepoMappingHelpers(t *testing.T) {
	assert.Equal(t, domain.QueueStatusTransferred, mapQueueStatus(" TRANSFERRED "))
	assert.Equal(t, domain.QueueStatusCancelled, mapQueueStatus("cancelled"))
	assert.Equal(t, domain.QueueStatusWaiting, mapQueueStatus("weird"))
	assert.Equal(t, domain.QueueStatusWaiting, mapQueueStatus(""))

	assert.Empty(t, marshalSkills(nil))
	assert.Equal(t, `["a","b"]`, marshalSkills([]string{"a", "b"}))

	assert.Nil(t, unmarshalSkills("  "))
	assert.Equal(t, []string{"a"}, unmarshalSkills(`["a"]`))
	assert.Equal(t, []string{"legacy"}, unmarshalSkills("legacy"))

	applyRoutingTransferScopeFields(context.Background(), nil)
	applyRoutingWaitingScopeFields(context.Background(), nil)

	transfer := &models.TransferRecord{}
	applyRoutingTransferScopeFields(routingCovScope("t9", "w9"), transfer)
	assert.Equal(t, "t9", transfer.TenantID)
	assert.Equal(t, "w9", transfer.WorkspaceID)

	waiting := &models.WaitingRecord{}
	applyRoutingWaitingScopeFields(routingCovScope("t9", "w9"), waiting)
	assert.Equal(t, "t9", waiting.TenantID)
	assert.Equal(t, "w9", waiting.WorkspaceID)
}
