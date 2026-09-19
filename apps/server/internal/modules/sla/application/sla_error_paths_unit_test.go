package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newSLAErrorDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newSLATestDB(t, &models.Ticket{}, &models.SLAConfig{}, &models.SLAViolation{}, &models.Customer{}, &models.User{})
}

func TestSLA_ConfigTableErrors(t *testing.T) {
	ctx := context.Background()

	// CreateSLAConfig: existing-config check fails
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("sla_configs"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	}); err == nil {
		t.Fatal("expected existing-config check error")
	}

	// GetSLAConfig: non-notfound error
	if _, err := svc.GetSLAConfig(ctx, 1); err == nil || err.Error() == "SLA config not found" {
		t.Fatalf("expected query error, got %v", err)
	}

	// ListSLAConfigs: count error
	if _, _, err := svc.ListSLAConfigs(ctx, &SLAConfigListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list count error")
	}

	// UpdateSLAConfig: find error
	if _, err := svc.UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{}); err == nil || err.Error() == "SLA config not found" {
		t.Fatalf("expected find error, got %v", err)
	}

	// GetSLAConfigByPriority: query errors
	if _, err := svc.GetSLAConfigByPriority(ctx, "high", "vip"); err == nil {
		t.Fatal("expected tier lookup error")
	}
	if _, err := svc.GetSLAConfigByPriority(ctx, "high", ""); err == nil {
		t.Fatal("expected fallback lookup error")
	}
}

func TestSLA_DeleteConfigTableErrors(t *testing.T) {
	ctx := context.Background()

	// violation count fails when sla_violations is missing
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if err := svc.DeleteSLAConfig(ctx, 1); err == nil {
		t.Fatal("expected violation count error")
	}

	// delete fails when sla_configs is missing but violations table is empty
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	if err := db2.Migrator().DropTable("sla_configs"); err != nil {
		t.Fatalf("drop configs: %v", err)
	}
	if err := svc2.DeleteSLAConfig(ctx, 1); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestSLA_CheckViolationTableErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// ticket validation query fails
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.CheckSLAViolation(ctx, &models.Ticket{ID: 1}); err == nil || err.Error() == "ticket not found" {
		t.Fatalf("expected ticket query error, got %v", err)
	}

	// config lookup fails with tickets present
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	ticket := &models.Ticket{Title: "T", Priority: "high", Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db2.Migrator().DropTable("sla_configs"); err != nil {
		t.Fatalf("drop configs: %v", err)
	}
	if _, err := svc2.CheckSLAViolation(ctx, ticket); err == nil {
		t.Fatal("expected config lookup error")
	}

	// existing-violation check fails
	db3 := newSLAErrorDB(t)
	svc3 := NewService(db3, logrus.New())
	cfg := &models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db3.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	late := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := db3.Create(late).Error; err != nil {
		t.Fatalf("seed late ticket: %v", err)
	}
	if err := db3.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if _, err := svc3.CheckSLAViolation(ctx, late); err == nil {
		t.Fatal("expected existing violation check error")
	}
}

func TestSLA_DetectViolation_ZeroCreatedAt(t *testing.T) {
	svc := NewService(newSLAErrorDB(t), logrus.New())
	now := time.Now()
	cfg := &models.SLAConfig{ID: 1, FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30}

	// zero CreatedAt falls back to now -> no violation
	zero := &models.Ticket{ID: 1, Status: "open"}
	if v := svc.detectViolation(zero, cfg, now); v != nil {
		t.Fatalf("expected no violation, got %+v", v)
	}
}

func TestSLA_CreateViolationTableErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// ticket lookup fails
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if err := svc.CreateSLAViolation(ctx, &models.SLAViolation{TicketID: 1, SLAConfigID: 1, ViolationType: "first_response"}); err == nil {
		t.Fatal("expected ticket lookup error")
	}

	// config lookup fails
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	ticket := &models.Ticket{Title: "T", CreatedAt: now, UpdatedAt: now}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db2.Migrator().DropTable("sla_configs"); err != nil {
		t.Fatalf("drop configs: %v", err)
	}
	if err := svc2.CreateSLAViolation(ctx, &models.SLAViolation{TicketID: ticket.ID, SLAConfigID: 1, ViolationType: "first_response"}); err == nil {
		t.Fatal("expected config lookup error")
	}
}

