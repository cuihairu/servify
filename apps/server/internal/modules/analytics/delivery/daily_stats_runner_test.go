package delivery

// DailyStatsRunner 的单轮行为：UpdateDailyStats 结果透传（周期循环由
// app/worker 的 StatisticsWorker 驱动）。

import (
	"context"
	"errors"
	"testing"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	analyticscontract "servify/apps/server/internal/modules/analytics/contract"

	"github.com/sirupsen/logrus"
)

// runnerStubRepo 驱动 DailyStatsRunner：UpdateDailyStats 行为可注入。
type runnerStubRepo struct {
	updateErr error
}

func (r *runnerStubRepo) GetDashboardStats(ctx context.Context) (*analyticscontract.DashboardStats, error) {
	return &analyticscontract.DashboardStats{}, nil
}

func (r *runnerStubRepo) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.TimeRangeStats, error) {
	return nil, nil
}

func (r *runnerStubRepo) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticscontract.AgentPerformanceStats, error) {
	return nil, nil
}

func (r *runnerStubRepo) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	return nil, nil
}

func (r *runnerStubRepo) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	return nil, nil
}

func (r *runnerStubRepo) GetCustomerSourceStats(ctx context.Context) ([]analyticscontract.CategoryStats, error) {
	return nil, nil
}

func (r *runnerStubRepo) GetRemoteAssistTicketStats(ctx context.Context) (*analyticscontract.RemoteAssistTicketStats, error) {
	return &analyticscontract.RemoteAssistTicketStats{}, nil
}

func (r *runnerStubRepo) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return r.updateErr
}

func (r *runnerStubRepo) IncrementDailyStat(ctx context.Context, event analyticsapp.IncrementEvent) error {
	return nil
}

func TestDailyStatsRunner_RunUpdateSuccess(t *testing.T) {
	runner := NewDailyStatsRunner(analyticsapp.NewService(&runnerStubRepo{}), logrus.New())

	if err := runner.RunDailyStatsUpdate(context.Background(), time.Now()); err != nil {
		t.Fatalf("RunDailyStatsUpdate: %v", err)
	}
}

func TestDailyStatsRunner_RunUpdateErrorPassthrough(t *testing.T) {
	// UpdateDailyStats 失败原样透传，由 worker 侧 TrackJob 记 job failure。
	runner := NewDailyStatsRunner(analyticsapp.NewService(&runnerStubRepo{updateErr: errors.New("boom: stats")}), logrus.New())

	if err := runner.RunDailyStatsUpdate(context.Background(), time.Now()); err == nil || err.Error() != "boom: stats" {
		t.Fatalf("want passthrough error, got %v", err)
	}
}

func TestDailyStatsRunner_NilLoggerDefaults(t *testing.T) {
	// nil logger 取默认 logger（构造分支），单轮调用照常成功。
	runner := NewDailyStatsRunner(analyticsapp.NewService(&runnerStubRepo{}), nil)

	if err := runner.RunDailyStatsUpdate(context.Background(), time.Now()); err != nil {
		t.Fatalf("RunDailyStatsUpdate: %v", err)
	}
}
