package delivery

// DailyStatsRunner 的 ticker 循环行为：ctx 取消退出、更新失败只记日志。

import (
	"context"
	"errors"
	"testing"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	analyticscontract "servify/apps/server/internal/modules/analytics/contract"

	"github.com/sirupsen/logrus"
)

// runnerStubRepo 驱动 DailyStatsRunner 循环：UpdateDailyStats 行为可注入。
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

func TestDailyStatsRunner_StopsOnCancel(t *testing.T) {
	runner := NewDailyStatsRunner(analyticsapp.NewService(&runnerStubRepo{}), logrus.New())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.StartDailyStatsWorkerContext(ctx, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestDailyStatsRunner_UpdateErrorsLogged(t *testing.T) {
	// daily stats updates fail; both error log branches in the ticker loop
	// must be exercised before cancellation
	runner := NewDailyStatsRunner(analyticsapp.NewService(&runnerStubRepo{updateErr: errors.New("boom: stats")}), logrus.New())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.StartDailyStatsWorkerContext(ctx, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestDailyStatsRunner_DefaultIntervalAndLogger(t *testing.T) {
	// interval <=0 取默认 1 小时：验证 initial run 执行后 ctx 取消即退出。
	runner := NewDailyStatsRunner(analyticsapp.NewService(&runnerStubRepo{}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.StartDailyStatsWorkerContext(ctx, 0)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // 让 initial run 先执行
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner with default interval did not return on cancel")
	}
}
