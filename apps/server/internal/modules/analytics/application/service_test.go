package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubRepo struct {
	dashboard    *DashboardStats
	dashboardErr error

	timeRange    []TimeRangeStats
	timeRangeErr error

	agentPerf    []AgentPerformanceStats
	agentPerfErr error

	category    []CategoryStats
	categoryErr error

	priority    []CategoryStats
	priorityErr error

	source    []CategoryStats
	sourceErr error

	updateDate time.Time
	updateErr  error

	incrementErr    error
	last            IncrementEvent
	remoteAssist    *RemoteAssistTicketStats
	remoteAssistErr error
}

func (s *stubRepo) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	if s.dashboardErr != nil {
		return nil, s.dashboardErr
	}
	return s.dashboard, nil
}

func (s *stubRepo) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]TimeRangeStats, error) {
	if s.timeRangeErr != nil {
		return nil, s.timeRangeErr
	}
	return s.timeRange, nil
}

func (s *stubRepo) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]AgentPerformanceStats, error) {
	if s.agentPerfErr != nil {
		return nil, s.agentPerfErr
	}
	return s.agentPerf, nil
}

func (s *stubRepo) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]CategoryStats, error) {
	if s.categoryErr != nil {
		return nil, s.categoryErr
	}
	return s.category, nil
}

func (s *stubRepo) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]CategoryStats, error) {
	if s.priorityErr != nil {
		return nil, s.priorityErr
	}
	return s.priority, nil
}

func (s *stubRepo) GetCustomerSourceStats(ctx context.Context) ([]CategoryStats, error) {
	if s.sourceErr != nil {
		return nil, s.sourceErr
	}
	return s.source, nil
}

func (s *stubRepo) GetRemoteAssistTicketStats(ctx context.Context) (*RemoteAssistTicketStats, error) {
	if s.remoteAssistErr != nil {
		return nil, s.remoteAssistErr
	}
	return s.remoteAssist, nil
}

func (s *stubRepo) UpdateDailyStats(ctx context.Context, date time.Time) error {
	s.updateDate = date
	return s.updateErr
}

func (s *stubRepo) IncrementDailyStat(ctx context.Context, event IncrementEvent) error {
	s.last = event
	return s.incrementErr
}

func TestServiceGetDashboardStats(t *testing.T) {
	repo := &stubRepo{dashboard: &DashboardStats{TotalTickets: 3}}
	svc := NewService(repo)

	stats, err := svc.GetDashboardStats(context.Background())
	if err != nil {
		t.Fatalf("GetDashboardStats() error = %v", err)
	}
	if stats != repo.dashboard {
		t.Fatalf("GetDashboardStats() = %+v, want %+v", stats, repo.dashboard)
	}

	repo.dashboardErr = errors.New("dashboard boom")
	if _, err := svc.GetDashboardStats(context.Background()); err == nil {
		t.Fatal("expected error from GetDashboardStats")
	}
}

func TestServiceGetTimeRangeStats(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	repo := &stubRepo{timeRange: []TimeRangeStats{{Date: "2026-04-01"}, {Date: "2026-04-02"}}}
	svc := NewService(repo)

	items, err := svc.GetTimeRangeStats(context.Background(), start, end)
	if err != nil {
		t.Fatalf("GetTimeRangeStats() error = %v", err)
	}
	if len(items) != 2 || items[1].Date != "2026-04-02" {
		t.Fatalf("GetTimeRangeStats() = %+v", items)
	}

	repo.timeRangeErr = errors.New("time range boom")
	if _, err := svc.GetTimeRangeStats(context.Background(), start, end); err == nil {
		t.Fatal("expected error from GetTimeRangeStats")
	}
}

func TestServiceGetAgentPerformanceStats(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	repo := &stubRepo{agentPerf: []AgentPerformanceStats{{AgentID: 7, AgentName: "A"}}}
	svc := NewService(repo)

	items, err := svc.GetAgentPerformanceStats(context.Background(), start, end, 5)
	if err != nil {
		t.Fatalf("GetAgentPerformanceStats() error = %v", err)
	}
	if len(items) != 1 || items[0].AgentID != 7 {
		t.Fatalf("GetAgentPerformanceStats() = %+v", items)
	}

	repo.agentPerfErr = errors.New("agent perf boom")
	if _, err := svc.GetAgentPerformanceStats(context.Background(), start, end, 5); err == nil {
		t.Fatal("expected error from GetAgentPerformanceStats")
	}
}

