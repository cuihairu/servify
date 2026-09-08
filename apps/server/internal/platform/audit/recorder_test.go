package audit

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

func uintPtr(v uint) *uint { return &v }

func TestNewGormRecorderNilDB(t *testing.T) {
	if NewGormRecorder(nil) != nil {
		t.Fatal("expected nil recorder for nil db")
	}
}

func TestGormRecorderNilReceiver(t *testing.T) {
	var recorder *GormRecorder
	if err := recorder.Record(context.Background(), Entry{Action: "a"}); err != nil {
		t.Fatalf("Record() on nil receiver error = %v", err)
	}
}

func TestGormRecorderRecordPersistsEntry(t *testing.T) {
	db := openTestDB(t)
	recorder := NewGormRecorder(db)
	actorID := uint(11)
	err := recorder.Record(context.Background(), Entry{
		ActorUserID:   &actorID,
		PrincipalKind: "admin",
		Action:        "tickets.update",
		ResourceType:  "tickets",
		ResourceID:    "42",
		Route:         "/api/tickets/42",
		Method:        "PUT",
		StatusCode:    200,
		Success:       true,
		RequestID:     "req-42",
		ClientIP:      "127.0.0.1",
		UserAgent:     "unit-test",
		TenantID:      "tenant-a",
		WorkspaceID:   "ws-1",
		RequestJSON:   `{"title":"x"}`,
		BeforeJSON:    `{"title":"old"}`,
		AfterJSON:     `{"title":"x"}`,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var log models.AuditLog
	if err := db.First(&log, "action = ?", "tickets.update").Error; err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if log.ActorUserID == nil || *log.ActorUserID != actorID {
		t.Fatalf("actor_user_id = %v want %d", log.ActorUserID, actorID)
	}
	if log.PrincipalKind != "admin" || log.ResourceID != "42" || log.RequestID != "req-42" {
		t.Fatalf("unexpected persisted log: %+v", log)
	}
	if log.RequestJSON != `{"title":"x"}` || log.BeforeJSON != `{"title":"old"}` {
		t.Fatalf("unexpected json columns: %+v", log)
	}
}

func TestGormRecorderRecordReturnsDBError(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrator().DropTable(&models.AuditLog{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	recorder := NewGormRecorder(db)
	if err := recorder.Record(context.Background(), Entry{Action: "boom"}); err == nil {
		t.Fatal("expected error when table missing")
	}
}

func TestNewGormQueryServiceNilDB(t *testing.T) {
	if NewGormQueryService(nil) != nil {
		t.Fatal("expected nil query service for nil db")
	}
}

func TestGormQueryServiceNilReceiver(t *testing.T) {
	var svc *GormQueryService
	logs, total, err := svc.List(context.Background(), ListQuery{Page: 1, PageSize: 10})
	if err != nil || total != 0 || logs != nil {
		t.Fatalf("List() on nil receiver = (%v, %d, %v)", logs, total, err)
	}
	got, err := svc.Get(context.Background(), 1, QueryScope{})
	if err != nil || got != nil {
		t.Fatalf("Get() on nil receiver = (%v, %v)", got, err)
	}
}

func TestGormQueryServiceGet(t *testing.T) {
	db := openTestDB(t)
	seed := []models.AuditLog{
		{Action: "get-hit", PrincipalKind: "admin", ResourceType: "tickets", TenantID: "tenant-a", WorkspaceID: "ws-1"},
		{Action: "get-other-tenant", PrincipalKind: "admin", ResourceType: "tickets", TenantID: "tenant-b", WorkspaceID: "ws-9"},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormQueryService(db)

	hit, err := svc.Get(context.Background(), seed[0].ID, QueryScope{TenantID: "tenant-a", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if hit == nil || hit.Action != "get-hit" {
		t.Fatalf("Get() = %+v want get-hit", hit)
	}

	missed, err := svc.Get(context.Background(), seed[0].ID, QueryScope{TenantID: "tenant-b"})
	if err != nil || missed != nil {
		t.Fatalf("Get() wrong scope = (%v, %v) want (nil, nil)", missed, err)
	}

	missing, err := svc.Get(context.Background(), 99999, QueryScope{})
	if err != nil || missing != nil {
		t.Fatalf("Get() missing id = (%v, %v) want (nil, nil)", missing, err)
	}

	if zero, err := svc.Get(context.Background(), 0, QueryScope{}); err != nil || zero != nil {
		t.Fatalf("Get() zero id = (%v, %v) want (nil, nil)", zero, err)
	}
}

func TestGormQueryServiceGetDBError(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrator().DropTable(&models.AuditLog{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	svc := NewGormQueryService(db)
	if _, err := svc.Get(context.Background(), 1, QueryScope{}); err == nil {
		t.Fatal("expected error when table missing")
	}
}

func TestGormQueryServiceListFiltersAndPaging(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	logs := []models.AuditLog{
		{Action: "filter.a", PrincipalKind: "agent", ResourceType: "widgets", ResourceID: "1", ActorUserID: uintPtr(7), Success: true, TenantID: "tenant-a", WorkspaceID: "ws-1", CreatedAt: now.Add(-3 * time.Hour)},
		{Action: "filter.b", PrincipalKind: "service", ResourceType: "gadgets", ResourceID: "2", Success: false, TenantID: "tenant-a", WorkspaceID: "ws-1", CreatedAt: now.Add(-2 * time.Hour)},
		{Action: "filter.c", PrincipalKind: "admin", ResourceType: "widgets", ResourceID: "3", ActorUserID: uintPtr(9), Success: true, TenantID: "tenant-a", WorkspaceID: "ws-1", CreatedAt: now.Add(-1 * time.Hour)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormQueryService(db)

	items, total, err := svc.List(context.Background(), ListQuery{
		Action:        "filter.a",
		ResourceType:  "widgets",
		ResourceID:    "1",
		PrincipalKind: "agent",
		ActorUserID:   uintPtr(7),
		From:          &logs[0].CreatedAt,
		To:            &logs[0].CreatedAt,
		Page:          0,
		PageSize:      0,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Action != "filter.a" {
		t.Fatalf("List() = %+v total=%d", items, total)
	}

	items, total, err = svc.List(context.Background(), ListQuery{TenantID: "tenant-a", Page: 1, PageSize: 500})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("List() all = %d items total=%d", len(items), total)
	}
}

func TestGormQueryServiceListFailure(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrator().DropTable(&models.AuditLog{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	svc := NewGormQueryService(db)
	if _, _, err := svc.List(context.Background(), ListQuery{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected error when table missing")
	}
}
