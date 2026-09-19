package application

import (
	"context"
	"time"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	return s.repo.GetDashboardStats(ctx)
}

func (s *Service) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]TimeRangeStats, error) {
	return s.repo.GetTimeRangeStats(ctx, startDate, endDate)
}

func (s *Service) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]AgentPerformanceStats, error) {
	return s.repo.GetAgentPerformanceStats(ctx, startDate, endDate, limit)
}

func (s *Service) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]CategoryStats, error) {
	return s.repo.GetTicketCategoryStats(ctx, startDate, endDate)
}

func (s *Service) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]CategoryStats, error) {
	return s.repo.GetTicketPriorityStats(ctx, startDate, endDate)
}

func (s *Service) GetCustomerSourceStats(ctx context.Context) ([]CategoryStats, error) {
	return s.repo.GetCustomerSourceStats(ctx)
}

func (s *Service) GetRemoteAssistTicketStats(ctx context.Context) (*RemoteAssistTicketStats, error) {
	return s.repo.GetRemoteAssistTicketStats(ctx)
}

func (s *Service) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return s.repo.UpdateDailyStats(ctx, date)
}

func (s *Service) IncrementDailyStat(ctx context.Context, event IncrementEvent) error {
	if event.Date.IsZero() {
		event.Date = time.Now()
	}
	return s.repo.IncrementDailyStat(ctx, event)
}
