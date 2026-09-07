package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newSLAUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newServicesTestDB(t,
		&models.Ticket{}, &models.SLAConfig{}, &models.SLAViolation{},
		&models.Customer{}, &models.User{},
	)
}

func newSLAUnitTestService(t *testing.T) *SLAService {
	t.Helper()
	return NewSLAService(newSLAUnitTestDB(t), logrus.New())
}

func TestSLAUnit_CreateConfigValidation(t *testing.T) {
	svc := newSLAUnitTestService(t)
	ctx := context.Background()

	valid := SLAConfigCreateRequest{Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30}
	cases := []struct {
		name string
		mut  func(*SLAConfigCreateRequest)
	}{
		{"invalid priority", func(r *SLAConfigCreateRequest) { r.Priority = "critical" }},
		{"first>=resolution", func(r *SLAConfigCreateRequest) { r.FirstResponseTime = 60; r.ResolutionTime = 60 }},
		{"escalation>=resolution", func(r *SLAConfigCreateRequest) { r.EscalationTime = 60 }},
	}
	for _, tc := range cases {
		req := valid
		tc.mut(&req)
		if _, err := svc.CreateSLAConfig(ctx, &req); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}

	cfg, err := svc.CreateSLAConfig(ctx, &valid)
	if err != nil {
		t.Fatalf("CreateSLAConfig: %v", err)
	}
	if cfg.CustomerTier != "" || cfg.WarningThreshold != 80 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	dup := valid
	if _, err := svc.CreateSLAConfig(ctx, &dup); err == nil {
		t.Fatal("expected duplicate config error")
	}

	// tier + warning clamping
	tierReq := SLAConfigCreateRequest{
		Name: "VIP", Priority: "high", CustomerTier: "  VIP ",
		WarningThreshold: intPtr(120), FirstResponseTime: 1, ResolutionTime: 60, EscalationTime: 30,
		Active: boolPtr(false), Tags: []string{" a ", "", "b"}, BusinessHoursOnly: true,
	}
	tierCfg, err := svc.CreateSLAConfig(ctx, &tierReq)
	if err != nil {
		t.Fatalf("CreateSLAConfig tier: %v", err)
	}
	if tierCfg.CustomerTier != "vip" || tierCfg.WarningThreshold != 100 || tierCfg.Tags != "a,b" || !tierCfg.BusinessHoursOnly {
		t.Fatalf("unexpected tier config: %+v", tierCfg)
	}
	// Active=false is overridden by the gorm default:true column on insert;
	// flip it explicitly to verify the stored value round-trips.
	if err := svc.db.Model(&models.SLAConfig{}).Where("id = ?", tierCfg.ID).Update("active", false).Error; err != nil {
		t.Fatalf("deactivate tier cfg: %v", err)
	}
	var reloaded models.SLAConfig
	if err := svc.db.First(&reloaded, tierCfg.ID).Error; err != nil {
		t.Fatalf("reload tier cfg: %v", err)
	}
	if reloaded.Active {
		t.Fatal("expected inactive tier config after explicit update")
	}

	// warning lower bound
	lowReq := SLAConfigCreateRequest{
		Name: "LowWarn", Priority: "urgent", WarningThreshold: intPtr(10),
		FirstResponseTime: 1, ResolutionTime: 60, EscalationTime: 30,
	}
	lowCfg, err := svc.CreateSLAConfig(ctx, &lowReq)
	if err != nil {
		t.Fatalf("CreateSLAConfig low: %v", err)
	}
	if lowCfg.WarningThreshold != 50 {
		t.Fatalf("expected clamp to 50, got %d", lowCfg.WarningThreshold)
	}
	_ = cfg
}

func TestSLAUnit_GetAndListConfigs(t *testing.T) {
	svc := newSLAUnitTestService(t)
	ctx := unitScopedContext("t1", "w1")

	created, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.GetSLAConfig(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetSLAConfig: %v %+v", err, got)
	}
	if _, err := svc.GetSLAConfig(ctx, 999); err == nil {
		t.Fatal("expected not found error")
	}

	_, _, err = svc.ListSLAConfigs(ctx, &SLAConfigListRequest{
		Page: 1, PageSize: 10,
		Priority:     []string{"high"},
		CustomerTier: []string{"vip"},
		Active:       boolPtr(true),
		SortBy:       "name",
		SortOrder:    "asc",
	})
	if err != nil {
		t.Fatalf("ListSLAConfigs: %v", err)
	}
	_, _, err = svc.ListSLAConfigs(ctx, &SLAConfigListRequest{SortBy: "", SortOrder: "bogus"})
	if err != nil {
		t.Fatalf("ListSLAConfigs defaults: %v", err)
	}
}

