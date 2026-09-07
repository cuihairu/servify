package delivery

import (
	"context"
	"testing"
	"time"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
)

// stubHandlerService pins the HandlerService contract so that accidental
// interface changes are caught at compile time.
type stubHandlerService struct{}

var _ HandlerService = (*stubHandlerService)(nil)

func (stubHandlerService) GetDashboardStats(ctx context.Context) (*analyticscontract.DashboardStats, error) {
	return &analyticscontract.DashboardStats{}, nil
}

func (stubHandlerService) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.TimeRangeStats, error) {
	return nil, nil
}

func (stubHandlerService) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticscontract.AgentPerformanceStats, error) {
	return nil, nil
}

func (stubHandlerService) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	return nil, nil
}

func (stubHandlerService) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	return nil, nil
}

func (stubHandlerService) GetCustomerSourceStats(ctx context.Context) ([]analyticscontract.CategoryStats, error) {
	return nil, nil
}

func (stubHandlerService) GetRemoteAssistTicketStats(ctx context.Context) (*analyticscontract.RemoteAssistTicketStats, error) {
	return &analyticscontract.RemoteAssistTicketStats{}, nil
}

func (stubHandlerService) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return nil
}

func TestHandlerServiceContract(t *testing.T) {
	var svc HandlerService = stubHandlerService{}
	stats, err := svc.GetDashboardStats(context.Background())
	if err != nil {
		t.Fatalf("GetDashboardStats() error = %v", err)
	}
	if stats == nil {
		t.Fatal("expected dashboard stats")
	}
	remote, err := svc.GetRemoteAssistTicketStats(context.Background())
	if err != nil {
		t.Fatalf("GetRemoteAssistTicketStats() error = %v", err)
	}
	if remote == nil {
		t.Fatal("expected remote assist stats")
	}
}
