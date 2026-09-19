package delivery

import (
	"context"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"

	"github.com/sirupsen/logrus"
)

// DailyStatsRunner 聚合每日统计：单轮口径为「补当日，非首轮补当日+昨日」
// （跨天边界漏算兜底；首轮/后续轮语义由 app/worker 的 StatisticsWorker
// 维护，周期循环也在 worker 侧）。
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

// RunDailyStatsUpdate 聚合指定日期的每日统计（单轮，循环由 worker 驱动）。
func (r *DailyStatsRunner) RunDailyStatsUpdate(ctx context.Context, day time.Time) error {
	return r.module.UpdateDailyStats(ctx, day)
}
