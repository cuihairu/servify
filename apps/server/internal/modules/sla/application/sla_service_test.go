//go:build integration
// +build integration

package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"servify/apps/server/internal/modules/platformauth"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// newSLAIntegrationDB 与 unit 侧 newSLATestDB 刻意区分名：
// integration 标签构建会把两文件一起编译，避免重声明冲突。
func newSLAIntegrationDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Ticket{}, &models.SLAConfig{}, &models.SLAViolation{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

// scopedContext 复刻自 services 集成测试原件（platformauth 注入租户/工作区）。
func scopedContext(tenantID, workspaceID string) context.Context {
	ctx := context.Background()
	ctx = platformauth.WithTenantID(ctx, tenantID)
	return platformauth.WithWorkspaceID(ctx, workspaceID)
}

func TestSLAService_CheckViolation_FirstResponse(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())

	now := time.Now()
	cfg := &models.SLAConfig{
		Name:              "High Priority",
		Priority:          "high",
		FirstResponseTime: 5,
		ResolutionTime:    60,
		EscalationTime:    30,
		Active:            true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(cfg).Error; err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	ticket := &models.Ticket{
		ID:        1,
		Title:     "Test",
		Priority:  "high",
		Status:    "open",
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now.Add(-10 * time.Minute),
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("failed to insert ticket: %v", err)
	}

	violation, err := svc.CheckSLAViolation(context.Background(), ticket)
	if err != nil {
		t.Fatalf("CheckSLAViolation failed: %v", err)
	}
	if violation == nil || violation.ViolationType != "first_response" {
		t.Fatalf("expected first_response violation, got %+v", violation)
	}
	if violation.Ticket.ID != ticket.ID {
		t.Fatalf("expected preloaded ticket in violation, got %+v", violation.Ticket)
	}
	if violation.SLAConfig.ID != cfg.ID {
		t.Fatalf("expected preloaded sla config in violation, got %+v", violation.SLAConfig)
	}

	// running again should reuse existing violation
	violation2, err := svc.CheckSLAViolation(context.Background(), ticket)
	if err != nil {
		t.Fatalf("CheckSLAViolation second call failed: %v", err)
	}
	if violation2 == nil || violation2.ID != violation.ID {
		t.Fatalf("expected duplicate detection; got %#v", violation2)
	}
	if violation2.Ticket.ID != ticket.ID || violation2.SLAConfig.ID != cfg.ID {
		t.Fatalf("expected preloaded associations on duplicate violation, got %+v", violation2)
	}
}

func TestSLAService_ListConfigsScopedByWorkspace(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())

	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")

	if _, err := svc.CreateSLAConfig(ctxA, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	}); err != nil {
		t.Fatalf("create A failed: %v", err)
	}
	if _, err := svc.CreateSLAConfig(ctxB, &SLAConfigCreateRequest{
		Name: "B", Priority: "normal", FirstResponseTime: 10, ResolutionTime: 120, EscalationTime: 30,
	}); err != nil {
		t.Fatalf("create B failed: %v", err)
	}

	items, total, err := svc.ListSLAConfigs(ctxA, &SLAConfigListRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected items: total=%d items=%+v", total, items)
	}
}

func TestSLAService_ResolveViolationsByTicket(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())

	now := time.Now()
	cfg := &models.SLAConfig{
		Name:              "Normal Priority",
		Priority:          "normal",
		FirstResponseTime: 10,
		ResolutionTime:    120,
		EscalationTime:    60,
		Active:            true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(cfg).Error; err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	ticket := &models.Ticket{
		ID:        2,
		Title:     "Need help",
		Priority:  "normal",
		Status:    "open",
		CreatedAt: now.Add(-3 * time.Hour),
		UpdatedAt: now.Add(-3 * time.Hour),
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("failed to insert ticket: %v", err)
	}

	// create violation manually via service
	if _, err := svc.CheckSLAViolation(context.Background(), ticket); err != nil {
		t.Fatalf("failed to create violation: %v", err)
	}

	if err := svc.ResolveViolationsByTicket(context.Background(), ticket.ID, []string{"first_response"}); err != nil {
		t.Fatalf("ResolveViolationsByTicket failed: %v", err)
	}

	var count int64
	if err := db.Model(&models.SLAViolation{}).Where("ticket_id = ? AND resolved = true", ticket.ID).Count(&count).Error; err != nil {
		t.Fatalf("failed to count resolved violations: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected resolved violations, got zero")
	}
}

