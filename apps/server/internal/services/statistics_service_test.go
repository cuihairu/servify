//go:build integration
// +build integration

package services

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"servify/apps/server/internal/models"
)

func newStatisticsServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:statistics_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Ticket{},
		&models.Session{},
		&models.Message{},
		&models.Customer{},
		&models.CustomerSatisfaction{},
		&models.DailyStats{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestNewStatisticsService(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()

	svc := NewStatisticsService(db, logger)

	if svc == nil {
		t.Fatal("expected service, got nil")
	}
	if svc.db != db {
		t.Error("expected db to be set")
	}
	if svc.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestNewStatisticsService_NilLogger(t *testing.T) {
	db := newStatisticsServiceTestDB(t)

	svc := NewStatisticsService(db, nil)

	if svc == nil {
		t.Fatal("expected service, got nil")
	}
	if svc.logger == nil {
		t.Error("expected logger to be initialized")
	}
}

func TestStatisticsService_GetDashboardStats_EmptyDB(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	stats, err := svc.GetDashboardStats(context.Background())
	if err != nil {
		t.Fatalf("GetDashboardStats() error = %v", err)
	}
	if stats == nil {
		t.Fatal("expected stats, got nil")
	}
	// All counts should be 0
	if stats.TotalCustomers != 0 {
		t.Errorf("expected 0 total customers, got %d", stats.TotalCustomers)
	}
	if stats.TotalAgents != 0 {
		t.Errorf("expected 0 total agents, got %d", stats.TotalAgents)
	}
}

func TestStatisticsService_GetDashboardStats_WithData(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	// Create test users
	customer := &models.User{
		Username: "customer1",
		Email:    "customer1@test.com",
		Name:     "Customer One",
		Role:     "customer",
	}
	db.Create(customer)

	agentUser := &models.User{
		Username: "agent1",
		Email:    "agent1@test.com",
		Name:     "Agent One",
		Role:     "agent",
	}
	db.Create(agentUser)

	// Create agent
	agent := &models.Agent{
		UserID: agentUser.ID,
		Status: "online",
	}
	db.Create(agent)

	// Create ticket
	ticket := &models.Ticket{
		Title:       "Test Ticket",
		Description: "Test",
		Status:      "open",
		CustomerID:  1,
	}
	db.Create(ticket)

	// Create session
	session := &models.Session{
		Platform:  "web",
		Status:    "active",
		StartedAt: time.Now(),
	}
	db.Create(session)

	stats, err := svc.GetDashboardStats(context.Background())
	if err != nil {
		t.Fatalf("GetDashboardStats() error = %v", err)
	}
	if stats.TotalCustomers != 1 {
		t.Errorf("expected 1 total customer, got %d", stats.TotalCustomers)
	}
	if stats.TotalAgents != 1 {
		t.Errorf("expected 1 total agent, got %d", stats.TotalAgents)
	}
	if stats.TotalTickets != 1 {
		t.Errorf("expected 1 total ticket, got %d", stats.TotalTickets)
	}
	if stats.TotalSessions != 1 {
		t.Errorf("expected 1 total session, got %d", stats.TotalSessions)
	}
	if stats.OnlineAgents != 1 {
		t.Errorf("expected 1 online agent, got %d", stats.OnlineAgents)
	}
	if stats.ActiveSessions != 1 {
		t.Errorf("expected 1 active session, got %d", stats.ActiveSessions)
	}
}

func TestStatisticsService_GetTimeRangeStats_Empty(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := svc.GetTimeRangeStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Fatalf("GetTimeRangeStats() error = %v", err)
	}
	if len(stats) != 8 { // 7 days + today
		t.Errorf("expected 8 stats entries, got %d", len(stats))
	}
	// All counts should be 0
	for _, stat := range stats {
		if stat.Tickets != 0 {
			t.Errorf("expected 0 tickets for %s, got %d", stat.Date, stat.Tickets)
		}
	}
}

func TestStatisticsService_GetTimeRangeStats_SingleDay(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	// Create a ticket for today
	ticket := &models.Ticket{
		Title:       "Test Ticket",
		Description: "Test",
		Status:      "open",
	}
	db.Create(ticket)

	startDate := time.Now().Truncate(24 * time.Hour)
	endDate := startDate

	stats, err := svc.GetTimeRangeStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Fatalf("GetTimeRangeStats() error = %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 stat entry, got %d", len(stats))
	}
	if stats[0].Tickets != 1 {
		t.Errorf("expected 1 ticket, got %d", stats[0].Tickets)
	}
}

