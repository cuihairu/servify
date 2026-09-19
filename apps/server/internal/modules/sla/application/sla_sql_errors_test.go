package application

// SLA 落库错误分支：丢表、trigger 注入、顺序查询失败（自 services
// error_paths/more_branches/sequential_errors/trigger_error_paths 单测的
// SLA 段下沉）。

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func execTrigger(t *testing.T, db *gorm.DB, stmt string) {
	t.Helper()
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

// ---- insert-error branches via BEFORE INSERT triggers ----

func bcryptHash(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	return string(b), err
}

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

func TestSLAService_MoreBranches(t *testing.T) {
	ctx := context.Background()

	// config insert error
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	execTrigger(t, db, "CREATE TRIGGER blk_cfg BEFORE INSERT ON sla_configs BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if _, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	}); err == nil {
		t.Fatal("expected config insert error")
	}

	// priority-change conflict and combined priority+tier conflict
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	if _, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", CustomerTier: "vip",
		FirstResponseTime: 1, ResolutionTime: 60, EscalationTime: 30,
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "B", Priority: "high", FirstResponseTime: 2, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	// B (tier '') -> tier vip: combined conflict on priority+tier
	if _, err := svc2.UpdateSLAConfig(ctx, b.ID, &SLAConfigUpdateRequest{CustomerTier: stringPtr("vip")}); err == nil {
		t.Fatal("expected combined priority+tier conflict")
	}
	// low -> high conflicts with existing high config
	c, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "C", Priority: "low", FirstResponseTime: 2, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create C: %v", err)
	}
	if _, err := svc2.UpdateSLAConfig(ctx, c.ID, &SLAConfigUpdateRequest{Priority: stringPtr("high")}); err == nil {
		t.Fatal("expected priority conflict")
	}
	// resolution + valid escalation assignments
	updated, err := svc2.UpdateSLAConfig(ctx, c.ID, &SLAConfigUpdateRequest{
		ResolutionTime: intPtr(90),
		EscalationTime: intPtr(45),
	})
	if err != nil {
		t.Fatalf("update C times: %v", err)
	}
	if updated.ResolutionTime != 90 || updated.EscalationTime != 45 {
		t.Fatalf("unexpected times: %+v", updated)
	}

	// monitor loop with per-ticket error (violations table dropped mid-flow)
	db3 := newSLAErrorDB(t)
	svc3 := NewService(db3, logrus.New())
	now := time.Now()
	cfg := &models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db3.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	late := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := db3.Create(late).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db3.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if err := svc3.monitorSLAViolations(ctx); err != nil {
		t.Fatalf("monitor with ticket errors: %v", err)
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
		svc := NewService(db, logrus.New())
		_, err := svc.GetSLAStats(context.Background())
		if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Fatalf("n=%d: expected %q, got %v", tc.n, tc.wantSubstr, err)
		}
	}

	// trend failure is logged, not returned
	db := newSLAErrorDB(t)
	seedSLAStatsData(t, db)
	failNthQuery(db, 9)
	svc := NewService(db, logrus.New())
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
	svc := NewService(db, logrus.New())
	if _, _, err := svc.ListSLAConfigs(ctx, &SLAConfigListRequest{Page: 1, PageSize: 10}); err == nil ||
		!strings.Contains(err.Error(), "failed to list SLA configs") {
		t.Fatalf("config find error: %v", err)
	}

	db2 := newSLAErrorDB(t)
	failNthQuery(db2, 2)
	svc2 := NewService(db2, logrus.New())
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
	svc := NewService(db, logrus.New())
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
	svc2 := NewService(db2, logrus.New())
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
	svc3 := NewService(db3, logrus.New())
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
	svc := NewService(db, logrus.New())
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

func TestSLA_InsertTriggerErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// CheckSLAViolation: violation insert fails
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	cfg := &models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	late := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := db.Create(late).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_sla_ins BEFORE INSERT ON sla_violations BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if _, err := svc.CheckSLAViolation(ctx, late); err == nil {
		t.Fatal("expected violation insert error")
	}

	// CreateSLAViolation: insert fails after validations
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	ticket := &models.Ticket{Title: "T", CreatedAt: now, UpdatedAt: now}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	cfg2 := &models.SLAConfig{
		Name: "C2", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db2.Create(cfg2).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	execTrigger(t, db2, "CREATE TRIGGER blk_sla2 BEFORE INSERT ON sla_violations BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if err := svc2.CreateSLAViolation(ctx, &models.SLAViolation{
		TicketID: ticket.ID, SLAConfigID: cfg2.ID, ViolationType: "first_response",
		Deadline: now, ViolatedAt: now,
	}); err == nil {
		t.Fatal("expected violation create error")
	}
}

func TestSLA_ResolveTicketNonNotFound(t *testing.T) {
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if err := svc.ResolveViolationsByTicket(context.Background(), 1, nil); err == nil || err.Error() == "ticket not found" {
		t.Fatalf("expected non-notfound ticket error, got %v", err)
	}
}

// ---- webrtc additional branches ----

func TestSLA_UpdateConfigSaveTriggerError(t *testing.T) {
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	ctx := context.Background()

	cfg, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_cfg_upd BEFORE UPDATE ON sla_configs BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc.UpdateSLAConfig(ctx, cfg.ID, &SLAConfigUpdateRequest{Name: stringPtr("B")}); err == nil ||
		!strings.Contains(err.Error(), "failed to update SLA config") {
		t.Fatalf("expected save error, got %v", err)
	}
}
