//go:build integration
// +build integration

package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/routing/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newRoutingInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:routing_scope")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.TransferRecord{}, &models.WaitingRecord{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func scopedRoutingContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestRoutingRepositoryAppliesScopeOnCreateAndRead(t *testing.T) {
	db := newRoutingInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	ctxA := scopedRoutingContext("tenant-a", "workspace-a")
	ctxB := scopedRoutingContext("tenant-b", "workspace-b")

	assignment := &domain.Assignment{
		SessionID:  "sess-a",
		ToAgentID:  7,
		Reason:     "handoff",
		AssignedAt: now,
	}
	if err := repo.CreateAssignment(ctxA, assignment); err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	entry := &domain.QueueEntry{
		SessionID:    "sess-a",
		Reason:       "no_agent",
		TargetSkills: []string{"billing"},
		Priority:     "high",
		Status:       domain.QueueStatusWaiting,
		QueuedAt:     now,
	}
	if err := repo.CreateQueueEntry(ctxA, entry); err != nil {
		t.Fatalf("create queue entry: %v", err)
	}

	var storedTransfer models.TransferRecord
	if err := db.First(&storedTransfer, "session_id = ?", "sess-a").Error; err != nil {
		t.Fatalf("load transfer record: %v", err)
	}
	if storedTransfer.TenantID != "tenant-a" || storedTransfer.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected transfer scope: %+v", storedTransfer)
	}

	var storedWaiting models.WaitingRecord
	if err := db.First(&storedWaiting, "session_id = ?", "sess-a").Error; err != nil {
		t.Fatalf("load waiting record: %v", err)
	}
	if storedWaiting.TenantID != "tenant-a" || storedWaiting.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected waiting scope: %+v", storedWaiting)
	}

	records, err := repo.ListAssignments(ctxA, "sess-a")
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one assignment, got %+v", records)
	}

	records, err = repo.ListAssignments(ctxB, "sess-a")
	if err != nil {
		t.Fatalf("list assignments with mismatched scope: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected scoped assignment filter, got %+v", records)
	}

	recent, err := repo.ListRecentAssignments(ctxA, 10)
	if err != nil {
		t.Fatalf("list recent assignments: %v", err)
	}
	if len(recent) != 1 || recent[0].SessionID != "sess-a" {
		t.Fatalf("unexpected recent assignments: %+v", recent)
	}

	recent, err = repo.ListRecentAssignments(ctxB, 10)
	if err != nil {
		t.Fatalf("list recent assignments with mismatched scope: %v", err)
	}
	if len(recent) != 0 {
		t.Fatalf("expected scoped recent assignments filter, got %+v", recent)
	}

	got, err := repo.GetQueueEntry(ctxA, "sess-a")
	if err != nil {
		t.Fatalf("get queue entry: %v", err)
	}
	if got.SessionID != "sess-a" {
		t.Fatalf("unexpected queue entry: %+v", got)
	}
	if _, err := repo.GetQueueEntry(ctxB, "sess-a"); err == nil {
		t.Fatal("expected scoped lookup to reject cross-tenant waiting record")
	}
}
