package services

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// failNthQuery registers gorm callbacks that force the n-th SELECT (including
// raw Scan/Row reads) on this connection to fail. Used to exercise sequential
// error branches unreachable with plain table drops.
func failNthQuery(db *gorm.DB, n int32) {
	var calls int32
	bump := func(tx *gorm.DB) {
		if atomic.AddInt32(&calls, 1) == n {
			_ = tx.AddError(errors.New("forced nth query failure"))
		}
	}
	_ = db.Callback().Query().Before("gorm:query").Register("fail_nth", bump)
	_ = db.Callback().Row().Before("gorm:row").Register("fail_nth_row", bump)
}

func seedSLAStatsData(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	if err := db.Create(&models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.Create(&models.Ticket{
		Title: "T", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Create(&models.SLAViolation{
		TicketID: 1, SLAConfigID: 1, ViolationType: "first_response",
		Deadline: now, ViolatedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed violation: %v", err)
	}
}

func TestSLA_GetStats_SequentialErrors(t *testing.T) {
	cases := []struct {
		n          int32
		wantSubstr string
	}{
		{2, "failed to count active SLA configs"},
		{4, "failed to count unresolved violations"},
		{5, "failed to get violation type stats"},
		{6, "failed to get violation priority stats"},
	}
	for _, tc := range cases {
		db := newSLAErrorDB(t)
		seedSLAStatsData(t, db)
		failNthQuery(db, tc.n)
		svc := NewSLAService(db, logrus.New())
		_, err := svc.GetSLAStats(context.Background())
		if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Fatalf("n=%d: expected %q, got %v", tc.n, tc.wantSubstr, err)
		}
	}

	// trend failure is logged, not returned
	db := newSLAErrorDB(t)
	seedSLAStatsData(t, db)
	failNthQuery(db, 9)
	svc := NewSLAService(db, logrus.New())
	stats, err := svc.GetSLAStats(context.Background())
	if err != nil {
		t.Fatalf("trend failure should not propagate: %v", err)
	}
	if len(stats.TrendData) != 0 {
		t.Fatalf("expected empty trend data on failure, got %+v", stats.TrendData)
	}
}

func TestSLA_ListFindErrors(t *testing.T) {
	ctx := context.Background()

	db := newSLAErrorDB(t)
	failNthQuery(db, 2)
	svc := NewSLAService(db, logrus.New())
	if _, _, err := svc.ListSLAConfigs(ctx, &SLAConfigListRequest{Page: 1, PageSize: 10}); err == nil ||
		!strings.Contains(err.Error(), "failed to list SLA configs") {
		t.Fatalf("config find error: %v", err)
	}

	db2 := newSLAErrorDB(t)
	failNthQuery(db2, 2)
	svc2 := NewSLAService(db2, logrus.New())
	if _, _, err := svc2.ListSLAViolations(ctx, &SLAViolationListRequest{Page: 1, PageSize: 10}); err == nil ||
		!strings.Contains(err.Error(), "failed to list SLA violations") {
		t.Fatalf("violation find error: %v", err)
	}
}

func TestSLA_UpdateConfigCheckErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// priority-change path: priority check (#2) fails
	db := newSLAErrorDB(t)
	svc := NewSLAService(db, logrus.New())
	cfg, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	failNthQuery(db, 2)
	if _, err := svc.UpdateSLAConfig(ctx, cfg.ID, &SLAConfigUpdateRequest{Priority: stringPtr("low")}); err == nil ||
		!strings.Contains(err.Error(), "failed to check existing config") {
		t.Fatalf("priority check error: %v", err)
	}

	// priority-change path: combined check (#3) fails
	db2 := newSLAErrorDB(t)
	svc2 := NewSLAService(db2, logrus.New())
	cfg2, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "B", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	failNthQuery(db2, 3)
	if _, err := svc2.UpdateSLAConfig(ctx, cfg2.ID, &SLAConfigUpdateRequest{Priority: stringPtr("low")}); err == nil ||
		!strings.Contains(err.Error(), "failed to check existing config") {
		t.Fatalf("combined check error (priority change): %v", err)
	}

	// no priority change: combined check (#2) fails
	db3 := newSLAErrorDB(t)
	svc3 := NewSLAService(db3, logrus.New())
	cfg3, err := svc3.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create C: %v", err)
	}
	failNthQuery(db3, 2)
	if _, err := svc3.UpdateSLAConfig(ctx, cfg3.ID, &SLAConfigUpdateRequest{Name: stringPtr("C2")}); err == nil ||
		!strings.Contains(err.Error(), "failed to check existing config") {
		t.Fatalf("combined check error: %v", err)
	}
	_ = now
}

func TestSLA_CheckViolationReloadError(t *testing.T) {
	db := newSLAErrorDB(t)
	now := time.Now()
	svc := NewSLAService(db, logrus.New())
	ctx := context.Background()

	if err := db.Create(&models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	ticket := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	// queries: 1 ticket lookup, 2 config lookup, 3 existing violation, 4 reload
	failNthQuery(db, 4)
	if _, err := svc.CheckSLAViolation(ctx, ticket); err == nil ||
		!strings.Contains(err.Error(), "failed to load created SLA violation") {
		t.Fatalf("reload error: %v", err)
	}
}

func TestRouter_EnsureSessionNoopUpdate(t *testing.T) {
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	if err := db.Create(&models.Session{ID: "blank", Status: "active", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed blank session: %v", err)
	}
	// RAISE(IGNORE) makes the scope-upgrade update a no-op (RowsAffected == 0)
	execTrigger(t, db, "CREATE TRIGGER ign_sess BEFORE UPDATE ON sessions BEGIN SELECT RAISE(IGNORE); END;")
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)
	if _, _, err := r.ensureSession("blank", "web", "t1", "w1"); err == nil {
		t.Fatal("expected RowsAffected==0 error")
	}
}

func TestWebSocket_AsICECandidateDecodeError(t *testing.T) {
	if _, err := asICECandidate(map[string]interface{}{"candidate": 123}); err == nil {
		t.Fatal("expected decode error for numeric candidate")
	}
}
