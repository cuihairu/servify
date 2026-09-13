package infra

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var gamificationInfraDBSeq atomic.Uint64

func newGamificationInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:gamifinfra_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(gamificationInfraDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Ticket{}, &models.CustomerSatisfaction{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func seedGamificationAgents(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	users := []models.User{
		{ID: 1, Username: "agent-one", Email: "one@example.com", Name: "Agent One", Role: "agent"},
		{ID: 2, Username: "agent-two", Email: "two@example.com", Name: "Agent Two", Role: "agent"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	agents := []models.Agent{
		{UserID: 1, Department: "support", AvgResponseTime: 30, CreatedAt: now, UpdatedAt: now},
		{UserID: 2, Department: "billing", AvgResponseTime: 60, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&agents).Error; err != nil {
		t.Fatalf("seed agents: %v", err)
	}
}

func TestGormGamificationRepoConstruction(t *testing.T) {
	if NewGormRepository(newGamificationInfraTestDB(t)) == nil {
		t.Fatal("expected repository instance")
	}
}

func TestGormGamificationListAgentProfiles(t *testing.T) {
	db := newGamificationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	seedGamificationAgents(t, db)

	profiles, err := repo.ListAgentProfiles(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 2 || profiles[0].Username != "agent-one" || profiles[0].Name != "Agent One" {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}

	profiles, err = repo.ListAgentProfiles(ctx, "billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 1 || profiles[0].UserID != 2 || profiles[0].Department != "billing" {
		t.Fatalf("unexpected filtered profiles: %+v", profiles)
	}

	scoped := platformauth.ContextWithScope(ctx, "tenant-1", "ws-1")
	profiles, err = repo.ListAgentProfiles(scoped, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected no profiles for unknown tenant, got %+v", profiles)
	}

	if err := db.Migrator().DropTable(&models.Agent{}); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := repo.ListAgentProfiles(ctx, ""); err == nil {
		t.Fatal("expected query error after dropping agents")
	}
}

func TestGormGamificationListResolvedCounts(t *testing.T) {
	db := newGamificationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	agent1, agent2 := uint(1), uint(2)
	day := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	tickets := []models.Ticket{
		{ID: 11, Title: "r1", CustomerID: 100, AgentID: &agent1, Status: "resolved", ResolvedAt: &day, CreatedAt: day, UpdatedAt: day},
		{ID: 12, Title: "r2", CustomerID: 100, AgentID: &agent1, Status: "closed", ResolvedAt: &day, CreatedAt: day, UpdatedAt: day},
		{ID: 13, Title: "r3", CustomerID: 100, AgentID: &agent2, Status: "resolved", ResolvedAt: &day, CreatedAt: day, UpdatedAt: day},
		{ID: 14, Title: "open", CustomerID: 100, AgentID: &agent1, Status: "open", CreatedAt: day, UpdatedAt: day},
	}
	if err := db.Create(&tickets).Error; err != nil {
		t.Fatalf("seed tickets: %v", err)
	}

	counts, err := repo.ListResolvedCounts(ctx, "2026-05-01 00:00:00", "2026-05-31 23:59:59")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := map[uint]int64{}
	for _, c := range counts {
		got[c.AgentID] = c.Count
	}
	if got[1] != 2 || got[2] != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}

	scoped := platformauth.ContextWithScope(ctx, "tenant-2", "")
	counts, err = repo.ListResolvedCounts(scoped, "2026-05-01 00:00:00", "2026-05-31 23:59:59")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected no scoped counts, got %+v", counts)
	}

	if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := repo.ListResolvedCounts(ctx, "2026-05-01 00:00:00", "2026-05-31 23:59:59"); err == nil {
		t.Fatal("expected query error after dropping tickets")
	}
}

func TestGormGamificationListCSATStats(t *testing.T) {
	db := newGamificationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	agent1, agent2 := uint(1), uint(2)
	now := time.Now()
	csats := []models.CustomerSatisfaction{
		{TicketID: 21, CustomerID: 100, AgentID: &agent1, Rating: 5, Category: "overall", CreatedAt: now},
		{TicketID: 22, CustomerID: 101, AgentID: &agent1, Rating: 3, Category: "overall", CreatedAt: now},
		{TicketID: 23, CustomerID: 102, AgentID: &agent2, Rating: 4, Category: "overall", CreatedAt: now},
	}
	if err := db.Create(&csats).Error; err != nil {
		t.Fatalf("seed csats: %v", err)
	}

	stats, err := repo.ListCSATStats(ctx, "2020-01-01 00:00:00", "2030-01-01 00:00:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byAgent := map[uint]float64{}
	countByAgent := map[uint]int64{}
	for _, s := range stats {
		byAgent[s.AgentID] = s.Avg
		countByAgent[s.AgentID] = s.Count
	}
	if byAgent[1] != 4.0 || countByAgent[1] != 2 || byAgent[2] != 4.0 || countByAgent[2] != 1 {
		t.Fatalf("unexpected csat stats: %+v", stats)
	}

	scoped := platformauth.ContextWithScope(ctx, "", "ws-9")
	stats, err = repo.ListCSATStats(scoped, "2020-01-01 00:00:00", "2030-01-01 00:00:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("expected no scoped stats, got %+v", stats)
	}

	if err := db.Migrator().DropTable(&models.CustomerSatisfaction{}); err != nil {
		t.Fatalf("drop csats: %v", err)
	}
	if _, err := repo.ListCSATStats(ctx, "2020-01-01 00:00:00", "2030-01-01 00:00:00"); err == nil {
		t.Fatal("expected query error after dropping csats")
	}
}