func TestSLAService_ResolveViolationsByTicket_NoMatchesIsNoop(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())

	ticket := &models.Ticket{
		ID:        999,
		Title:     "No violations yet",
		Priority:  "normal",
		Status:    "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("failed to insert ticket: %v", err)
	}

	if err := svc.ResolveViolationsByTicket(context.Background(), ticket.ID, []string{"first_response"}); err != nil {
		t.Fatalf("expected no-op success, got %v", err)
	}
}

func TestSLAService_ResolveViolationsByTicket_MissingTicket(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())

	if err := svc.ResolveViolationsByTicket(context.Background(), 999, []string{"first_response"}); err == nil {
		t.Fatal("expected missing ticket to fail")
	}
}

func TestSLAService_ViolationsScopedByWorkspace(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())

	now := time.Now()
	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")

	cfgA, err := svc.CreateSLAConfig(ctxA, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create config A failed: %v", err)
	}
	cfgB, err := svc.CreateSLAConfig(ctxB, &SLAConfigCreateRequest{
		Name: "B", Priority: "urgent", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create config B failed: %v", err)
	}

	ticketA := &models.Ticket{
		ID:          21,
		Title:       "Ticket A",
		Priority:    "high",
		Status:      "open",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now.Add(-2 * time.Hour),
	}
	ticketB := &models.Ticket{
		ID:          22,
		Title:       "Ticket B",
		Priority:    "urgent",
		Status:      "open",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now.Add(-2 * time.Hour),
	}
	if err := db.Create(ticketA).Error; err != nil {
		t.Fatalf("create ticket A failed: %v", err)
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B failed: %v", err)
	}

	violationA, err := svc.CheckSLAViolation(ctxA, ticketA)
	if err != nil {
		t.Fatalf("check violation A failed: %v", err)
	}
	if violationA == nil || violationA.WorkspaceID != "workspace-a" || violationA.SLAConfigID != cfgA.ID {
		t.Fatalf("unexpected violation A: %+v", violationA)
	}
	violationB, err := svc.CheckSLAViolation(ctxB, ticketB)
	if err != nil {
		t.Fatalf("check violation B failed: %v", err)
	}
	if violationB == nil || violationB.WorkspaceID != "workspace-b" || violationB.SLAConfigID != cfgB.ID {
		t.Fatalf("unexpected violation B: %+v", violationB)
	}

	itemsA, totalA, err := svc.ListSLAViolations(ctxA, &SLAViolationListRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list violations A failed: %v", err)
	}
	if totalA != 1 || len(itemsA) != 1 || itemsA[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scoped violations: total=%d items=%+v", totalA, itemsA)
	}

	statsA, err := svc.GetSLAStats(ctxA)
	if err != nil {
		t.Fatalf("stats A failed: %v", err)
	}
	if statsA.TotalConfigs != 1 || statsA.TotalViolations != 1 || statsA.ViolationsByPriority["high"] != 1 {
		t.Fatalf("unexpected scoped stats: %+v", statsA)
	}

	if config, err := svc.GetSLAConfigByPriority(ctxA, "urgent", ""); err != nil {
		t.Fatalf("get urgent config in scope A errored: %v", err)
	} else if config != nil {
		t.Fatalf("expected urgent config to be hidden from workspace A, got %+v", config)
	}

	if err := svc.ResolveSLAViolation(ctxB, violationA.ID); err == nil {
		t.Fatal("expected cross-workspace resolve to fail")
	}
	if err := svc.DeleteSLAConfig(ctxB, cfgA.ID); err == nil {
		t.Fatal("expected cross-workspace delete config to fail")
	}

	if err := svc.ResolveSLAViolation(ctxA, violationA.ID); err != nil {
		t.Fatalf("resolve violation A failed: %v", err)
	}
	resolved := true
	resolvedItems, resolvedTotal, err := svc.ListSLAViolations(ctxA, &SLAViolationListRequest{
		Page: 1, PageSize: 20, Resolved: &resolved,
	})
	if err != nil {
		t.Fatalf("list resolved violations failed: %v", err)
	}
	if resolvedTotal != 1 || len(resolvedItems) != 1 || !resolvedItems[0].Resolved {
		t.Fatalf("unexpected resolved items: total=%d items=%+v", resolvedTotal, resolvedItems)
	}
}