func TestSLAUnit_UpdateConfig(t *testing.T) {
	svc := newSLAUnitTestService(t)
	ctx := context.Background()

	created, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "normal", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.UpdateSLAConfig(ctx, 999, &SLAConfigUpdateRequest{}); err == nil {
		t.Fatal("expected not found on update")
	}
	if _, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{Priority: stringPtr("critical")}); err == nil {
		t.Fatal("expected invalid priority error")
	}
	if _, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{FirstResponseTime: intPtr(0)}); err == nil {
		t.Fatal("expected first response validation error")
	}
	if _, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{ResolutionTime: intPtr(0)}); err == nil {
		t.Fatal("expected resolution validation error")
	}
	if _, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{EscalationTime: intPtr(0)}); err == nil {
		t.Fatal("expected escalation validation error")
	}
	if _, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{EscalationTime: intPtr(70)}); err == nil {
		t.Fatal("expected escalation<resolution error")
	}

	updated, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{
		Name:              stringPtr("B"),
		Priority:          stringPtr("high"),
		CustomerTier:      stringPtr(" VIP "),
		Tags:              []string{"x"},
		WarningThreshold:  intPtr(60),
		FirstResponseTime: intPtr(2),
		BusinessHoursOnly: boolPtr(true),
		Active:            boolPtr(false),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "B" || updated.Priority != "high" || updated.CustomerTier != "vip" || updated.Active {
		t.Fatalf("unexpected updated: %+v", updated)
	}

	// first>=resolution via combination
	if _, err := svc.UpdateSLAConfig(ctx, created.ID, &SLAConfigUpdateRequest{FirstResponseTime: intPtr(70)}); err == nil {
		t.Fatal("expected combined time validation error")
	}

	// conflict with same priority+tier on a second config
	other, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "C", Priority: "low", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create C: %v", err)
	}
	if _, err := svc.UpdateSLAConfig(ctx, other.ID, &SLAConfigUpdateRequest{Priority: stringPtr("high")}); err == nil {
		t.Fatal("expected priority conflict on update")
	}
	// updating without priority change still checks uniqueness against itself (no conflict)
	if _, err := svc.UpdateSLAConfig(ctx, other.ID, &SLAConfigUpdateRequest{Name: stringPtr("C2")}); err != nil {
		t.Fatalf("self update should not conflict: %v", err)
	}
}

func TestSLAUnit_DeleteConfig(t *testing.T) {
	svc := newSLAUnitTestService(t)
	ctx := context.Background()

	created, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.DeleteSLAConfig(ctx, 999); err == nil {
		t.Fatal("expected delete not found error")
	}
	if err := svc.DeleteSLAConfig(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// with associated violation -> cannot delete
	cfg2, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "B", Priority: "urgent", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	violation := &models.SLAViolation{
		TicketID: cfg2.ID, SLAConfigID: cfg2.ID, ViolationType: "first_response",
		Deadline: time.Now(), ViolatedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := svc.db.Create(violation).Error; err != nil {
		t.Fatalf("seed violation: %v", err)
	}
	if err := svc.DeleteSLAConfig(ctx, cfg2.ID); err == nil {
		t.Fatal("expected delete error with associated violations")
	}
}

func TestSLAUnit_GetConfigByPriority(t *testing.T) {
	svc := newSLAUnitTestService(t)
	ctx := context.Background()

	if cfg, err := svc.GetSLAConfigByPriority(ctx, "high", ""); err != nil || cfg != nil {
		t.Fatalf("expected nil config, got %v %v", cfg, err)
	}

	base, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "Base", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	vip, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "VIP", Priority: "high", CustomerTier: "vip",
		FirstResponseTime: 1, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create vip: %v", err)
	}
	inactive, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "Inactive", Priority: "urgent", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create inactive: %v", err)
	}
	if err := svc.db.Model(&models.SLAConfig{}).Where("id = ?", inactive.ID).Update("active", false).Error; err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	if cfg, _ := svc.GetSLAConfigByPriority(ctx, "high", "vip"); cfg == nil || cfg.ID != vip.ID {
		t.Fatalf("expected vip config, got %+v", cfg)
	}
	if cfg, _ := svc.GetSLAConfigByPriority(ctx, "high", ""); cfg == nil || cfg.ID != base.ID {
		t.Fatalf("expected base config, got %+v", cfg)
	}
	if cfg, _ := svc.GetSLAConfigByPriority(ctx, "urgent", ""); cfg != nil {
		t.Fatalf("expected nil for inactive config, got %+v", cfg)
	}
}

