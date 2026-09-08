package audit

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

func TestGormRetentionServiceCleanup(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	logs := []models.AuditLog{
		{Action: "old-1", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/1", Method: "POST", CreatedAt: now.Add(-400 * 24 * time.Hour)},
		{Action: "old-2", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/2", Method: "POST", CreatedAt: now.Add(-300 * 24 * time.Hour)},
		{Action: "fresh", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/3", Method: "POST", CreatedAt: now.Add(-30 * 24 * time.Hour)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormRetentionService(db, 180*24*time.Hour, 1)
	deleted, err := svc.Cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d want 2", deleted)
	}

	var remaining []models.AuditLog
	if err := db.Order("created_at asc").Find(&remaining).Error; err != nil {
		t.Fatalf("query remaining: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Action != "fresh" {
		t.Fatalf("unexpected remaining logs: %+v", remaining)
	}
}

func TestGormRetentionServiceCleanupNoopWhenDisabled(t *testing.T) {
	db := openTestDB(t)
	svc := NewGormRetentionService(db, 0, 10)
	if svc != nil {
		t.Fatal("expected nil service for non-positive retention")
	}
}

func TestGormRetentionServiceCleanupRetainsBoundaryTimestamp(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	cutoff := now.Add(-180 * 24 * time.Hour)
	logs := []models.AuditLog{
		{Action: "expired", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/1", Method: "POST", CreatedAt: cutoff.Add(-time.Second)},
		{Action: "boundary", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/2", Method: "POST", CreatedAt: cutoff},
		{Action: "fresh", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/3", Method: "POST", CreatedAt: cutoff.Add(time.Second)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormRetentionService(db, 180*24*time.Hour, 10)
	deleted, err := svc.Cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d want 1", deleted)
	}

	var remaining []models.AuditLog
	if err := db.Order("created_at asc").Find(&remaining).Error; err != nil {
		t.Fatalf("query remaining: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining count = %d want 2", len(remaining))
	}
	if remaining[0].Action != "boundary" || remaining[1].Action != "fresh" {
		t.Fatalf("unexpected remaining logs: %+v", remaining)
	}
}

func TestNewGormRetentionServiceDefaults(t *testing.T) {
	db := openTestDB(t)
	if NewGormRetentionService(nil, time.Hour, 1) != nil {
		t.Fatal("expected nil service for nil db")
	}
	svc := NewGormRetentionService(db, time.Hour, 0)
	if svc == nil || svc.batchSize != 500 {
		t.Fatalf("expected default batch size 500, got %+v", svc)
	}
	var nilSvc *GormRetentionService
	if deleted, err := nilSvc.Cleanup(context.Background(), time.Now()); deleted != 0 || err != nil {
		t.Fatalf("nil receiver Cleanup = (%d, %v)", deleted, err)
	}
}

func TestGormRetentionServiceCleanupBatches(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	var logs []models.AuditLog
	for i := 0; i < 5; i++ {
		logs = append(logs, models.AuditLog{Action: "batch", CreatedAt: now.Add(-time.Duration(400-i) * 24 * time.Hour)})
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormRetentionService(db, 180*24*time.Hour, 2)
	deleted, err := svc.Cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 5 {
		t.Fatalf("deleted = %d want 5", deleted)
	}
}

func TestGormRetentionServiceCleanupErrors(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrator().DropTable(&models.AuditLog{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	svc := NewGormRetentionService(db, time.Hour, 1)
	if _, err := svc.Cleanup(context.Background(), time.Now()); err == nil {
		t.Fatal("expected error when table missing")
	}
}
