package delivery_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	gamificationapp "servify/apps/server/internal/modules/gamification/application"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 与 integration 标签下的测试助手同名冲突规避：这里是无标签的单测专用内存库。
var gamificationUnitDBSeq atomic.Uint64

func newGamificationHandlerUnitDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:gamifunit_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(gamificationUnitDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
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

// adapterStubRepo 直接驱动 application.Service，用于校验 adapter 的契约映射。
type adapterStubRepo struct {
	profiles []gamificationapp.AgentProfile
	resolved []gamificationapp.AgentResolvedCount
	csats    []gamificationapp.AgentCSAT
}

func (r *adapterStubRepo) ListAgentProfiles(ctx context.Context, department string) ([]gamificationapp.AgentProfile, error) {
	return r.profiles, nil
}

func (r *adapterStubRepo) ListResolvedCounts(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentResolvedCount, error) {
	return r.resolved, nil
}

func (r *adapterStubRepo) ListCSATStats(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentCSAT, error) {
	return r.csats, nil
}

func TestHandlerAdapterUnitServiceConstruction(t *testing.T) {
	adapter := gamificationdelivery.NewHandlerService(newGamificationHandlerUnitDB(t))
	if adapter == nil {
		t.Fatal("expected adapter instance")
	}
	var _ gamificationdelivery.HandlerService = adapter

	if gamificationdelivery.NewHandlerServiceAdapter(gamificationapp.NewService(&adapterStubRepo{})) == nil {
		t.Fatal("expected adapter instance from service")
	}
}

func TestHandlerAdapterUnitGetLeaderboardValidation(t *testing.T) {
	adapter := gamificationdelivery.NewHandlerServiceAdapter(gamificationapp.NewService(&adapterStubRepo{}))
	ctx := context.Background()

	// req 为 nil -> application 层校验失败
	if _, err := adapter.GetLeaderboard(ctx, nil); err == nil || err.Error() != "start_date and end_date required" {
		t.Fatalf("expected validation error, got %v", err)
	}

	// end < start -> 校验失败
	if _, err := adapter.GetLeaderboard(ctx, &gamificationdelivery.LeaderboardRequest{
		StartDate: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil || err.Error() != "end_date must be after start_date" {
		t.Fatalf("expected range error, got %v", err)
	}
}

func TestHandlerAdapterUnitGetLeaderboardMapsEntries(t *testing.T) {
	repo := &adapterStubRepo{
		profiles: []gamificationapp.AgentProfile{
			{UserID: 5, Username: "a5", Name: "Five", Department: "support", AvgResponseTime: 0},
		},
		resolved: []gamificationapp.AgentResolvedCount{{AgentID: 5, Count: 7}},
		csats:    []gamificationapp.AgentCSAT{{AgentID: 5, Avg: 4.5, Count: 4}},
	}
	adapter := gamificationdelivery.NewHandlerServiceAdapter(gamificationapp.NewService(repo))

	resp, err := adapter.GetLeaderboard(context.Background(), &gamificationdelivery.LeaderboardRequest{
		StartDate:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Limit:      0,
		Department: " support ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StartDate != "2026-05-01" || resp.EndDate != "2026-05-31" || resp.Limit != 10 {
		t.Fatalf("unexpected envelope: %+v", resp)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %+v", resp.Entries)
	}
	entry := resp.Entries[0]
	if entry.AgentID != 5 || entry.Username != "a5" || entry.ResolvedTickets != 7 || entry.CSATAvg != 4.5 || entry.CSATCount != 4 {
		t.Fatalf("unexpected entry mapping: %+v", entry)
	}
	if entry.Rank != 1 || len(entry.Badges) == 0 {
		t.Fatalf("expected rank and badges, got %+v", entry)
	}
	for _, badge := range entry.Badges {
		if badge.ID == "" || badge.Name == "" || badge.Description == "" {
			t.Fatalf("badge not mapped: %+v", badge)
		}
	}
}

func TestHandlerAdapterUnitGetLeaderboardRealDB(t *testing.T) {
	db := newGamificationHandlerUnitDB(t)
	now := time.Now()
	if err := db.Create(&models.User{ID: 8, Username: "real-agent", Email: "real@example.com", Name: "Real", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 8, Department: "support", AvgResponseTime: 15, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	agent := uint(8)
	resolvedAt := now
	if err := db.Create(&models.Ticket{ID: 31, Title: "done", CustomerID: 1, AgentID: &agent, Status: "resolved", ResolvedAt: &resolvedAt, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Create(&models.CustomerSatisfaction{TicketID: 31, CustomerID: 1, AgentID: &agent, Rating: 5, Category: "overall", CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed csat: %v", err)
	}

	adapter := gamificationdelivery.NewHandlerService(db)
	resp, err := adapter.GetLeaderboard(context.Background(), &gamificationdelivery.LeaderboardRequest{
		StartDate: now.AddDate(0, 0, -1),
		EndDate:   now.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].AgentID != 8 || resp.Entries[0].ResolvedTickets != 1 {
		t.Fatalf("unexpected entries: %+v", resp.Entries)
	}
}

// adapterErrRepo 用于验证 service 错误直接透传。
type adapterErrRepo struct{}

func (adapterErrRepo) ListAgentProfiles(ctx context.Context, department string) ([]gamificationapp.AgentProfile, error) {
	return nil, errors.New("profiles exploded")
}

func (adapterErrRepo) ListResolvedCounts(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentResolvedCount, error) {
	return nil, nil
}

func (adapterErrRepo) ListCSATStats(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentCSAT, error) {
	return nil, nil
}

func TestHandlerAdapterUnitGetLeaderboardPropagatesError(t *testing.T) {
	adapter := gamificationdelivery.NewHandlerServiceAdapter(gamificationapp.NewService(adapterErrRepo{}))
	_, err := adapter.GetLeaderboard(context.Background(), &gamificationdelivery.LeaderboardRequest{
		StartDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
	})
	if err == nil || err.Error() != "profiles exploded" {
		t.Fatalf("expected propagated error, got %v", err)
	}
}