func TestStatisticsService_GetTicketCategoryStats_Empty(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := svc.GetTicketCategoryStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Fatalf("GetTicketCategoryStats() error = %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 category stats, got %d", len(stats))
	}
}

func TestStatisticsService_GetTicketCategoryStats_WithCategories(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	// Create tickets with different categories
	db.Create(&models.Ticket{Title: "T1", Category: "technical", Status: "open"})
	db.Create(&models.Ticket{Title: "T2", Category: "billing", Status: "open"})
	db.Create(&models.Ticket{Title: "T3", Category: "technical", Status: "open"})

	startDate := time.Now().AddDate(0, 0, -1)
	endDate := time.Now()

	stats, err := svc.GetTicketCategoryStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Fatalf("GetTicketCategoryStats() error = %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("expected 2 category stats, got %d", len(stats))
	}
	// Find technical category
	var technicalStat *CategoryStats
	for i := range stats {
		if stats[i].Category == "technical" {
			technicalStat = &stats[i]
			break
		}
	}
	if technicalStat == nil {
		t.Fatal("technical category not found")
	}
	if technicalStat.Count != 2 {
		t.Errorf("expected 2 technical tickets, got %d", technicalStat.Count)
	}
}

func TestStatisticsService_GetDashboardStats_AppliesScope(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)
	now := time.Now()

	db.Create(&models.User{ID: 1, Username: "customer_a", Email: "customer_a@test.com", Role: "customer"})
	db.Create(&models.User{ID: 2, Username: "customer_b", Email: "customer_b@test.com", Role: "customer"})
	db.Create(&models.Customer{UserID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Source: "web"})
	db.Create(&models.Customer{UserID: 2, TenantID: "tenant-b", WorkspaceID: "workspace-b", Source: "referral"})
	db.Create(&models.Agent{UserID: 11, TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "online"})
	db.Create(&models.Agent{UserID: 12, TenantID: "tenant-b", WorkspaceID: "workspace-b", Status: "busy"})
	db.Create(&models.Ticket{Title: "A", TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "open", CreatedAt: now, UpdatedAt: now})
	db.Create(&models.Ticket{Title: "B", TenantID: "tenant-b", WorkspaceID: "workspace-b", Status: "closed", CreatedAt: now, UpdatedAt: now})
	db.Create(&models.Session{ID: "sess-a", TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "active", StartedAt: now})
	db.Create(&models.Session{ID: "sess-b", TenantID: "tenant-b", WorkspaceID: "workspace-b", Status: "active", StartedAt: now})
	db.Create(&models.Message{SessionID: "sess-a", TenantID: "tenant-a", WorkspaceID: "workspace-a", Content: "hello", Type: "text", Sender: "user", CreatedAt: now})
	db.Create(&models.Message{SessionID: "sess-b", TenantID: "tenant-b", WorkspaceID: "workspace-b", Content: "hello", Type: "text", Sender: "user", CreatedAt: now})

	stats, err := svc.GetDashboardStats(scopedContext("tenant-a", "workspace-a"))
	if err != nil {
		t.Fatalf("GetDashboardStats() error = %v", err)
	}
	if stats.TotalCustomers != 1 || stats.TotalAgents != 1 || stats.TotalTickets != 1 || stats.TotalSessions != 1 || stats.TodayMessages != 1 {
		t.Fatalf("unexpected scoped dashboard stats: %+v", stats)
	}
}

