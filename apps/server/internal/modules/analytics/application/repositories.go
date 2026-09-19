package application

import (
	"context"
	"time"
)

type Repository interface {
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]TimeRangeStats, error)
	GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]AgentPerformanceStats, error)
	GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]CategoryStats, error)
	GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]CategoryStats, error)
	GetCustomerSourceStats(ctx context.Context) ([]CategoryStats, error)
	GetRemoteAssistTicketStats(ctx context.Context) (*RemoteAssistTicketStats, error)
	UpdateDailyStats(ctx context.Context, date time.Time) error
	IncrementDailyStat(ctx context.Context, event IncrementEvent) error
}
