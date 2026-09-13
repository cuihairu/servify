package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func TestBuildAgentServiceAssembly_InMemory(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	asm := BuildAgentServiceAssembly(db, logrus.New(), nil)
	if asm == nil || asm.Service == nil || asm.Maintenance == nil {
		t.Fatalf("unexpected assembly: %+v", asm)
	}
}

func TestBuildAgentServiceAssembly_Redis(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	asm := BuildAgentServiceAssembly(db, logrus.New(), client)
	if asm == nil || asm.Service == nil {
		t.Fatalf("unexpected assembly: %+v", asm)
	}
}

func TestNewAgentServiceWithDependencies_NilLogger(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	svc := NewAgentServiceWithDependencies(AgentServiceDependencies{DB: db, Logger: nil})
	if svc == nil {
		t.Fatal("expected service")
	}
}

func TestAgentRuntimeMaintenance_StartSmoke(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	asm := BuildAgentServiceAssembly(db, logrus.New(), nil)
	go asm.Maintenance.Start()
	// give the goroutine a moment to enter the loop; the process exits with the test
	time.Sleep(20 * time.Millisecond)
}

func TestAgentRuntimeCache(t *testing.T) {
	cache := &agentRuntimeCache{}
	if _, ok := cache.Load(1); ok {
		t.Fatal("expected miss on empty cache")
	}
	cache.Store(1, &AgentInfo{UserID: 1})
	if info, ok := cache.Load(1); !ok || info.UserID != 1 {
		t.Fatalf("expected hit, got %v %v", info, ok)
	}
	cache.agents.Store(uint(2), "not-an-agent")
	if _, ok := cache.Load(2); ok {
		t.Fatal("expected miss for wrong stored type")
	}
	if stale := cache.CollectStale(map[uint]struct{}{1: {}}); len(stale) != 1 || stale[0] != 2 {
		t.Fatalf("unexpected stale list: %+v", stale)
	}
	cache.Delete(2)
	if stale := cache.CollectStale(map[uint]struct{}{}); len(stale) != 1 {
		t.Fatalf("expected remaining stale entry: %+v", stale)
	}
	// non-uint keys are ignored by CollectStale
	cache.agents.Store("bad", "x")
	if stale := cache.CollectStale(map[uint]struct{}{}); len(stale) != 1 {
		t.Fatalf("expected stale entry to ignore non-uint keys: %+v", stale)
	}
}

func TestAgentLegacyRuntimeAdapter_SyncRemovesStale(t *testing.T) {
	cache := &agentRuntimeCache{}
	cache.Store(99, &AgentInfo{UserID: 99})
	adapter := newAgentLegacyRuntimeAdapter(cache)

	repo := &maintenanceRepo{profile: nil, model: nil}
	registry := &maintenanceRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}}
	module := agentapp.NewService(repo, registry)
	adapter.Sync(context.Background(), module)

	if _, ok := adapter.GetOnlineAgent(99); ok {
		t.Fatal("expected stale agent to be removed")
	}
}