func TestSLAService_ListViolations_DoesNotLeakCrossScopePreloads(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()

	ticketB := &models.Ticket{
		ID:          101,
		Title:       "Ticket B",
		Priority:    "urgent",
		Status:      "open",
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B failed: %v", err)
	}

	configB := &models.SLAConfig{
		ID:                202,
		Name:              "Config B",
		Priority:          "urgent",
		TenantID:          "tenant-b",
		WorkspaceID:       "workspace-b",
		FirstResponseTime: 5,
		ResolutionTime:    60,
		EscalationTime:    30,
		Active:            true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(configB).Error; err != nil {
		t.Fatalf("create config B failed: %v", err)
	}

	violationA := &models.SLAViolation{
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-a",
		TicketID:      ticketB.ID,
		SLAConfigID:   configB.ID,
		ViolationType: "first_response",
		Deadline:      now.Add(-10 * time.Minute),
		ViolatedAt:    now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(violationA).Error; err != nil {
		t.Fatalf("create violation A failed: %v", err)
	}

	items, total, err := svc.ListSLAViolations(scopedContext("tenant-a", "workspace-a"), &SLAViolationListRequest{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListSLAViolations failed: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected scoped violations: total=%d items=%+v", total, items)
	}
	if items[0].Ticket.ID != 0 {
		t.Fatalf("expected scoped ticket preload to stay empty, got %+v", items[0].Ticket)
	}
	if items[0].SLAConfig.ID != 0 {
		t.Fatalf("expected scoped config preload to stay empty, got %+v", items[0].SLAConfig)
	}
}

func TestSLAService_CheckViolation_RejectsCrossScopeTicket(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()

	cfgB := &models.SLAConfig{
		Name:              "Urgent B",
		Priority:          "urgent",
		TenantID:          "tenant-a",
		WorkspaceID:       "workspace-b",
		FirstResponseTime: 5,
		ResolutionTime:    60,
		EscalationTime:    30,
		Active:            true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(cfgB).Error; err != nil {
		t.Fatalf("create config B failed: %v", err)
	}

	ticketB := &models.Ticket{
		ID:          303,
		Title:       "Ticket B",
		Priority:    "urgent",
		Status:      "open",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now.Add(-2 * time.Hour),
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B failed: %v", err)
	}

	if _, err := svc.CheckSLAViolation(scopedContext("tenant-a", "workspace-a"), ticketB); err == nil {
		t.Fatal("expected cross-scope ticket violation check to fail")
	}
}

func TestSLAService_CreateSLAViolation_RejectsCrossScopeReferences(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()

	ticketB := &models.Ticket{
		ID:          401,
		Title:       "Ticket B",
		Priority:    "urgent",
		Status:      "open",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B failed: %v", err)
	}

	configB := &models.SLAConfig{
		ID:                402,
		Name:              "Config B",
		Priority:          "urgent",
		TenantID:          "tenant-a",
		WorkspaceID:       "workspace-b",
		FirstResponseTime: 5,
		ResolutionTime:    60,
		EscalationTime:    30,
		Active:            true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(configB).Error; err != nil {
		t.Fatalf("create config B failed: %v", err)
	}

	err := svc.CreateSLAViolation(scopedContext("tenant-a", "workspace-a"), &models.SLAViolation{
		TicketID:      ticketB.ID,
		SLAConfigID:   configB.ID,
		ViolationType: "first_response",
		Deadline:      now.Add(-10 * time.Minute),
		ViolatedAt:    now,
	})
	if err == nil {
		t.Fatal("expected cross-scope violation creation to fail")
	}
}

func TestSLAService_GetStats_DoesNotLeakCrossScopeConfigJoins(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()

	violationA := &models.SLAViolation{
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-a",
		TicketID:      501,
		SLAConfigID:   601,
		ViolationType: "first_response",
		Deadline:      now.Add(-10 * time.Minute),
		ViolatedAt:    now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(violationA).Error; err != nil {
		t.Fatalf("create violation A failed: %v", err)
	}

	configB := &models.SLAConfig{
		ID:                601,
		Name:              "Config B",
		Priority:          "urgent",
		CustomerTier:      "vip",
		TenantID:          "tenant-a",
		WorkspaceID:       "workspace-b",
		FirstResponseTime: 5,
		ResolutionTime:    60,
		EscalationTime:    30,
		Active:            true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(configB).Error; err != nil {
		t.Fatalf("create config B failed: %v", err)
	}

	stats, err := svc.GetSLAStats(scopedContext("tenant-a", "workspace-a"))
	if err != nil {
		t.Fatalf("GetSLAStats failed: %v", err)
	}
	if stats.ViolationsByPriority["urgent"] != 0 {
		t.Fatalf("expected cross-scope priority join to be excluded, got %+v", stats.ViolationsByPriority)
	}
	if stats.ViolationsByTier["vip"] != 0 {
		t.Fatalf("expected cross-scope tier join to be excluded, got %+v", stats.ViolationsByTier)
	}
}

func TestSLAService_GetStats_DoesNotLeakCrossScopeTrendData(t *testing.T) {
	db := newSLAIntegrationDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()

	if err := db.Create(&[]models.Ticket{
		{
			ID:          701,
			Title:       "ticket-a",
			Status:      "open",
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          702,
			Title:       "ticket-b",
			Status:      "open",
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-b",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}).Error; err != nil {
		t.Fatalf("create tickets failed: %v", err)
	}

	if err := db.Create(&[]models.SLAViolation{
		{
			ID:            801,
			TenantID:      "tenant-a",
			WorkspaceID:   "workspace-a",
			TicketID:      701,
			SLAConfigID:   1,
			ViolationType: "first_response",
			Deadline:      now.Add(-time.Hour),
			ViolatedAt:    now,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            802,
			TenantID:      "tenant-a",
			WorkspaceID:   "workspace-b",
			TicketID:      702,
			SLAConfigID:   2,
			ViolationType: "resolution",
			Deadline:      now.Add(-time.Hour),
			ViolatedAt:    now,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}).Error; err != nil {
		t.Fatalf("create violations failed: %v", err)
	}

	stats, err := svc.GetSLAStats(scopedContext("tenant-a", "workspace-a"))
	if err != nil {
		t.Fatalf("GetSLAStats failed: %v", err)
	}
	if len(stats.TrendData) == 0 {
		t.Fatalf("expected trend data, got %+v", stats)
	}
	foundScopedDay := false
	for _, item := range stats.TrendData {
		if item.TotalTickets > 1 || item.Violations > 1 {
			t.Fatalf("expected scoped trend to exclude workspace-b data, got %+v", stats.TrendData)
		}
		if item.TotalTickets == 1 && item.Violations == 1 && item.ComplianceRate == 0 {
			foundScopedDay = true
		}
	}
	if !foundScopedDay {
		t.Fatalf("expected one scoped trend row with 1 ticket and 1 violation, got %+v", stats.TrendData)
	}
}
