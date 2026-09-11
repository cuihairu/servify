//go:build integration
// +build integration

package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newAgentRedisIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:agent_redis_" + t.Name() + "")
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

func TestBuildAgentServiceAssembly_UsesRedisRegistryAcrossInstances(t *testing.T) {
	db := newAgentRedisIntegrationDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	user := &models.User{
		ID:       501,
		Username: "redis-agent",
		Email:    "redis-agent@example.com",
		Name:     "Redis Agent",
		Role:     "agent",
		Status:   "active",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	agent := &models.Agent{
		UserID:        user.ID,
		Department:    "support",
		Status:        "offline",
		MaxConcurrent: 5,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mr := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer clientA.Close()
	clientB := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer clientB.Close()

	assemblyA := BuildAgentServiceAssembly(db, logger, clientA)
	assemblyB := BuildAgentServiceAssembly(db, logger, clientB)

	if err := assemblyA.Service.AgentGoOnline(context.Background(), user.ID); err != nil {
		t.Fatalf("AgentGoOnline() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if got, ok := assemblyB.Service.GetOnlineAgent(context.Background(), user.ID); ok {
			if got.Status != "online" {
				t.Fatalf("expected online status, got %q", got.Status)
			}
			if got.ConnectedAt.IsZero() {
				t.Fatal("expected connected_at to be populated")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected second assembly to observe online agent via shared redis/db state")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBuildAgentServiceAssembly_SyncsTransferLoadAcrossInstances(t *testing.T) {
	db := newAgentRedisIntegrationDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	user1 := &models.User{
		ID:       601,
		Username: "redis-agent-1",
		Email:    "redis-agent-1@example.com",
		Name:     "Redis Agent One",
		Role:     "agent",
		Status:   "active",
	}
	user2 := &models.User{
		ID:       602,
		Username: "redis-agent-2",
		Email:    "redis-agent-2@example.com",
		Name:     "Redis Agent Two",
		Role:     "agent",
		Status:   "active",
	}
	for _, user := range []*models.User{user1, user2} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user %d: %v", user.ID, err)
		}
	}
	for _, agent := range []*models.Agent{
		{UserID: user1.ID, Department: "support", Status: "offline", MaxConcurrent: 5},
		{UserID: user2.ID, Department: "support", Status: "offline", MaxConcurrent: 5},
	} {
		if err := db.Create(agent).Error; err != nil {
			t.Fatalf("create agent %d: %v", agent.UserID, err)
		}
	}
	session := &models.Session{
		ID:        "redis-transfer-session",
		Platform:  "web",
		Status:    "active",
		StartedAt: time.Now(),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	mr := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer clientA.Close()
	clientB := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer clientB.Close()

	assemblyA := BuildAgentServiceAssembly(db, logger, clientA)
	assemblyB := BuildAgentServiceAssembly(db, logger, clientB)

	for _, userID := range []uint{user1.ID, user2.ID} {
		if err := assemblyA.Service.AgentGoOnline(context.Background(), userID); err != nil {
			t.Fatalf("AgentGoOnline(%d) error = %v", userID, err)
		}
	}
	if err := assemblyA.Service.AssignSessionToAgent(context.Background(), session.ID, user1.ID); err != nil {
		t.Fatalf("AssignSessionToAgent() error = %v", err)
	}

	waitForAgentLoad(t, assemblyB.Service, user1.ID, 1)
	waitForAgentLoad(t, assemblyB.Service, user2.ID, 0)

	assemblyA.Service.ApplySessionTransfer(context.Background(), session.ID, &user1.ID, user2.ID)

	waitForAgentLoad(t, assemblyB.Service, user1.ID, 0)
	waitForAgentLoad(t, assemblyB.Service, user2.ID, 1)
}

func waitForAgentLoad(t *testing.T, svc *AgentService, userID uint, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		info, ok := svc.GetOnlineAgent(context.Background(), userID)
		if ok && info.CurrentLoad == want {
			return
		}
		if time.Now().After(deadline) {
			if !ok {
				t.Fatalf("expected agent %d to be online", userID)
			}
			t.Fatalf("agent %d load = %d, want %d", userID, info.CurrentLoad, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
