package services

import (
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newShiftTestService(t *testing.T) (*ShiftService, *gorm.DB) {
	t.Helper()
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.ShiftSchedule{})
	return NewShiftService(db, logrus.New()), db
}

func TestShiftService_CreateShift(t *testing.T) {
	svc, db := newShiftTestService(t)
	ctx := unitScopedContext("t1", "w1")

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	start := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)

	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{AgentID: user.ID, ShiftType: "morning", StartTime: end, EndTime: start}); err == nil {
		t.Fatal("expected end<=start error")
	}
	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{AgentID: 999, ShiftType: "morning", StartTime: start, EndTime: end}); err == nil {
		t.Fatal("expected agent not found error")
	}

	shift, err := svc.CreateShift(ctx, &ShiftCreateRequest{AgentID: user.ID, ShiftType: "morning", StartTime: start, EndTime: end, Status: "active"})
	if err != nil {
		t.Fatalf("CreateShift: %v", err)
	}
	if shift.Status != "active" || shift.TenantID != "t1" || shift.WorkspaceID != "w1" {
		t.Fatalf("unexpected shift: %+v", shift)
	}
}

func TestShiftService_ListShifts(t *testing.T) {
	svc, db := newShiftTestService(t)
	ctx := unitScopedContext("t1", "w1")

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	base := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	for i, typ := range []string{"morning", "night"} {
		if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{
			AgentID:   user.ID,
			ShiftType: typ,
			StartTime: base.Add(time.Duration(i) * 24 * time.Hour),
			EndTime:   base.Add(time.Duration(i)*24*time.Hour + 4*time.Hour),
		}); err != nil {
			t.Fatalf("CreateShift %s: %v", typ, err)
		}
	}

	agentID := user.ID
	items, total, err := svc.ListShifts(ctx, &ShiftListRequest{
		Page:      1,
		PageSize:  10,
		AgentID:   &agentID,
		ShiftType: []string{"morning"},
		Status:    []string{"scheduled"},
		DateFrom:  timePtr(base.Truncate(24 * time.Hour)),
		DateTo:    timePtr(base.Add(48 * time.Hour)),
		SortBy:    "start_time",
		SortOrder: "weird",
	})
	if err != nil {
		t.Fatalf("ListShifts: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected list: %d %+v", total, items)
	}
	if items[0].Agent.ID == 0 {
		t.Fatalf("expected preloaded agent: %+v", items[0])
	}

	if _, _, err := svc.ListShifts(ctx, &ShiftListRequest{Page: 1, PageSize: 10, SortOrder: "desc"}); err != nil {
		t.Fatalf("ListShifts desc: %v", err)
	}
}

func TestShiftService_UpdateDelete(t *testing.T) {
	svc, db := newShiftTestService(t)
	ctx := unitScopedContext("t1", "w1")

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	start := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	shift, err := svc.CreateShift(ctx, &ShiftCreateRequest{AgentID: user.ID, ShiftType: "morning", StartTime: start, EndTime: end})
	if err != nil {
		t.Fatalf("CreateShift: %v", err)
	}

	if _, err := svc.UpdateShift(ctx, 999, &ShiftUpdateRequest{}); err == nil {
		t.Fatal("expected shift not found error")
	}

	newStart := start.Add(2 * time.Hour)
	newEnd := newStart.Add(2 * time.Hour)
	updated, err := svc.UpdateShift(ctx, shift.ID, &ShiftUpdateRequest{
		ShiftType: stringPtr("evening"),
		StartTime: &newStart,
		EndTime:   &newEnd,
		Status:    stringPtr("active"),
	})
	if err != nil {
		t.Fatalf("UpdateShift: %v", err)
	}
	if updated.ShiftType != "evening" || updated.Status != "active" {
		t.Fatalf("unexpected update: %+v", updated)
	}

	if _, err := svc.UpdateShift(ctx, shift.ID, &ShiftUpdateRequest{EndTime: &newStart}); err == nil {
		t.Fatal("expected end<=start error on update")
	}

	if err := svc.DeleteShift(ctx, shift.ID); err != nil {
		t.Fatalf("DeleteShift: %v", err)
	}
	if err := svc.DeleteShift(ctx, shift.ID); err == nil {
		t.Fatal("expected delete missing error")
	}
}

func TestShiftService_GetShiftStats(t *testing.T) {
	svc, db := newShiftTestService(t)
	ctx := unitScopedContext("t1", "w1")

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	base := time.Date(2020, 1, 2, 8, 0, 0, 0, time.UTC)
	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{
		AgentID: user.ID, ShiftType: "morning", StartTime: base, EndTime: base.Add(4 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateShift past: %v", err)
	}
	now := time.Now()
	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{
		AgentID: user.ID, ShiftType: "afternoon", StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateShift today: %v", err)
	}
	future := now.Add(48 * time.Hour)
	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{
		AgentID: user.ID, ShiftType: "night", StartTime: future, EndTime: future.Add(4 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateShift future: %v", err)
	}

	stats, err := svc.GetShiftStats(ctx)
	if err != nil {
		t.Fatalf("GetShiftStats: %v", err)
	}
	if stats.Total != 3 || stats.Upcoming != 1 || stats.TodayActive != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ByType["morning"] != 1 || stats.ByStatus["scheduled"] != 3 {
		t.Fatalf("unexpected aggregates: %+v", stats)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
