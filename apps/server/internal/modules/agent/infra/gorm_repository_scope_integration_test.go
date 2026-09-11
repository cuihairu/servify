//go:build integration
// +build integration

package infra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var agentInfraDBSeq uint32

func newAgentInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 全局唯一序号避免 -count 重跑或并行测试时命中同一命名内存库
	dsn := fmt.Sprintf("file:agent_infra_%s_%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"), atomic.AddUint32(&agentInfraDBSeq, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func scopedAgentContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestAgentRepositoryAppliesScopeOnCreateAndRead(t *testing.T) {
	db := newAgentInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	if err := db.Create(&models.User{ID: 1, Username: "agent1", Email: "agent1@example.com", Name: "Agent One", Role: "customer", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	ctxA := scopedAgentContext("tenant-a", "workspace-a")
	ctxB := scopedAgentContext("tenant-b", "workspace-b")

	if _, err := repo.CreateAgent(ctxA, 1, "support", []string{"billing"}, 5); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var stored models.Agent
	if err := db.First(&stored, "user_id = ?", 1).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected agent scope: %+v", stored)
	}

	if _, _, err := repo.GetAgentByUserID(ctxA, 1); err != nil {
		t.Fatalf("get agent with matching scope: %v", err)
	}
	if _, _, err := repo.GetAgentByUserID(ctxB, 1); err == nil {
		t.Fatal("expected cross-tenant agent lookup to fail")
	}
}

func TestAgentRepositoryFiltersListAndStatsByScope(t *testing.T) {
	db := newAgentInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	users := []models.User{
		{ID: 1, Username: "agent1", Email: "agent1@example.com", Role: "agent", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Username: "agent2", Email: "agent2@example.com", Role: "agent", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	agents := []models.Agent{
		{UserID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "online", AvgResponseTime: 10, Rating: 4.5, CreatedAt: now, UpdatedAt: now},
		{UserID: 2, TenantID: "tenant-b", WorkspaceID: "workspace-b", Status: "busy", AvgResponseTime: 20, Rating: 3.5, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&agents).Error; err != nil {
		t.Fatalf("seed agents: %v", err)
	}

	ctxA := scopedAgentContext("tenant-a", "workspace-a")
	items, err := repo.ListAgents(ctxA, 20)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(items) != 1 || items[0].UserID != 1 {
		t.Fatalf("unexpected scoped agents: %+v", items)
	}

	stats, err := repo.GetStats(ctxA, nil)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("unexpected scoped stats: %+v", stats)
	}
}

func TestAgentRepositoryAssignSessionReturnsNotFoundWhenSessionMissing(t *testing.T) {
	db := newAgentInfraTestDB(t)
	repo := NewGormRepository(db)

	err := repo.AssignSession(context.Background(), "missing", 1)
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestAgentRepositoryReleaseSessionReturnsNotFoundWhenNotAssigned(t *testing.T) {
	db := newAgentInfraTestDB(t)
	repo := NewGormRepository(db)

	session := &models.Session{ID: "sess-1", Status: "active", UserID: 1}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	err := repo.ReleaseSession(context.Background(), "sess-1", 99)
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}
