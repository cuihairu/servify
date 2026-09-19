package delivery

import (
	"context"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"

	"github.com/sirupsen/logrus"
)

// DailyStatsRunner 周期聚合每日统计：启动即补当日，之后每轮补当日+昨日
// （跨天边界漏算兜底）。由 app/worker 的 StatisticsWorker 驱动。
type DailyStatsRunner struct {
	module *analyticsapp.Service
	logger *logrus.Logger
}

func NewDailyStatsRunner(module *analyticsapp.Service, logger *logrus.Logger) *DailyStatsRunner {
	if logger == nil {
		logger = logrus.New()
	}
	return &DailyStatsRunner{module: module, logger: logger}
}

// StartDailyStatsWorkerContext 阻塞运行（worker 侧由 go 起），ctx 取消时退出。
func (r *DailyStatsRunner) StartDailyStatsWorkerContext(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial run with context respect.
	if err := r.module.UpdateDailyStats(ctx, time.Now()); err != nil {
		r.logger.Errorf("Failed to update daily stats: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.module.UpdateDailyStats(ctx, time.Now()); err != nil {
				r.logger.Errorf("Failed to update daily stats: %v", err)
			}
			yesterday := time.Now().AddDate(0, 0, -1)
			if err := r.module.UpdateDailyStats(ctx, yesterday); err != nil {
				r.logger.Errorf("Failed to update yesterday stats: %v", err)
			}
		}
	}
}
