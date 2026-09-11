//go:build integration
// +build integration

package services

import (
	"context"
	"strconv"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
)

func newAgentServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:agent_service_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Session{},
		&models.Ticket{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestAgentService_CreateAgent(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Name:     "Test User",
		Role:     "agent",
		Status:   "active",
	}
	db.Create(user)

	req := &AgentCreateRequest{
		UserID:        user.ID,
		Department:    "support",
		Skills:        "technical,support",
		MaxConcurrent: 5,
	}

	agent, err := svc.CreateAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	if agent.ID == 0 {
		t.Error("expected agent ID to be set")
	}
	if agent.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if agent.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
	if agent.Department != "support" {
		t.Errorf("expected department 'support', got '%s'", agent.Department)
	}
}

func TestAgentService_ListAgents(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	// 创建测试用户和客服
	for i := 1; i <= 3; i++ {
		user := &models.User{
			ID:       uint(i),
			Username: "agent-" + strconv.Itoa(i),
			Email:    "agent-" + strconv.Itoa(i) + "@example.com",
			Name:     "Agent",
			Role:     "agent",
		}
		db.Create(user)

		agent := &models.Agent{
			UserID:     uint(i),
			Department: "support",
			Status:     "online",
		}
		db.Create(agent)
	}

	agents, err := svc.ListAgents(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}

	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
	}
}

func TestAgentService_AgentGoOffline(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	user := &models.User{
		ID:       1,
		Username: "offline_agent",
		Email:    "offline@example.com",
		Name:     "Offline Agent",
		Role:     "agent",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:     1,
		Department: "support",
		Status:     "online",
	}
	db.Create(agent)

	// 先上线
	if err := svc.AgentGoOnline(context.Background(), 1); err != nil {
		t.Fatalf("AgentGoOnline() error = %v", err)
	}

	// 然后下线
	if err := svc.AgentGoOffline(context.Background(), 1); err != nil {
		t.Fatalf("AgentGoOffline() error = %v", err)
	}

	// 验证状态
	var dbAgent models.Agent
	if err := db.Where("user_id = ?", 1).First(&dbAgent).Error; err != nil {
		t.Fatalf("failed to query agent: %v", err)
	}

	if dbAgent.Status != "offline" {
		t.Errorf("expected status 'offline', got '%s'", dbAgent.Status)
	}

	// 验证不在在线列表中
	if _, ok := svc.GetOnlineAgent(context.Background(), 1); ok {
		t.Error("agent should not be in online agents list")
	}
}

func TestAgentService_UpdateAgentStatus(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	user := &models.User{
		ID:       1,
		Username: "status_agent",
		Email:    "status@example.com",
		Name:     "Status Agent",
		Role:     "agent",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:     1,
		Department: "support",
		Status:     "online",
	}
	db.Create(agent)

	err := svc.UpdateAgentStatus(context.Background(), 1, "busy")
	if err != nil {
		t.Fatalf("UpdateAgentStatus() error = %v", err)
	}

	var dbAgent models.Agent
	if err := db.Where("user_id = ?", 1).First(&dbAgent).Error; err != nil {
		t.Fatalf("failed to query agent: %v", err)
	}

	if dbAgent.Status != "busy" {
		t.Errorf("expected status 'busy', got '%s'", dbAgent.Status)
	}
}

func TestAgentService_GetOnlineAgents(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	// 创建并上线多个客服
	for i := 1; i <= 3; i++ {
		user := &models.User{
			ID:       uint(i),
			Username: "online-agent-" + strconv.Itoa(i),
			Email:    "online-" + strconv.Itoa(i) + "@example.com",
			Name:     "Online Agent",
			Role:     "agent",
		}
		db.Create(user)

		agent := &models.Agent{
			UserID:     uint(i),
			Department: "support",
			Status:     "online",
		}
		db.Create(agent)

		if err := svc.AgentGoOnline(context.Background(), uint(i)); err != nil {
			t.Fatalf("AgentGoOnline() error = %v", err)
		}
	}

	agents := svc.GetOnlineAgents(context.Background())
	if len(agents) != 3 {
		t.Errorf("expected 3 online agents, got %d", len(agents))
	}
}

func TestAgentService_GetOnlineAgent_UsesDatabaseStateAfterRestart(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	user := &models.User{
		ID:       21,
		Username: "restart_agent",
		Email:    "restart@example.com",
		Name:     "Restart Agent",
		Role:     "agent",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	agent := &models.Agent{
		UserID:        user.ID,
		Status:        "online",
		CurrentLoad:   2,
		MaxConcurrent: 5,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	svc := NewAgentService(db, logger)
	agentInfo, ok := svc.GetOnlineAgent(context.Background(), user.ID)
	if !ok {
		t.Fatal("expected online agent from database state")
	}
	if agentInfo.CurrentLoad != 2 {
		t.Fatalf("expected CurrentLoad 2, got %d", agentInfo.CurrentLoad)
	}
	if !agentInfo.LastActivity.IsZero() {
		t.Fatalf("expected zero LastActivity without runtime metadata, got %v", agentInfo.LastActivity)
	}
}

func TestAgentService_GetAgentStats(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	user := &models.User{
		ID:       1,
		Username: "stats_agent",
		Email:    "stats@example.com",
		Name:     "Stats Agent",
		Role:     "agent",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:          1,
		Department:      "support",
		Status:          "online",
		MaxConcurrent:   5,
		CurrentLoad:     3,
		AvgResponseTime: 150,
	}
	db.Create(agent)

	// 先上线
	if err := svc.AgentGoOnline(context.Background(), 1); err != nil {
		t.Fatalf("AgentGoOnline() error = %v", err)
	}

	agentID := uint(1)
	stats, err := svc.GetAgentStats(context.Background(), &agentID)
	if err != nil {
		t.Fatalf("GetAgentStats() error = %v", err)
	}

	if stats.Total != 1 {
		t.Errorf("expected Total 1, got %d", stats.Total)
	}

	if stats.Online != 1 {
		t.Errorf("expected Online 1, got %d", stats.Online)
	}

	if stats.AvgResponseTime != 150 {
		t.Errorf("expected AvgResponseTime 150, got %d", stats.AvgResponseTime)
	}
}

func TestAgentService_GetAgentStats_AppliesScope(t *testing.T) {
	db := newAgentServiceTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)

	if err := db.Create(&[]models.User{
		{ID: 11, Username: "agent-a", Email: "agent-a@test.com", Name: "Agent A", Role: "agent"},
		{ID: 12, Username: "agent-b", Email: "agent-b@test.com", Name: "Agent B", Role: "agent"},
	}).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Create(&[]models.Agent{
		{UserID: 11, TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "online", AvgResponseTime: 10, Rating: 4.8},
		{UserID: 12, TenantID: "tenant-a", WorkspaceID: "workspace-b", Status: "busy", AvgResponseTime: 90, Rating: 2.0},
	}).Error; err != nil {
		t.Fatalf("create agents: %v", err)
	}

	stats, err := svc.GetAgentStats(scopedContext("tenant-a", "workspace-a"), nil)
	if err != nil {
		t.Fatalf("GetAgentStats() error = %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("unexpected scoped agent stats: %+v", stats)
	}
	if stats.AvgResponseTime != 10 {
		t.Fatalf("unexpected scoped avg response time: %+v", stats)
	}
}