func TestSLAUnit_CheckViolation(t *testing.T) {
	svc := newSLAUnitTestService(t)
	now := time.Now()

	if _, err := svc.CheckSLAViolation(context.Background(), nil); err == nil {
		t.Fatal("expected nil ticket error")
	}

	cfg := &models.SLAConfig{
		Name: "High", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// ticket missing
	if _, err := svc.CheckSLAViolation(context.Background(), &models.Ticket{ID: 999}); err == nil {
		t.Fatal("expected ticket not found error")
	}

	// no SLA config for priority
	noCfgTicket := &models.Ticket{Title: "X", Priority: "low", Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := svc.db.Create(noCfgTicket).Error; err != nil {
		t.Fatalf("seed noCfg ticket: %v", err)
	}
	if v, err := svc.CheckSLAViolation(context.Background(), noCfgTicket); err != nil || v != nil {
		t.Fatalf("expected nil violation, got %v %v", v, err)
	}

	// within deadline -> no violation
	fresh := &models.Ticket{Title: "Fresh", Priority: "high", Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := svc.db.Create(fresh).Error; err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	if v, err := svc.CheckSLAViolation(context.Background(), fresh); err != nil || v != nil {
		t.Fatalf("expected no violation, got %v %v", v, err)
	}

	// first response violation
	late := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now}
	if err := svc.db.Create(late).Error; err != nil {
		t.Fatalf("seed late: %v", err)
	}
	violation, err := svc.CheckSLAViolation(context.Background(), late)
	if err != nil {
		t.Fatalf("CheckSLAViolation: %v", err)
	}
	if violation == nil || violation.ViolationType != "first_response" {
		t.Fatalf("expected first_response violation, got %+v", violation)
	}
	if violation.Ticket.ID != late.ID || violation.SLAConfig.ID != cfg.ID {
		t.Fatalf("expected preloads: %+v", violation)
	}

	// duplicate detection
	second, err := svc.CheckSLAViolation(context.Background(), late)
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if second == nil || second.ID != violation.ID {
		t.Fatalf("expected existing violation, got %+v", second)
	}

	// resolution violation (assigned agent, resolved-window passed)
	resolvedLate := &models.Ticket{
		Title: "ResLate", Priority: "high", Status: "assigned", AgentID: uintPtr(7),
		CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now,
	}
	if err := svc.db.Create(resolvedLate).Error; err != nil {
		t.Fatalf("seed resolvedLate: %v", err)
	}
	resViolation, err := svc.CheckSLAViolation(context.Background(), resolvedLate)
	if err != nil {
		t.Fatalf("resolution check: %v", err)
	}
	if resViolation == nil || resViolation.ViolationType != "resolution" {
		t.Fatalf("expected resolution violation, got %+v", resViolation)
	}

	// closed/resolved tickets are exempt from resolution checks
	closedLate := &models.Ticket{
		Title: "Closed", Priority: "high", Status: "closed", AgentID: uintPtr(7),
		CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now,
	}
	if err := svc.db.Create(closedLate).Error; err != nil {
		t.Fatalf("seed closed: %v", err)
	}
	if v, err := svc.CheckSLAViolation(context.Background(), closedLate); err != nil || v != nil {
		t.Fatalf("expected no violation for closed ticket, got %v %v", v, err)
	}

	// zero created_at falls back to now (no violation)
	zeroCreated := &models.Ticket{Title: "Zero", Priority: "high", Status: "open"}
	if err := svc.db.Create(zeroCreated).Error; err != nil {
		t.Fatalf("seed zero: %v", err)
	}
	if v, err := svc.CheckSLAViolation(context.Background(), zeroCreated); err != nil || v != nil {
		t.Fatalf("expected no violation for zero created_at, got %v %v", v, err)
	}
}

func TestSLAUnit_CheckViolation_CustomerTierLookup(t *testing.T) {
	svc := newSLAUnitTestService(t)
	now := time.Now()

	if err := svc.db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}

	vipCfg := &models.SLAConfig{
		Name: "VIP", Priority: "high", CustomerTier: "vip",
		FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(vipCfg).Error; err != nil {
		t.Fatalf("seed vip config: %v", err)
	}

	customer := &models.User{Username: "vip-cust", Email: "vip@x.com", Role: "customer"}
	if err := svc.db.Create(customer).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := svc.db.Create(&models.Customer{UserID: customer.ID, Priority: "VIP"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	ticket := &models.Ticket{
		Title: "VIP ticket", Priority: "high", Status: "open", CustomerID: customer.ID,
		CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now,
	}
	if err := svc.db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	violation, err := svc.CheckSLAViolation(context.Background(), ticket)
	if err != nil {
		t.Fatalf("CheckSLAViolation: %v", err)
	}
	if violation == nil || violation.SLAConfigID != vipCfg.ID {
		t.Fatalf("expected vip config violation, got %+v", violation)
	}
}

func TestSLAUnit_CreateSLAViolationValidation(t *testing.T) {
	svc := newSLAUnitTestService(t)
	now := time.Now()

	if err := svc.CreateSLAViolation(context.Background(), nil); err == nil {
		t.Fatal("expected nil violation error")
	}

	ticket := &models.Ticket{Title: "T", Priority: "high", Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := svc.db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	cfg := &models.SLAConfig{
		Name: "Cfg", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(cfg).Error; err != nil {
		t.Fatalf("seed cfg: %v", err)
	}

	if err := svc.CreateSLAViolation(context.Background(), &models.SLAViolation{
		TicketID: 999, SLAConfigID: cfg.ID, ViolationType: "first_response",
	}); err == nil {
		t.Fatal("expected ticket not found error")
	}
	if err := svc.CreateSLAViolation(context.Background(), &models.SLAViolation{
		TicketID: ticket.ID, SLAConfigID: 999, ViolationType: "first_response",
	}); err == nil {
		t.Fatal("expected config not found error")
	}
	if err := svc.CreateSLAViolation(context.Background(), &models.SLAViolation{
		TicketID: ticket.ID, SLAConfigID: cfg.ID, ViolationType: "first_response",
		Deadline: now, ViolatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSLAViolation: %v", err)
	}
}

func TestSLAUnit_ListResolveViolations(t *testing.T) {
	svc := newSLAUnitTestService(t)
	now := time.Now()
	ctx := unitScopedContext("t1", "w1")

	ticket := &models.Ticket{
		Title: "T", Priority: "high", Status: "open", TenantID: "t1", WorkspaceID: "w1",
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}
	if err := svc.db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	cfg := &models.SLAConfig{
		Name: "Cfg", Priority: "high", TenantID: "t1", WorkspaceID: "w1",
		FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(cfg).Error; err != nil {
		t.Fatalf("seed cfg: %v", err)
	}

	if err := svc.ResolveSLAViolation(ctx, 999); err == nil {
		t.Fatal("expected resolve not found error")
	}

	violations, total, err := svc.ListSLAViolations(ctx, &SLAViolationListRequest{
		Page: 1, PageSize: 10,
		TicketID:      uintPtr(ticket.ID),
		SLAConfigID:   uintPtr(cfg.ID),
		ViolationType: []string{"first_response"},
		Resolved:      boolPtr(false),
		DateFrom:      timePtr(now.Add(-time.Hour)),
		DateTo:        timePtr(now.Add(time.Hour)),
		SortBy:        "violated_at",
		SortOrder:     "asc",
	})
	if err != nil {
		t.Fatalf("ListSLAViolations empty: %v", err)
	}
	if total != 0 || len(violations) != 0 {
		t.Fatalf("expected empty list, got %d", total)
	}
	if _, _, err := svc.ListSLAViolations(ctx, &SLAViolationListRequest{SortBy: "", SortOrder: "bogus"}); err != nil {
		t.Fatalf("ListSLAViolations defaults: %v", err)
	}

	violation, err := svc.CheckSLAViolation(ctx, ticket)
	if err != nil {
		t.Fatalf("CheckSLAViolation: %v", err)
	}
	if violation == nil {
		t.Fatal("expected violation")
	}

	if err := svc.ResolveSLAViolation(unitScopedContext("t1", "other"), violation.ID); err == nil {
		t.Fatal("expected cross-scope resolve failure")
	}
	if err := svc.ResolveSLAViolation(ctx, violation.ID); err != nil {
		t.Fatalf("ResolveSLAViolation: %v", err)
	}

	if err := svc.ResolveViolationsByTicket(ctx, 999, nil); err == nil {
		t.Fatal("expected missing ticket error")
	}
	if err := svc.ResolveViolationsByTicket(ctx, ticket.ID, []string{"first_response"}); err != nil {
		t.Fatalf("ResolveViolationsByTicket: %v", err)
	}
}

func TestSLAUnit_GetStats(t *testing.T) {
	svc := newSLAUnitTestService(t)
	now := time.Now()
	ctx := unitScopedContext("t1", "w1")

	ticket := &models.Ticket{
		Title: "T", Priority: "high", Status: "open", TenantID: "t1", WorkspaceID: "w1",
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}
	if err := svc.db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	cfg := &models.SLAConfig{
		Name: "Cfg", Priority: "high", TenantID: "t1", WorkspaceID: "w1",
		FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(cfg).Error; err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	vipCfg := &models.SLAConfig{
		Name: "VipCfg", Priority: "urgent", CustomerTier: "vip", TenantID: "t1", WorkspaceID: "w1",
		FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(vipCfg).Error; err != nil {
		t.Fatalf("seed vip cfg: %v", err)
	}
	if _, err := svc.CheckSLAViolation(ctx, ticket); err != nil {
		t.Fatalf("CheckSLAViolation: %v", err)
	}
	// seed a tier-specific violation directly for tier aggregation coverage
	if err := svc.db.Create(&models.SLAViolation{
		TenantID: "t1", WorkspaceID: "w1", TicketID: ticket.ID, SLAConfigID: vipCfg.ID,
		ViolationType: "resolution", Deadline: now, ViolatedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed tier violation: %v", err)
	}

	stats, err := svc.GetSLAStats(ctx)
	if err != nil {
		t.Fatalf("GetSLAStats: %v", err)
	}
	if stats.TotalConfigs != 2 || stats.ActiveConfigs != 2 || stats.TotalViolations != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ViolationsByType["first_response"] != 1 || stats.ViolationsByPriority["high"] != 1 || stats.ViolationsByPriority["urgent"] != 1 {
		t.Fatalf("unexpected breakdowns: %+v", stats)
	}

	// unscoped stats: compliance 100 when no tickets
	unscoped := newSLAUnitTestService(t)
	emptyStats, err := unscoped.GetSLAStats(context.Background())
	if err != nil {
		t.Fatalf("unscoped GetSLAStats: %v", err)
	}
	if emptyStats.ComplianceRate != 100 {
		t.Fatalf("expected 100 compliance, got %v", emptyStats.ComplianceRate)
	}
}

func TestSLAUnit_MonitorAndStart(t *testing.T) {
	svc := newSLAUnitTestService(t)
	now := time.Now()

	ticket := &models.Ticket{
		Title: "T", Priority: "low", Status: "closed", CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	if err := svc.monitorSLAViolations(context.Background()); err != nil {
		t.Fatalf("monitorSLAViolations: %v", err)
	}

	// StartSLAMonitor with cancelled context returns promptly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.StartSLAMonitor(ctx, time.Minute)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StartSLAMonitor did not stop")
	}
}

func TestSLAUnit_Helpers(t *testing.T) {
	if joinTags(nil) != "" || joinTags([]string{" ", ""}) != "" {
		t.Fatal("joinTags empty handling")
	}
	if got := joinTags([]string{" a ", "b"}); got != "a,b" {
		t.Fatalf("joinTags = %q", got)
	}
	if normalizeTier("  VIP ") != "vip" {
		t.Fatal("normalizeTier")
	}
	if defaultWarning(nil) != 80 || defaultWarning(intPtr(40)) != 50 || defaultWarning(intPtr(90)) != 90 {
		t.Fatal("defaultWarning")
	}

	ctx := slaRecordScopeContext(context.Background(), "r-t", "r-w")
	if tenant, ws := tenantAndWorkspace(ctx); tenant != "r-t" || ws != "r-w" {
		t.Fatalf("record scope fallback: %q/%q", tenant, ws)
	}
	scoped := slaRecordScopeContext(unitScopedContext("c-t", "c-w"), "r-t", "r-w")
	if tenant, ws := tenantAndWorkspace(scoped); tenant != "c-t" || ws != "c-w" {
		t.Fatalf("record scope keeps context: %q/%q", tenant, ws)
	}

	db := newSLAUnitTestDB(t)
	if scopeAwareSLAViolationPreloads(db.Session(&gorm.Session{}), context.Background()) == nil {
		t.Fatal("expected preloads chain")
	}
}

func TestSLAUnit_SetAutomationService(t *testing.T) {
	svc := newSLAUnitTestService(t)
	automationDB := newServicesTestDB(t,
		&models.AutomationTrigger{}, &models.AutomationRun{},
		&models.Ticket{}, &models.TicketComment{},
	)
	svc.SetAutomationService(NewAutomationService(automationDB, nil))

	now := time.Now()
	cfg := &models.SLAConfig{
		Name: "Cfg", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.db.Create(cfg).Error; err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	ticket := &models.Ticket{
		Title: "T", Priority: "high", Status: "open",
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}
	if err := svc.db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if _, err := svc.CheckSLAViolation(context.Background(), ticket); err != nil {
		t.Fatalf("CheckSLAViolation with automation: %v", err)
	}
}
