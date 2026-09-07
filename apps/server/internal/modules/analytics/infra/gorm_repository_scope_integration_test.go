package infra

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

func newAnalyticsScopeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:analytics_scope_" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Customer{},
		&models.Agent{},
		&models.Ticket{},
		&models.Session{},
		&models.Message{},
		&models.CustomerSatisfaction{},
		&models.DailyStats{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	// Close all connections when the test finishes so the shared-cache
	// in-memory database is destroyed and re-runs (-count>1) start clean.
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func scopedAnalyticsContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestGormRepositoryScopedDashboardIgnoresGlobalDailyStats(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	today := now.Truncate(24 * time.Hour)
	ctxA := scopedAnalyticsContext("tenant-a", "workspace-a")

	customerUser := models.User{ID: 11, Username: "cust-a", Email: "cust-a@example.com", Role: "customer"}
	if err := db.Create(&customerUser).Error; err != nil {
		t.Fatalf("create customer user: %v", err)
	}
	customer := models.Customer{UserID: customerUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a", Priority: "normal"}
	if err := db.Create(&customer).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	agentUser := models.User{ID: 12, Username: "agent-a", Email: "agent-a@example.com", Role: "agent"}
	if err := db.Create(&agentUser).Error; err != nil {
		t.Fatalf("create agent user: %v", err)
	}
	agent := models.Agent{UserID: agentUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "online", AvgResponseTime: 42}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	ticket := models.Ticket{
		ID:          101,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		Title:       "Scoped ticket",
		Status:      "open",
		CustomerID:  customerUser.ID,
		CreatedAt:   today.Add(2 * time.Hour),
		UpdatedAt:   today.Add(2 * time.Hour),
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	session := models.Session{
		ID:          "sess-a",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		UserID:      customerUser.ID,
		Status:      "active",
		CreatedAt:   today.Add(2 * time.Hour),
		StartedAt:   today.Add(2 * time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&models.Message{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		SessionID:   session.ID,
		Content:     "hello",
		Type:        "text",
		Sender:      "customer",
		CreatedAt:   today.Add(2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	// Global/system DailyStats contains unrelated high counters and must not leak
	// into scoped dashboard results.
	if err := db.Create(&models.DailyStats{
		Date:              today,
		AIUsageCount:      999,
		WeKnoraUsageCount: 555,
	}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	stats, err := repo.GetDashboardStats(ctxA)
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats.TotalCustomers != 1 || stats.TotalAgents != 1 || stats.TotalTickets != 1 || stats.TotalSessions != 1 {
		t.Fatalf("unexpected scoped counts: %+v", stats)
	}
	if stats.AIUsageToday != 0 || stats.WeKnoraUsageToday != 0 {
		t.Fatalf("expected scoped dashboard to ignore global daily stats, got %+v", stats)
	}
}

func TestGormRepositoryScopedTimeRangeIgnoresGlobalDailyStats(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	day := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	ctxA := scopedAnalyticsContext("tenant-a", "workspace-a")

	if err := db.Create(&models.CustomerSatisfaction{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		TicketID:    201,
		CustomerID:  21,
		Rating:      4,
		Category:    "overall",
		CreatedAt:   day.Add(4 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create scoped satisfaction: %v", err)
	}
	if err := db.Create(&models.CustomerSatisfaction{
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		TicketID:    202,
		CustomerID:  22,
		Rating:      1,
		Category:    "overall",
		CreatedAt:   day.Add(5 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create unscoped satisfaction: %v", err)
	}
	if err := db.Create(&models.DailyStats{
		Date:                 day,
		AvgResponseTime:      777,
		CustomerSatisfaction: 1.5,
	}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	items, err := repo.GetTimeRangeStats(ctxA, day, day)
	if err != nil {
		t.Fatalf("GetTimeRangeStats: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(items))
	}
	if items[0].CustomerSatisfaction != 4 {
		t.Fatalf("expected scoped satisfaction avg 4, got %+v", items[0])
	}
	if items[0].AvgResponseTime != 0 {
		t.Fatalf("expected scoped time-range to avoid global avg response time, got %+v", items[0])
	}
}

func TestGormRepositoryScopedAgentPerformanceStaysScoped(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctxA := scopedAnalyticsContext("tenant-a", "workspace-a")

	agentAUser := models.User{ID: 31, Username: "agent-a", Email: "agent-a@example.com", Name: "Agent A", Role: "agent"}
	agentBUser := models.User{ID: 32, Username: "agent-b", Email: "agent-b@example.com", Name: "Agent B", Role: "agent"}
	if err := db.Create(&[]models.User{agentAUser, agentBUser}).Error; err != nil {
		t.Fatalf("create agent users: %v", err)
	}
	agentA := models.Agent{UserID: agentAUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a", Department: "support", AvgResponseTime: 15, Rating: 4.8}
	agentB := models.Agent{UserID: agentBUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-b", Department: "support", AvgResponseTime: 99, Rating: 1.2}
	if err := db.Create(&[]models.Agent{agentA, agentB}).Error; err != nil {
		t.Fatalf("create agents: %v", err)
	}

	tickets := []models.Ticket{
		{
			ID:          301,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AgentID:     &agentAUser.ID,
			Title:       "scoped-resolved",
			Status:      "resolved",
			CreatedAt:   start.Add(2 * time.Hour),
			ResolvedAt:  timePtr(start.Add(3 * time.Hour)),
			UpdatedAt:   start.Add(3 * time.Hour),
		},
		{
			ID:          302,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AgentID:     &agentAUser.ID,
			Title:       "scoped-open",
			Status:      "open",
			CreatedAt:   start.Add(4 * time.Hour),
			UpdatedAt:   start.Add(4 * time.Hour),
		},
		{
			ID:          303,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-b",
			AgentID:     &agentBUser.ID,
			Title:       "cross-workspace",
			Status:      "resolved",
			CreatedAt:   start.Add(5 * time.Hour),
			ResolvedAt:  timePtr(start.Add(9 * time.Hour)),
			UpdatedAt:   start.Add(9 * time.Hour),
		},
	}
	if err := db.Create(&tickets).Error; err != nil {
		t.Fatalf("create tickets: %v", err)
	}

	stats, err := repo.GetAgentPerformanceStats(ctxA, start, end, 10)
	if err != nil {
		t.Fatalf("GetAgentPerformanceStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 scoped agent row, got %+v", stats)
	}
	if stats[0].AgentID != agentAUser.ID || stats[0].AgentName != "Agent A" {
		t.Fatalf("unexpected agent row: %+v", stats[0])
	}
	if stats[0].TotalTickets != 2 || stats[0].ResolvedTickets != 1 {
		t.Fatalf("unexpected scoped ticket aggregates: %+v", stats[0])
	}
}

func TestGormRepositoryScopedCategoryPriorityAndSourceStatsStayScoped(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctxA := scopedAnalyticsContext("tenant-a", "workspace-a")

	customerAUser := models.User{ID: 41, Username: "customer-a", Email: "customer-a@example.com", Role: "customer"}
	customerBUser := models.User{ID: 42, Username: "customer-b", Email: "customer-b@example.com", Role: "customer"}
	if err := db.Create(&[]models.User{customerAUser, customerBUser}).Error; err != nil {
		t.Fatalf("create customer users: %v", err)
	}
	if err := db.Create(&[]models.Customer{
		{UserID: customerAUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a", Source: "web"},
		{UserID: customerBUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-b", Source: "referral"},
	}).Error; err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if err := db.Create(&[]models.Ticket{
		{
			ID:          401,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			CustomerID:  customerAUser.ID,
			Title:       "billing a1",
			Category:    "billing",
			Priority:    "high",
			Status:      "open",
			CreatedAt:   start.Add(1 * time.Hour),
			UpdatedAt:   start.Add(1 * time.Hour),
		},
		{
			ID:          402,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			CustomerID:  customerAUser.ID,
			Title:       "billing a2",
			Category:    "billing",
			Priority:    "low",
			Status:      "open",
			CreatedAt:   start.Add(2 * time.Hour),
			UpdatedAt:   start.Add(2 * time.Hour),
		},
		{
			ID:          403,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-b",
			CustomerID:  customerBUser.ID,
			Title:       "technical b1",
			Category:    "technical",
			Priority:    "urgent",
			Status:      "open",
			CreatedAt:   start.Add(3 * time.Hour),
			UpdatedAt:   start.Add(3 * time.Hour),
		},
	}).Error; err != nil {
		t.Fatalf("create tickets: %v", err)
	}

	categoryStats, err := repo.GetTicketCategoryStats(ctxA, start, end)
	if err != nil {
		t.Fatalf("GetTicketCategoryStats: %v", err)
	}
	if len(categoryStats) != 1 || categoryStats[0].Category != "billing" || categoryStats[0].Count != 2 {
		t.Fatalf("unexpected scoped category stats: %+v", categoryStats)
	}

	priorityStats, err := repo.GetTicketPriorityStats(ctxA, start, end)
	if err != nil {
		t.Fatalf("GetTicketPriorityStats: %v", err)
	}
	if len(priorityStats) != 2 {
		t.Fatalf("unexpected scoped priority stats: %+v", priorityStats)
	}
	for _, item := range priorityStats {
		if item.Category == "urgent" {
			t.Fatalf("unexpected cross-workspace priority stat: %+v", priorityStats)
		}
	}

	sourceStats, err := repo.GetCustomerSourceStats(ctxA)
	if err != nil {
		t.Fatalf("GetCustomerSourceStats: %v", err)
	}
	if len(sourceStats) != 1 || sourceStats[0].Category != "web" || sourceStats[0].Count != 1 {
		t.Fatalf("unexpected scoped source stats: %+v", sourceStats)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