func TestSLA_ListResolveViolationTableErrors(t *testing.T) {
	ctx := context.Background()

	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if _, _, err := svc.ListSLAViolations(ctx, &SLAViolationListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list violations error")
	}
	if err := svc.ResolveSLAViolation(ctx, 1); err == nil || err.Error() == "SLA violation not found" {
		t.Fatalf("expected resolve query error, got %v", err)
	}
	if err := svc.ResolveViolationsByTicket(ctx, 1, nil); err == nil {
		t.Fatal("expected resolve-by-ticket query error")
	}

	// ticket exists, violation updates fail
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db2.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if err := svc2.ResolveViolationsByTicket(ctx, ticket.ID, nil); err == nil {
		t.Fatal("expected resolve updates error")
	}
}

func TestSLA_StatsTableErrors(t *testing.T) {
	ctx := context.Background()

	// config count fails
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("sla_configs"); err != nil {
		t.Fatalf("drop configs: %v", err)
	}
	if _, err := svc.GetSLAStats(ctx); err == nil {
		t.Fatal("expected config count error")
	}

	// violations count fails
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	if err := db2.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if _, err := svc2.GetSLAStats(ctx); err == nil {
		t.Fatal("expected violations count error")
	}

	// tickets count fails
	db3 := newSLAErrorDB(t)
	svc3 := NewService(db3, logrus.New())
	if err := db3.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc3.GetSLAStats(ctx); err == nil {
		t.Fatal("expected tickets count error")
	}
}

func TestSLA_TrendDataErrors(t *testing.T) {
	ctx := context.Background()

	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.getSLATrendData(ctx, 1); err == nil {
		t.Fatal("expected ticket trend error")
	}

	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	if err := db2.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if _, err := svc2.getSLATrendData(ctx, 1); err == nil {
		t.Fatal("expected violation trend error")
	}
}

func TestSLA_ResolveCustomerTierErrors(t *testing.T) {
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	if err := db.Migrator().DropTable("customers"); err != nil {
		t.Fatalf("drop customers: %v", err)
	}
	if got := svc.resolveCustomerTier(context.Background(), 42); got != "" {
		t.Fatalf("expected empty tier on error, got %q", got)
	}
}

func TestSLA_MonitorWithViolations(t *testing.T) {
	db := newSLAErrorDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()
	ctx := context.Background()

	cfg := &models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	// cross-scope ticket forces CheckSLAViolation error inside monitor loop
	scopedTicket := &models.Ticket{
		Title: "Scoped", Priority: "high", Status: "open", TenantID: "other", WorkspaceID: "other",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}
	if err := db.Create(scopedTicket).Error; err != nil {
		t.Fatalf("seed scoped ticket: %v", err)
	}
	// same-scope violating ticket counts as violation
	violating := &models.Ticket{
		Title: "Violating", Priority: "high", Status: "open",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}
	if err := db.Create(violating).Error; err != nil {
		t.Fatalf("seed violating ticket: %v", err)
	}
	// closed ticket skipped by monitor query
	closed := &models.Ticket{
		Title: "Closed", Priority: "high", Status: "closed",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}
	if err := db.Create(closed).Error; err != nil {
		t.Fatalf("seed closed ticket: %v", err)
	}

	if err := svc.monitorSLAViolations(ctx); err != nil {
		t.Fatalf("monitorSLAViolations: %v", err)
	}

	// Find error branch
	db2 := newSLAErrorDB(t)
	svc2 := NewService(db2, logrus.New())
	if err := db2.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if err := svc2.monitorSLAViolations(ctx); err == nil {
		t.Fatal("expected monitor find error")
	}
}
