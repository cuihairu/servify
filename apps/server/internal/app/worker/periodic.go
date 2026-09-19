package worker

import (
	"context"
	"time"

	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

// periodicJob 统一后台 worker 的「jitter 初始延迟 → 首跑 → ticker 循环」
// 执行模板，并为每轮 job 经 async.TrackJob 记录
// worker_jobs_total{worker_name, outcome} 与 worker_job_duration_seconds
// （job 级埋点，known-gaps 尾项接线）。
type periodicJob struct {
	name     string
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics
	run      func(ctx context.Context) error
}

func newPeriodicJob(name string, interval time.Duration, logger *logrus.Logger, metrics *async.WorkerMetrics, run func(ctx context.Context) error) *periodicJob {
	return &periodicJob{
		name:     name,
		interval: interval,
		logger:   logger,
		metrics:  metrics,
		run:      run,
	}
}

// loop 阻塞运行直至 ctx 取消（worker 侧由 go 起驱动）。
func (p *periodicJob) loop(ctx context.Context) {
	// Add initial jitter to stagger workers on restart.
	initialDelay := jitter(p.interval, 0.1)
	if initialDelay > 0 {
		if p.logger != nil {
			p.logger.Debugf("%s worker: initial jitter delay %v", p.name, initialDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialDelay):
		}
	}
	p.runOnce(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx)
		}
	}
}

// runOnce 执行单轮 job：TrackJob 记计数/时长/活跃 gauge，单轮失败只告警
// 不终止循环（DB 抖动等下一轮重试）。
func (p *periodicJob) runOnce(ctx context.Context) {
	err := async.TrackJob(p.name, p.metrics, func() error { return p.run(ctx) })
	if err != nil && p.logger != nil {
		p.logger.WithError(err).Warnf("%s worker: job failed", p.name)
	}
}

// jobMetricsAware 由需要 job 级埋点的 worker 实现；RegisterDefaultWorkers
// 注册完成后统一注入共享 WorkerMetrics（同 ObservableWorker 包装一处收口），
// 直构 worker（测试）不注入则 TrackJob 直通。
type jobMetricsAware interface {
	setJobMetrics(*async.WorkerMetrics)
}
