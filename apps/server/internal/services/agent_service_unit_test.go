package services

import (
	"context"
	"strconv"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newAgentUnitTestService(t *testing.T) (*AgentService, *gorm.DB) {
	t.Helper()
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return NewAgentService(db, logger), db
}

func seedAgentUser(t *testing.T, db *gorm.DB, id uint) *models.User {
	t.Helper()
	user := &models.User{
		ID:       id,
		Username: "agent-" + strconv.Itoa(int(id)),
		Email:    "agent-" + strconv.Itoa(int(id)) + "@x.com",
		Name:     "Agent " + strconv.Itoa(int(id)),
		Role:     "agent",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
	return user
}

func TestAgentService_Unit_AllMethods(t *testing.T) {
	svc, db := newAgentUnitTestService(t)
	ctx := context.Background()

	seedAgentUser(t, db, 1)
	seedAgentUser(t, db, 2)

	created, err := svc.CreateAgent(ctx, &AgentCreateRequest{
		UserID:        1,
		Department:    "support",
		Skills:        " tech , , voice ",
		MaxConcurrent: 4,
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if created.UserID != 1 {
		t.Fatalf("unexpected agent: %+v", created)
	}

	got, err := svc.GetAgentByUserID(ctx, 1)
	if err != nil || got.UserID != 1 {
		t.Fatalf("GetAgentByUserID: %v %+v", err, got)
	}
	if _, err := svc.GetAgentByUserID(ctx, 4242); err == nil {
		t.Fatal("expected error for missing agent")
	}

	if _, err := svc.ListAgents(ctx, 10); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	if err := svc.AgentGoOnline(ctx, 1); err != nil {
		t.Fatalf("AgentGoOnline: %v", err)
	}
	if err := svc.AgentGoOnline(ctx, 4242); err == nil {
		t.Fatal("expected online error for missing agent")
	}
	if err := svc.AgentGoOffline(ctx, 1); err != nil {
		t.Fatalf("AgentGoOffline: %v", err)
	}
	if err := svc.AgentGoOffline(ctx, 4242); err == nil {
		t.Fatal("expected offline error for missing agent")
	}

	if err := svc.UpdateAgentStatus(ctx, 1, "busy"); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}
	if err := svc.UpdateAgentStatus(ctx, 1, "nonsense"); err == nil {
		t.Fatal("expected invalid status error")
	}

	version, err := svc.RevokeAgentTokens(ctx, 1)
	if err != nil {
		t.Fatalf("RevokeAgentTokens: %v", err)
	}
	if version < 1 {
		t.Fatalf("expected positive token version, got %d", version)
	}
	if _, err := svc.RevokeAgentTokens(ctx, 0); err == nil {
		t.Fatal("expected error for zero user id")
	}
}

func TestAgentService_Unit_Sessions(t *testing.T) {
	svc, db := newAgentUnitTestService(t)
	ctx := context.Background()

	seedAgentUser(t, db, 1)
	if _, err := svc.CreateAgent(ctx, &AgentCreateRequest{UserID: 1}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := svc.AgentGoOnline(ctx, 1); err != nil {
		t.Fatalf("AgentGoOnline: %v", err)
	}
	if err := db.Create(&models.Session{ID: "sess-a", Status: "active", StartedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := svc.AssignSessionToAgent(ctx, "sess-a", 1); err != nil {
		t.Fatalf("AssignSessionToAgent: %v", err)
	}
	if err := svc.AssignSessionToAgent(ctx, "sess-a", 4242); err == nil {
		t.Fatal("expected assign error for missing agent")
	}
	if err := svc.ReleaseSessionFromAgent(ctx, "sess-a", 1); err != nil {
		t.Fatalf("ReleaseSessionFromAgent: %v", err)
	}
	if err := svc.ReleaseSessionFromAgent(ctx, "sess-a", 4242); err == nil {
		t.Fatal("expected release error for missing agent")
	}
}

func TestAgentService_Unit_QueryAndStats(t *testing.T) {
	svc, db := newAgentUnitTestService(t)
	ctx := context.Background()

	seedAgentUser(t, db, 1)
	if _, err := svc.CreateAgent(ctx, &AgentCreateRequest{UserID: 1}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := svc.AgentGoOnline(ctx, 1); err != nil {
		t.Fatalf("AgentGoOnline: %v", err)
	}

	found, err := svc.FindAvailableAgent(ctx, []string{"tech"}, "high")
	if err != nil {
		t.Fatalf("FindAvailableAgent: %v", err)
	}
	if found.UserID != 1 {
		t.Fatalf("unexpected agent: %+v", found)
	}

	online := svc.GetOnlineAgents(ctx)
	if len(online) != 1 || online[0].UserID != 1 {
		t.Fatalf("unexpected online agents: %+v", online)
	}

	info, ok := svc.GetOnlineAgent(ctx, 1)
	if !ok || info.UserID != 1 {
		t.Fatalf("GetOnlineAgent: %v %+v", ok, info)
	}
	if _, ok := svc.GetOnlineAgent(ctx, 4242); ok {
		t.Fatal("expected miss for unknown agent")
	}

	if err := svc.AgentGoOffline(ctx, 1); err != nil {
		t.Fatalf("AgentGoOffline: %v", err)
	}
	if _, err := svc.FindAvailableAgent(ctx, nil, ""); err == nil {
		t.Fatal("expected error when all agents offline")
	}

	agentID := uint(1)
	stats, err := svc.GetAgentStats(ctx, &agentID)
	if err != nil {
		t.Fatalf("GetAgentStats: %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if _, err := svc.GetAgentStats(ctx, uintPtr(4242)); err != nil {
		t.Fatalf("GetAgentStats for unknown filter should not error: %v", err)
	}
}

func TestAgentService_Unit_ApplySessionTransfer(t *testing.T) {
	svc, db := newAgentUnitTestService(t)
	seedAgentUser(t, db, 1)
	seedAgentUser(t, db, 2)
	for _, id := range []uint{1, 2} {
		if _, err := svc.CreateAgent(context.Background(), &AgentCreateRequest{UserID: id}); err != nil {
			t.Fatalf("CreateAgent %d: %v", id, err)
		}
	}

	from := uint(1)
	svc.ApplySessionTransfer(context.Background(), "sess-x", &from, 2)
	svc.ApplySessionTransfer(nil, "sess-x", nil, 4242) // nil ctx + failing module call must not panic
}

func TestAgentService_Unit_ParseSkills(t *testing.T) {
	svc, _ := newAgentUnitTestService(t)
	if got := svc.parseSkills(""); len(got) != 0 {
		t.Fatalf("expected empty skills, got %+v", got)
	}
	got := svc.parseSkills(" a ,, b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected skills: %+v", got)
	}
}

func TestMapRuntimeToLegacyNil(t *testing.T) {
	if mapRuntimeToLegacy(nil) != nil {
		t.Fatal("nil runtime should map to nil")
	}
}