func TestStatisticsService_GetAgentPerformanceStats_AppliesScope(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	agentAUser := models.User{ID: 21, Username: "agent_a", Email: "agent_a@test.com", Name: "Agent A", Role: "agent"}
	agentBUser := models.User{ID: 22, Username: "agent_b", Email: "agent_b@test.com", Name: "Agent B", Role: "agent"}
	if err := db.Create(&[]models.User{agentAUser, agentBUser}).Error; err != nil {
		t.Fatalf("create agent users: %v", err)
	}
	if err := db.Create(&[]models.Agent{
		{UserID: agentAUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a", Department: "support", AvgResponseTime: 12, Rating: 4.9},
		{UserID: agentBUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-b", Department: "support", AvgResponseTime: 88, Rating: 1.5},
	}).Error; err != nil {
		t.Fatalf("create agents: %v", err)
	}

	if err := db.Create(&[]models.Ticket{
		{
			ID:          501,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AgentID:     &agentAUser.ID,
			Title:       "scoped resolved",
			Status:      "resolved",
			CreatedAt:   start.Add(1 * time.Hour),
			ResolvedAt:  timeRef(start.Add(2 * time.Hour)),
			UpdatedAt:   start.Add(2 * time.Hour),
		},
		{
			ID:          502,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AgentID:     &agentAUser.ID,
			Title:       "scoped open",
			Status:      "open",
			CreatedAt:   start.Add(3 * time.Hour),
			UpdatedAt:   start.Add(3 * time.Hour),
		},
		{
			ID:          503,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-b",
			AgentID:     &agentBUser.ID,
			Title:       "cross workspace resolved",
			Status:      "resolved",
			CreatedAt:   start.Add(4 * time.Hour),
			ResolvedAt:  timeRef(start.Add(5 * time.Hour)),
			UpdatedAt:   start.Add(5 * time.Hour),
		},
	}).Error; err != nil {
		t.Fatalf("create tickets: %v", err)
	}

	stats, err := svc.GetAgentPerformanceStats(scopedContext("tenant-a", "workspace-a"), start, end, 10)
	if err != nil {
		t.Fatalf("GetAgentPerformanceStats() error = %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 scoped row, got %+v", stats)
	}
	if stats[0].AgentID != agentAUser.ID || stats[0].AgentName != "Agent A" {
		t.Fatalf("unexpected scoped agent stats row: %+v", stats[0])
	}
	if stats[0].TotalTickets != 2 || stats[0].ResolvedTickets != 1 {
		t.Fatalf("unexpected scoped aggregates: %+v", stats[0])
	}
}

func TestStatisticsService_CategoryPriorityAndSourceStatsApplyScope(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	customerAUser := models.User{ID: 31, Username: "customer_a", Email: "customer_a@test.com", Role: "customer"}
	customerBUser := models.User{ID: 32, Username: "customer_b", Email: "customer_b@test.com", Role: "customer"}
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
			ID:          601,
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
			ID:          602,
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
			ID:          603,
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

	categoryStats, err := svc.GetTicketCategoryStats(scopedContext("tenant-a", "workspace-a"), start, end)
	if err != nil {
		t.Fatalf("GetTicketCategoryStats() error = %v", err)
	}
	if len(categoryStats) != 1 || categoryStats[0].Category != "billing" || categoryStats[0].Count != 2 {
		t.Fatalf("unexpected scoped category stats: %+v", categoryStats)
	}

	priorityStats, err := svc.GetTicketPriorityStats(scopedContext("tenant-a", "workspace-a"), start, end)
	if err != nil {
		t.Fatalf("GetTicketPriorityStats() error = %v", err)
	}
	if len(priorityStats) != 2 {
		t.Fatalf("unexpected scoped priority stats: %+v", priorityStats)
	}
	for _, item := range priorityStats {
		if item.Category == "urgent" {
			t.Fatalf("unexpected cross-workspace priority stat: %+v", priorityStats)
		}
	}

	sourceStats, err := svc.GetCustomerSourceStats(scopedContext("tenant-a", "workspace-a"))
	if err != nil {
		t.Fatalf("GetCustomerSourceStats() error = %v", err)
	}
	if len(sourceStats) != 1 || sourceStats[0].Category != "web" || sourceStats[0].Count != 1 {
		t.Fatalf("unexpected scoped source stats: %+v", sourceStats)
	}
}

func TestStatisticsService_GetTicketPriorityStats_Empty(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := svc.GetTicketPriorityStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Fatalf("GetTicketPriorityStats() error = %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 priority stats, got %d", len(stats))
	}
}

func timeRef(t time.Time) *time.Time {
	return &t
}

func TestStatisticsService_GetTicketPriorityStats_WithPriorities(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	// Create tickets with different priorities
	db.Create(&models.Ticket{Title: "T1", Priority: "high", Status: "open"})
	db.Create(&models.Ticket{Title: "T2", Priority: "low", Status: "open"})
	db.Create(&models.Ticket{Title: "T3", Priority: "high", Status: "open"})

	startDate := time.Now().AddDate(0, 0, -1)
	endDate := time.Now()

	stats, err := svc.GetTicketPriorityStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Fatalf("GetTicketPriorityStats() error = %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("expected 2 priority stats, got %d", len(stats))
	}
}

func TestStatisticsService_GetCustomerSourceStats_Empty(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	stats, err := svc.GetCustomerSourceStats(context.Background())
	if err != nil {
		t.Fatalf("GetCustomerSourceStats() error = %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 source stats, got %d", len(stats))
	}
}

func TestStatisticsService_GetCustomerSourceStats_WithSources(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	// Create customers with different sources
	db.Create(&models.Customer{UserID: 1, Source: "web"})
	db.Create(&models.Customer{UserID: 2, Source: "api"})
	db.Create(&models.Customer{UserID: 3, Source: "web"})

	stats, err := svc.GetCustomerSourceStats(context.Background())
	if err != nil {
		t.Fatalf("GetCustomerSourceStats() error = %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("expected 2 source stats, got %d", len(stats))
	}
	// Find web source
	var webStat *CategoryStats
	for i := range stats {
		if stats[i].Category == "web" {
			webStat = &stats[i]
			break
		}
	}
	if webStat == nil {
		t.Fatal("web source not found")
	}
	if webStat.Count != 2 {
		t.Errorf("expected 2 web customers, got %d", webStat.Count)
	}
}

func TestStatisticsService_UpdateDailyStats_NewRecord(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	date := time.Now().Truncate(24 * time.Hour)

	err := svc.UpdateDailyStats(context.Background(), date)
	if err != nil {
		t.Fatalf("UpdateDailyStats() error = %v", err)
	}

	// Verify record was created
	var dailyStats models.DailyStats
	err = db.Where("date = ?", date).First(&dailyStats).Error
	if err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
}

func TestStatisticsService_UpdateDailyStats_IgnoresRequestScopeForSystemAggregate(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	scopedCtx := scopedContext("tenant-a", "workspace-a")

	if err := db.Create(&models.User{ID: 801, Username: "cust-a", Email: "cust-a@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer user: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: 801, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 802, TenantID: "tenant-a", WorkspaceID: "workspace-a", AvgResponseTime: 30}).Error; err != nil {
		t.Fatalf("seed agent a: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 803, TenantID: "tenant-b", WorkspaceID: "workspace-b", AvgResponseTime: 90}).Error; err != nil {
		t.Fatalf("seed agent b: %v", err)
	}

	resolvedAtA := date.Add(3 * time.Hour)
	resolvedAtB := date.Add(5 * time.Hour)
	tickets := []models.Ticket{
		{ID: 811, TenantID: "tenant-a", WorkspaceID: "workspace-a", Title: "ticket-a", Status: "resolved", CreatedAt: date.Add(2 * time.Hour), UpdatedAt: resolvedAtA, ResolvedAt: &resolvedAtA},
		{ID: 812, TenantID: "tenant-b", WorkspaceID: "workspace-b", Title: "ticket-b", Status: "resolved", CreatedAt: date.Add(4 * time.Hour), UpdatedAt: resolvedAtB, ResolvedAt: &resolvedAtB},
	}
	if err := db.Create(&tickets).Error; err != nil {
		t.Fatalf("seed tickets: %v", err)
	}
	if err := db.Create(&[]models.Session{
		{ID: "stats-sess-a", TenantID: "tenant-a", WorkspaceID: "workspace-a", CreatedAt: date.Add(time.Hour), StartedAt: date.Add(time.Hour)},
		{ID: "stats-sess-b", TenantID: "tenant-b", WorkspaceID: "workspace-b", CreatedAt: date.Add(2 * time.Hour), StartedAt: date.Add(2 * time.Hour)},
	}).Error; err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	if err := db.Create(&[]models.Message{
		{SessionID: "stats-sess-a", TenantID: "tenant-a", WorkspaceID: "workspace-a", Content: "a", Type: "text", Sender: "customer", CreatedAt: date.Add(time.Hour)},
		{SessionID: "stats-sess-b", TenantID: "tenant-b", WorkspaceID: "workspace-b", Content: "b", Type: "text", Sender: "customer", CreatedAt: date.Add(2 * time.Hour)},
	}).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}
	if err := db.Create(&[]models.CustomerSatisfaction{
		{TicketID: 811, CustomerID: 801, TenantID: "tenant-a", WorkspaceID: "workspace-a", Rating: 4, Category: "overall", CreatedAt: date.Add(6 * time.Hour)},
		{TicketID: 812, CustomerID: 802, TenantID: "tenant-b", WorkspaceID: "workspace-b", Rating: 2, Category: "overall", CreatedAt: date.Add(7 * time.Hour)},
	}).Error; err != nil {
		t.Fatalf("seed satisfaction: %v", err)
	}

	if err := svc.UpdateDailyStats(scopedCtx, date); err != nil {
		t.Fatalf("UpdateDailyStats() error = %v", err)
	}

	var dailyStats models.DailyStats
	if err := db.Where("date = ?", date).First(&dailyStats).Error; err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
	if dailyStats.TotalSessions != 2 || dailyStats.TotalMessages != 2 || dailyStats.TotalTickets != 2 || dailyStats.ResolvedTickets != 2 {
		t.Fatalf("expected global daily stats despite scoped ctx, got %+v", dailyStats)
	}
	if dailyStats.AvgResponseTime != 60 {
		t.Fatalf("expected avg response time 60, got %+v", dailyStats)
	}
	if dailyStats.CustomerSatisfaction != 3 {
		t.Fatalf("expected global satisfaction avg 3, got %+v", dailyStats)
	}
}

func TestStatisticsService_IncrementAIUsage_NewRecord(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	svc.IncrementAIUsage(context.Background())

	// Verify record was created
	today := time.Now().Truncate(24 * time.Hour)
	var dailyStats models.DailyStats
	err := db.Where("date = ?", today).First(&dailyStats).Error
	if err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
	if dailyStats.AIUsageCount != 1 {
		t.Errorf("expected AI usage count 1, got %d", dailyStats.AIUsageCount)
	}
}

func TestStatisticsService_IncrementWeKnoraUsage_NewRecord(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	svc.IncrementWeKnoraUsage(context.Background())

	// Verify record was created
	today := time.Now().Truncate(24 * time.Hour)
	var dailyStats models.DailyStats
	err := db.Where("date = ?", today).First(&dailyStats).Error
	if err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
	if dailyStats.WeKnoraUsageCount != 1 {
		t.Errorf("expected WeKnora usage count 1, got %d", dailyStats.WeKnoraUsageCount)
	}
}

func TestStatisticsService_IncrementKnowledgeProviderUsage_NewRecord(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	svc.IncrementKnowledgeProviderUsage(context.Background())

	today := time.Now().Truncate(24 * time.Hour)
	var dailyStats models.DailyStats
	err := db.Where("date = ?", today).First(&dailyStats).Error
	if err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
	if dailyStats.WeKnoraUsageCount != 1 {
		t.Errorf("expected knowledge provider usage count 1, got %d", dailyStats.WeKnoraUsageCount)
	}
}

func TestStatisticsService_IncrementAIUsage_ExistingRecord(t *testing.T) {
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	today := time.Now().Truncate(24 * time.Hour)
	db.Create(&models.DailyStats{
		Date:         today,
		AIUsageCount: 5,
	})

	svc.IncrementAIUsage(context.Background())

	// Verify count was incremented
	var dailyStats models.DailyStats
	err := db.Where("date = ?", today).First(&dailyStats).Error
	if err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
	if dailyStats.AIUsageCount != 6 {
		t.Errorf("expected AI usage count 6, got %d", dailyStats.AIUsageCount)
	}
}

func TestStatisticsService_IncrementWeKnoraUsage_ExistingRecord(t *testing.T) {
	t.Skip("Skipping: SQLite doesn't support GORM's UpdateColumn with expression for column increments")
	db := newStatisticsServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewStatisticsService(db, logger)

	today := time.Now().Truncate(24 * time.Hour)
	db.Create(&models.DailyStats{
		Date:              today,
		WeKnoraUsageCount: 3,
	})

	svc.IncrementWeKnoraUsage(context.Background())

	// Verify count was incremented
	var dailyStats models.DailyStats
	err := db.Where("date = ?", today).First(&dailyStats).Error
	if err != nil {
		t.Fatalf("failed to find daily stats: %v", err)
	}
	if dailyStats.WeKnoraUsageCount != 4 {
		t.Errorf("expected WeKnora usage count 4, got %d", dailyStats.WeKnoraUsageCount)
	}
}
