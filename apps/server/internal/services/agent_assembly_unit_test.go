package services

import (
	"testing"
	"time"

	"servify/apps/server/internal/models"

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
