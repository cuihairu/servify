package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	analyticsapp "servify/apps/server/internal/modules/analytics/application"

	"github.com/sirupsen/logrus"
)

func newStatisticsTestService(t *testing.T) *StatisticsService {
	t.Helper()
	db := newServicesTestDB(t,
		&models.User{}, &models.Agent{}, &models.Ticket{}, &models.Session{},
		&models.Message{}, &models.CustomerSatisfaction{}, &models.DailyStats{},
	)
	return NewStatisticsService(db, logrus.New())
}

func TestStatisticsService_ModuleStats(t *testing.T) {
	svc := newStatisticsTestService(t)
	ctx := unitScopedContext("t1", "w1")

	dash, err := svc.GetDashboardStats(ctx)
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if dash == nil {
		t.Fatal("expected dashboard stats")
	}

	now := time.Now()
	stats, err := svc.GetTimeRangeStats(ctx, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("GetTimeRangeStats: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("expected time range rows")
	}

	if _, err := svc.GetAgentPerformanceStats(ctx, now.Add(-24*time.Hour), now, 5); err != nil {
		t.Fatalf("GetAgentPerformanceStats: %v", err)
	}
	if _, err := svc.GetTicketCategoryStats(ctx, now.Add(-24*time.Hour), now); err != nil {
		t.Fatalf("GetTicketCategoryStats: %v", err)
	}
	if _, err := svc.GetTicketPriorityStats(ctx, now.Add(-24*time.Hour), now); err != nil {
		t.Fatalf("GetTicketPriorityStats: %v", err)
	}
	if _, err := svc.GetCustomerSourceStats(ctx); err != nil {
		t.Fatalf("GetCustomerSourceStats: %v", err)
	}
	if err := svc.UpdateDailyStats(ctx, now); err != nil {
		t.Fatalf("UpdateDailyStats: %v", err)
	}
}

func TestStatisticsService_RemoteAssistStats(t *testing.T) {
	svc := newStatisticsTestService(t)
	ctx := unitScopedContext("t1", "w1")

	closedAt := time.Now().Add(-time.Hour)
	created := time.Now().Add(-2 * time.Hour)
	tickets := []models.Ticket{
		{Title: "ra-open", Source: "remote_assist", Status: "open", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created},
		{Title: "ra-resolved", Category: "remote-assist", Status: "resolved", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created, ResolvedAt: &closedAt},
		{Title: "ra-closed", Tags: "remote_assist", Status: "closed", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created, ClosedAt: &closedAt},
		{Title: "other", Status: "open", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created},
	}
	for i := range tickets {
		if err := svc.db.Create(&tickets[i]).Error; err != nil {
			t.Fatalf("seed ticket: %v", err)
		}
	}

	stats, err := svc.GetRemoteAssistTicketStats(ctx)
	if err != nil {
		t.Fatalf("GetRemoteAssistTicketStats: %v", err)
	}
	if stats.Total != 3 || stats.Open != 1 || stats.Resolved != 1 || stats.Closed != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ResolvedRate != 1.0/3.0 {
		t.Fatalf("unexpected resolved rate: %v", stats.ResolvedRate)
	}
	if stats.AvgCloseHours <= 0 {
		t.Fatalf("expected avg close hours > 0, got %v", stats.AvgCloseHours)
	}

	// unscoped context exercises the no-tenant/workspace branch
	if _, err := svc.GetRemoteAssistTicketStats(context.Background()); err != nil {
		t.Fatalf("unscoped GetRemoteAssistTicketStats: %v", err)
	}
}

func TestStatisticsService_IncrementCounters(t *testing.T) {
	svc := newStatisticsTestService(t)
	ctx := context.Background()

	svc.IncrementAIUsage(ctx)
	svc.IncrementKnowledgeProviderUsage(ctx)
	svc.IncrementWeKnoraUsage(ctx)
}

func TestStatisticsService_StartDailyStatsWorkerContext(t *testing.T) {
	svc := newStatisticsTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.StartDailyStatsWorkerContext(ctx, -time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop after context cancel")
	}
	go svc.StartDailyStatsWorker() // smoke: returns control immediately, runs in background
}

func TestStatisticsService_SetEventBus(t *testing.T) {
	svc := newStatisticsTestService(t)
	svc.SetEventBus(nil)
}

func TestStatisticsService_Mappers(t *testing.T) {
	if dashboardStatsFromDTO(nil) != nil {
		t.Fatal("nil dashboard should map to nil")
	}
	dash := dashboardStatsFromDTO(&analyticsapp.DashboardStats{TotalTickets: 5})
	if dash.TotalTickets != 5 {
		t.Fatalf("unexpected dashboard: %+v", dash)
	}
	tr := timeRangeStatsFromDTO([]analyticsapp.TimeRangeStats{{Date: "2024-01-01", Tickets: 2}})
	if len(tr) != 1 || tr[0].Tickets != 2 {
		t.Fatalf("unexpected time range: %+v", tr)
	}
	ap := agentPerformanceStatsFromDTO([]analyticsapp.AgentPerformanceStats{{AgentID: 3}})
	if len(ap) != 1 || ap[0].AgentID != 3 {
		t.Fatalf("unexpected agent perf: %+v", ap)
	}
	cs := categoryStatsFromDTO([]analyticsapp.CategoryStats{{Category: "c", Count: 1}})
	if len(cs) != 1 || cs[0].Category != "c" {
		t.Fatalf("unexpected category stats: %+v", cs)
	}
	if got := timeRangeStatsFromDTO(nil); len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
	if got := agentPerformanceStatsFromDTO(nil); len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
	if got := categoryStatsFromDTO(nil); len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}