func TestServiceGetTicketCategoryStats(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	repo := &stubRepo{category: []CategoryStats{{Category: "billing", Count: 2}}}
	svc := NewService(repo)

	items, err := svc.GetTicketCategoryStats(context.Background(), start, end)
	if err != nil {
		t.Fatalf("GetTicketCategoryStats() error = %v", err)
	}
	if len(items) != 1 || items[0].Category != "billing" {
		t.Fatalf("GetTicketCategoryStats() = %+v", items)
	}

	repo.categoryErr = errors.New("category boom")
	if _, err := svc.GetTicketCategoryStats(context.Background(), start, end); err == nil {
		t.Fatal("expected error from GetTicketCategoryStats")
	}
}

func TestServiceGetTicketPriorityStats(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	repo := &stubRepo{priority: []CategoryStats{{Category: "high", Count: 1}}}
	svc := NewService(repo)

	items, err := svc.GetTicketPriorityStats(context.Background(), start, end)
	if err != nil {
		t.Fatalf("GetTicketPriorityStats() error = %v", err)
	}
	if len(items) != 1 || items[0].Category != "high" {
		t.Fatalf("GetTicketPriorityStats() = %+v", items)
	}

	repo.priorityErr = errors.New("priority boom")
	if _, err := svc.GetTicketPriorityStats(context.Background(), start, end); err == nil {
		t.Fatal("expected error from GetTicketPriorityStats")
	}
}

func TestServiceGetCustomerSourceStats(t *testing.T) {
	repo := &stubRepo{source: []CategoryStats{{Category: "web", Count: 4}}}
	svc := NewService(repo)

	items, err := svc.GetCustomerSourceStats(context.Background())
	if err != nil {
		t.Fatalf("GetCustomerSourceStats() error = %v", err)
	}
	if len(items) != 1 || items[0].Category != "web" {
		t.Fatalf("GetCustomerSourceStats() = %+v", items)
	}

	repo.sourceErr = errors.New("source boom")
	if _, err := svc.GetCustomerSourceStats(context.Background()); err == nil {
		t.Fatal("expected error from GetCustomerSourceStats")
	}
}

func TestServiceUpdateDailyStats(t *testing.T) {
	day := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	repo := &stubRepo{}
	svc := NewService(repo)

	if err := svc.UpdateDailyStats(context.Background(), day); err != nil {
		t.Fatalf("UpdateDailyStats() error = %v", err)
	}
	if !repo.updateDate.Equal(day) {
		t.Fatalf("UpdateDailyStats() date = %v, want %v", repo.updateDate, day)
	}

	repo.updateErr = errors.New("update boom")
	if err := svc.UpdateDailyStats(context.Background(), day); err == nil {
		t.Fatal("expected error from UpdateDailyStats")
	}
}

func TestIncrementDailyStatDefaultsDate(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	if err := svc.IncrementDailyStat(context.Background(), IncrementEvent{Kind: IncrementTickets}); err != nil {
		t.Fatalf("IncrementDailyStat() error = %v", err)
	}
	if repo.last.Date.IsZero() {
		t.Fatal("expected date to be defaulted")
	}
	if repo.last.Kind != IncrementTickets {
		t.Fatalf("unexpected kind: %s", repo.last.Kind)
	}
}

func TestIncrementDailyStatKeepsExistingDate(t *testing.T) {
	day := time.Date(2026, 4, 8, 10, 30, 0, 0, time.UTC)
	repo := &stubRepo{}
	svc := NewService(repo)

	if err := svc.IncrementDailyStat(context.Background(), IncrementEvent{Date: day, Kind: IncrementMessages}); err != nil {
		t.Fatalf("IncrementDailyStat() error = %v", err)
	}
	if !repo.last.Date.Equal(day) {
		t.Fatalf("expected date %v to be preserved, got %v", day, repo.last.Date)
	}
	if repo.last.Kind != IncrementMessages {
		t.Fatalf("unexpected kind: %s", repo.last.Kind)
	}
}

func TestIncrementDailyStatPropagatesError(t *testing.T) {
	repo := &stubRepo{incrementErr: errors.New("increment boom")}
	svc := NewService(repo)

	if err := svc.IncrementDailyStat(context.Background(), IncrementEvent{Kind: IncrementSessions}); err == nil {
		t.Fatal("expected error from IncrementDailyStat")
	}
}

func TestServiceGetRemoteAssistTicketStats(t *testing.T) {
	want := &RemoteAssistTicketStats{Total: 3, Open: 1, ResolvedRate: 0.5}
	repo := &stubRepo{remoteAssist: want}
	svc := NewService(repo)

	got, err := svc.GetRemoteAssistTicketStats(context.Background())
	if err != nil {
		t.Fatalf("GetRemoteAssistTicketStats() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetRemoteAssistTicketStats() = %+v, want %+v", got, want)
	}

	repo.remoteAssistErr = errors.New("remote assist boom")
	if _, err := svc.GetRemoteAssistTicketStats(context.Background()); err == nil {
		t.Fatal("expected error from GetRemoteAssistTicketStats")
	}
}
