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

func newWorkspaceServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:workspace_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Session{},
		&models.Agent{},
		&models.User{},
		&models.Ticket{},
		&models.Customer{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestWorkspaceService_GetOverview_EmptyDB(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := NewAgentService(db, logger)
	svc := NewWorkspaceService(db, agentSvc)

	overview, err := svc.GetOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview == nil {
		t.Fatal("expected overview, got nil")
	}
	if overview.TotalActiveSessions != 0 {
		t.Errorf("expected 0 active sessions, got %d", overview.TotalActiveSessions)
	}
	if overview.OnlineAgents != 0 {
		t.Errorf("expected 0 online agents, got %d", overview.OnlineAgents)
	}
}

func TestWorkspaceService_GetOverview_WithSessions(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := NewAgentService(db, logger)
	svc := NewWorkspaceService(db, agentSvc)

	// Create test users
	agentUser := &models.User{
		Username: "agent1",
		Email:    "agent1@test.com",
		Name:     "Agent One",
		Role:     "agent",
	}
	customerUser := &models.User{
		Username: "customer1",
		Email:    "customer1@test.com",
		Name:     "Customer One",
		Role:     "customer",
	}
	db.Create(agentUser)
	db.Create(customerUser)

	// Create agent
	agent := &models.Agent{
		UserID: agentUser.ID,
		Status: "online",
	}
	db.Create(agent)

	// Create active session
	session := &models.Session{
		Platform:  "web",
		Status:    "active",
		AgentID:   &agent.ID,
		StartedAt: time.Now(),
	}
	db.Create(session)

	// Make agent online first
	agentSvc.AgentGoOnline(context.Background(), agent.UserID)

	overview, err := svc.GetOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.TotalActiveSessions != 1 {
		t.Errorf("expected 1 active session, got %d", overview.TotalActiveSessions)
	}
	if overview.OnlineAgents != 1 {
		t.Errorf("expected 1 online agent, got %d", overview.OnlineAgents)
	}
}

func TestWorkspaceService_GetOverview_DefaultLimit(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := NewAgentService(db, logger)
	svc := NewWorkspaceService(db, agentSvc)

	// Test with limit <= 0 (should default to 10)
	overview, err := svc.GetOverview(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview == nil {
		t.Fatal("expected overview, got nil")
	}
}

func TestWorkspaceService_GetOverview_WaitingSessions(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := NewAgentService(db, logger)
	svc := NewWorkspaceService(db, agentSvc)

	// Create active session without agent (waiting)
	session := &models.Session{
		Platform:  "web",
		Status:    "active",
		AgentID:   nil,
		StartedAt: time.Now(),
	}
	db.Create(session)

	overview, err := svc.GetOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.WaitingQueue != 1 {
		t.Errorf("expected 1 waiting session, got %d", overview.WaitingQueue)
	}
}

func TestWorkspaceService_GetOverview_MultipleChannels(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := NewAgentService(db, logger)
	svc := NewWorkspaceService(db, agentSvc)

	// Create sessions for different platforms with unique IDs
	for _, platform := range []string{"web", "api", "mobile"} {
		session := &models.Session{
			ID:        "test-session-" + platform,
			Platform:  platform,
			Status:    "active",
			AgentID:   nil,
			StartedAt: time.Now(),
		}
		db.Create(session)
	}

	overview, err := svc.GetOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if len(overview.Channels) != 3 {
		t.Errorf("expected 3 channels, got %d", len(overview.Channels))
	}
}

func TestWorkspaceService_GetOverview_BusyAgents(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := NewAgentService(db, logger)
	svc := NewWorkspaceService(db, agentSvc)

	// Create users and agents
	agent1User := &models.User{Username: "agent1", Email: "agent1@test.com", Name: "Agent 1", Role: "agent"}
	agent2User := &models.User{Username: "agent2", Email: "agent2@test.com", Name: "Agent 2", Role: "agent"}
	db.Create(agent1User)
	db.Create(agent2User)

	// Create agents - one busy, one online
	agent1 := &models.Agent{UserID: agent1User.ID, Status: "online", CurrentLoad: 5, MaxConcurrent: 5}
	agent2 := &models.Agent{UserID: agent2User.ID, Status: "online", CurrentLoad: 2, MaxConcurrent: 5}
	db.Create(agent1)
	db.Create(agent2)

	// Make them online
	agentSvc.AgentGoOnline(context.Background(), agent1.UserID)
	agentSvc.AgentGoOnline(context.Background(), agent2.UserID)

	overview, err := svc.GetOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.BusyAgents != 1 {
		t.Errorf("expected 1 busy agent, got %d", overview.BusyAgents)
	}
}

func TestWorkspaceService_GetOverview_NilAgentService(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	svc := NewWorkspaceService(db, nil)

	// Create active session
	session := &models.Session{
		Platform:  "web",
		Status:    "active",
		AgentID:   nil,
		StartedAt: time.Now(),
	}
	db.Create(session)

	overview, err := svc.GetOverview(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.OnlineAgents != 0 {
		t.Errorf("expected 0 online agents (nil service), got %d", overview.OnlineAgents)
	}
}

func TestNewWorkspaceService(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	logger := logrus.New()
	agentSvc := NewAgentService(db, logger)

	svc := NewWorkspaceService(db, agentSvc)

	if svc == nil {
		t.Fatal("expected service, got nil")
	}
	if svc.db != db {
		t.Error("expected db to be set")
	}
	if svc.agentService != agentSvc {
		t.Error("expected agentService to be set")
	}
}

func TestChannelSummary_Structure(t *testing.T) {
	summary := ChannelSummary{
		Platform:        "web",
		ActiveSessions:  10,
		WaitingSessions: 2,
		AvgResponseTime: 150.5,
	}

	if summary.Platform != "web" {
		t.Errorf("expected platform 'web', got '%s'", summary.Platform)
	}
	if summary.ActiveSessions != 10 {
		t.Errorf("expected 10 active sessions, got %d", summary.ActiveSessions)
	}
}

func TestWorkspaceOverview_Structure(t *testing.T) {
	overview := &WorkspaceOverview{
		TotalActiveSessions: 5,
		WaitingQueue:        2,
		OnlineAgents:        3,
		BusyAgents:          1,
		Channels: []ChannelSummary{
			{Platform: "web", ActiveSessions: 3},
		},
		RecentSessions: []WorkspaceSession{
			{ID: "sess1", Platform: "web"},
		},
	}

	if overview.TotalActiveSessions != 5 {
		t.Errorf("expected 5 total active sessions, got %d", overview.TotalActiveSessions)
	}
	if len(overview.Channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(overview.Channels))
	}
}

func TestWorkspaceService_GetOverview_AppliesScope(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	svc := NewWorkspaceService(db, nil)
	now := time.Now()

	db.Create(&models.Agent{UserID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "online", AvgResponseTime: 10})
	db.Create(&models.Agent{UserID: 2, TenantID: "tenant-b", WorkspaceID: "workspace-b", Status: "busy", AvgResponseTime: 20, CurrentLoad: 5, MaxConcurrent: 5})
	db.Create(&models.Session{ID: "sess-a", TenantID: "tenant-a", WorkspaceID: "workspace-a", Platform: "web", Status: "active", StartedAt: now})
	db.Create(&models.Session{ID: "sess-b", TenantID: "tenant-b", WorkspaceID: "workspace-b", Platform: "api", Status: "active", StartedAt: now})

	overview, err := svc.GetOverview(scopedContext("tenant-a", "workspace-a"), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.TotalActiveSessions != 1 || overview.OnlineAgents != 1 || overview.BusyAgents != 0 {
		t.Fatalf("unexpected scoped overview: %+v", overview)
	}
	if len(overview.Channels) != 1 || overview.Channels[0].Platform != "web" {
		t.Fatalf("unexpected scoped channels: %+v", overview.Channels)
	}
}

func TestWorkspaceService_GetOverview_RecentSessionsDoesNotLeakCrossScopeJoins(t *testing.T) {
	db := newWorkspaceServiceTestDB(t)
	svc := NewWorkspaceService(db, nil)
	now := time.Now()

	customerAUser := &models.User{Username: "customer-a", Email: "customer-a@test.com", Name: "Customer A", Role: "customer"}
	customerBUser := &models.User{Username: "customer-b", Email: "customer-b@test.com", Name: "Customer B", Role: "customer"}
	agentAUser := &models.User{Username: "agent-a", Email: "agent-a@test.com", Name: "Agent A", Role: "agent"}
	agentBUser := &models.User{Username: "agent-b", Email: "agent-b@test.com", Name: "Agent B", Role: "agent"}
	if err := db.Create(customerAUser).Error; err != nil {
		t.Fatalf("create customer A user: %v", err)
	}
	if err := db.Create(customerBUser).Error; err != nil {
		t.Fatalf("create customer B user: %v", err)
	}
	if err := db.Create(agentAUser).Error; err != nil {
		t.Fatalf("create agent A user: %v", err)
	}
	if err := db.Create(agentBUser).Error; err != nil {
		t.Fatalf("create agent B user: %v", err)
	}

	customerA := &models.Customer{TenantID: "tenant-a", WorkspaceID: "workspace-a", UserID: customerAUser.ID}
	customerB := &models.Customer{TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: customerBUser.ID}
	if err := db.Create(customerA).Error; err != nil {
		t.Fatalf("create customer A: %v", err)
	}
	if err := db.Create(customerB).Error; err != nil {
		t.Fatalf("create customer B: %v", err)
	}

	agentA := &models.Agent{TenantID: "tenant-a", WorkspaceID: "workspace-a", UserID: agentAUser.ID, Status: "online"}
	agentB := &models.Agent{TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: agentBUser.ID, Status: "online"}
	if err := db.Create(agentA).Error; err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	if err := db.Create(agentB).Error; err != nil {
		t.Fatalf("create agent B: %v", err)
	}

	ticketB := &models.Ticket{
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		Title:       "cross-scope ticket",
		CustomerID:  customerBUser.ID,
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	sessionA := &models.Session{
		ID:          "sess-cross-scope",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		TicketID:    &ticketB.ID,
		AgentID:     &agentBUser.ID,
		Platform:    "web",
		Status:      "active",
		StartedAt:   now,
	}
	if err := db.Create(sessionA).Error; err != nil {
		t.Fatalf("create session A: %v", err)
	}

	overview, err := svc.GetOverview(scopedContext("tenant-a", "workspace-a"), 10)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if len(overview.RecentSessions) != 1 {
		t.Fatalf("expected 1 recent session, got %d", len(overview.RecentSessions))
	}

	recent := overview.RecentSessions[0]
	if recent.CustomerName != "" || recent.CustomerID != nil {
		t.Fatalf("expected customer join to be scope-protected, got %+v", recent)
	}
	if recent.AgentName != "" {
		t.Fatalf("expected agent join to be scope-protected, got %+v", recent)
	}
}
