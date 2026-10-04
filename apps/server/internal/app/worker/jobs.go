package worker

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	automationapp "servify/apps/server/internal/modules/automation/application"
	customerapp "servify/apps/server/internal/modules/customer/application"
	customerinfra "servify/apps/server/internal/modules/customer/infra"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	qualityapp "servify/apps/server/internal/modules/quality/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"
	slapp "servify/apps/server/internal/modules/sla/application"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	"servify/apps/server/internal/observability/async"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	auditplatform "servify/apps/server/internal/platform/audit"
	"servify/apps/server/internal/platform/usersecurity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// jitter returns a random duration in [0, fraction*base).
// Returns 0 if base is negative or fraction <= 0.
func jitter(base time.Duration, fraction float64) time.Duration {
	if fraction <= 0 || base <= 0 {
		return 0
	}
	return time.Duration(float64(base) * fraction * rand.Float64())
}

type statisticsService interface {
	// RunDailyStatsUpdate 聚合指定日期的每日统计（单轮，循环由 worker 驱动）。
	RunDailyStatsUpdate(ctx context.Context, day time.Time) error
}

// StatisticsWorker runs periodic daily-stats aggregation.
type StatisticsWorker struct {
	service  statisticsService
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

type RuntimeWorkerDependencies interface {
	StatisticsServiceForWorker() *analyticsdelivery.DailyStatsRunner
	SLAServiceForWorker() slapp.SLAMonitor
	WebhookDeliveryForWorker() webhookapp.Processor
	EmailPollAdapterForWorker() emaildelivery.PollProcessor
	QualityScanForWorker() *qualityapp.QualityService
	WaitingQueueForWorker() *routingdelivery.HandlerServiceAdapter
	SurveysForWorker() satisfapp.SurveyEmailProcessor
	AutomationTimersForWorker() automationapp.TimerProcessor
}

// RegisterDefaultWorkers registers the default background workers for the server runtime.
func RegisterDefaultWorkers(app *bootstrap.App, cfg *config.Config, db *gorm.DB, deps RuntimeWorkerDependencies) {
	if app == nil || cfg == nil || deps == nil {
		return
	}

	app.RegisterWorker(NewStatisticsWorker(deps.StatisticsServiceForWorker(), time.Hour, app.Logger))
	app.RegisterWorker(NewSLAMonitorWorker(deps.SLAServiceForWorker(), 5*time.Minute, app.Logger))

	if processor := deps.WebhookDeliveryForWorker(); processor != nil {
		app.RegisterWorker(NewWebhookDeliveryWorker(processor, 30*time.Second, app.Logger))
	}

	if processor := deps.EmailPollAdapterForWorker(); processor != nil {
		interval := time.Duration(cfg.Email.PollIntervalSeconds) * time.Second
		app.RegisterWorker(NewEmailPollWorker(processor, interval, app.Logger))
	}

	if scanner := deps.QualityScanForWorker(); scanner != nil {
		interval := time.Duration(cfg.Quality.ScanIntervalSeconds) * time.Second
		app.RegisterWorker(NewQualityReviewWorker(scanner, interval, app.Logger))
	}

	if dispatcher := deps.WaitingQueueForWorker(); dispatcher != nil {
		interval := time.Duration(cfg.Routing.DispatchIntervalSeconds) * time.Second
		app.RegisterWorker(NewWaitingQueueWorker(dispatcher, interval, app.Logger))
	}

	if surveyService := deps.SurveysForWorker(); surveyService != nil {
		app.RegisterWorker(NewSurveyEmailWorker(surveyService, app.Logger))
	}

	if processor := deps.AutomationTimersForWorker(); processor != nil {
		interval := time.Duration(cfg.Automation.TimerScanIntervalSeconds) * time.Second
		app.RegisterWorker(NewAutomationTimerWorker(processor, interval, app.Logger))
	}

	if cfg.Security.Audit.Enabled && db != nil {
		retention := auditplatform.NewGormRetentionService(db, cfg.Security.Audit.Retention, cfg.Security.Audit.CleanupBatchSize)
		// 冷热分层（T3）：配置归档目录时删除前先落 gzip JSON 快照冷层，
		// 归档失败即中断本轮清理（不产生未归档删除）。
		if dir := strings.TrimSpace(cfg.Security.Audit.ArchiveDir); dir != "" {
			if writer, err := auditplatform.NewFileArchiveWriter(dir); err != nil {
				app.Logger.Warnf("audit archive disabled: %v", err)
			} else {
				retention = retention.WithArchive(writer)
			}
		}
		app.RegisterWorker(NewAuditCleanupWorker(
			retention,
			cfg.Security.Audit.CleanupInterval,
			app.Logger,
		))
	}
	if cfg.Security.TokenRevocation.Enabled && db != nil {
		app.RegisterWorker(NewRevokedTokenCleanupWorker(
			usersecurity.NewGormRevokedTokenRetentionService(db, cfg.Security.TokenRevocation.CleanupBatchSize),
			cfg.Security.TokenRevocation.CleanupInterval,
			app.Logger,
		))
	}
	// 客户数据保留清理（B2-2b）：privacy.retention_days > 0 时按周期擦除
	// 已结束会话的消息内容与已关闭工单的内容（默认关闭保持既有行为）。
	if cfg.Privacy.RetentionDays > 0 && db != nil {
		retention := customerapp.NewRetentionService(
			customerinfra.NewGormDataBoundaryRepository(db),
			cfg.Privacy.RetentionDays,
		)
		app.RegisterWorker(NewRetentionCleanupWorker(retention, cfg.Privacy.CleanupInterval, app.Logger))
	}
	// 后台 worker 观测接线：全部注册完成后统一注入 job 级 metrics 并包装，
	// worker_active_jobs{worker_name}（包装层）与 worker_jobs_total /
	// worker_job_duration_seconds（periodicJob 每轮 TrackJob）。
	// collector 挂进程级 registry，同进程内只注册一次（测试会多次装配）。
	for i := range app.Workers {
		if setter, ok := app.Workers[i].(jobMetricsAware); ok {
			setter.setJobMetrics(sharedWorkerMetrics())
		}
		app.Workers[i] = async.NewObservableWorker(app.Workers[i], sharedWorkerMetrics())
	}
}

var (
	workerMetricsOnce sync.Once
	workerMetrics     *async.WorkerMetrics
)

func sharedWorkerMetrics() *async.WorkerMetrics {
	workerMetricsOnce.Do(func() {
		workerMetrics = async.NewWorkerMetrics(svcmetrics.DefaultRegistry)
	})
	return workerMetrics
}

func NewStatisticsWorker(service statisticsService, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = time.Hour
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &StatisticsWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *StatisticsWorker) Name() string { return "statistics-daily-stats" }

func (w *StatisticsWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *StatisticsWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.service == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	// 首轮只补当日；后续轮补当日+昨日（跨天边界漏算兜底）。
	firstRun := true
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, func(ctx context.Context) error {
			day := time.Now()
			if firstRun {
				firstRun = false
				return w.service.RunDailyStatsUpdate(ctx, day)
			}
			// 两步独立执行（任一失败整轮记 failure，互不阻断）。
			return errors.Join(
				w.service.RunDailyStatsUpdate(ctx, day),
				w.service.RunDailyStatsUpdate(ctx, day.AddDate(0, 0, -1)),
			)
		}).loop(ctx)
	}()
	return nil
}

func (w *StatisticsWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type slaMonitorService interface {
	// RunMonitorOnce 执行一轮 SLA 违约扫描（单轮，循环由 worker 驱动）。
	RunMonitorOnce(ctx context.Context) error
}

// SLAMonitorWorker runs periodic SLA violation scanning.
type SLAMonitorWorker struct {
	service  slaMonitorService
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewSLAMonitorWorker(service slaMonitorService, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &SLAMonitorWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *SLAMonitorWorker) Name() string { return "sla-monitor" }

func (w *SLAMonitorWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *SLAMonitorWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.service == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, w.service.RunMonitorOnce).loop(ctx)
	}()
	return nil
}

func (w *SLAMonitorWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type auditRetentionService interface {
	Cleanup(context.Context, time.Time) (int64, error)
}

// AuditCleanupWorker periodically deletes expired audit logs.
type AuditCleanupWorker struct {
	service  auditRetentionService
	interval time.Duration
	logger   *logrus.Logger
	now      func() time.Time
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewAuditCleanupWorker(service auditRetentionService, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &AuditCleanupWorker{
		service:  service,
		interval: interval,
		logger:   logger,
		now:      time.Now,
	}
}

func (w *AuditCleanupWorker) Name() string { return "audit-retention-cleanup" }

func (w *AuditCleanupWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *AuditCleanupWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.service == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, func(ctx context.Context) error {
			deleted, err := w.service.Cleanup(ctx, w.now().UTC())
			if err != nil {
				return err
			}
			if deleted > 0 && w.logger != nil {
				w.logger.Infof("audit cleanup worker: deleted %d expired audit logs", deleted)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *AuditCleanupWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type revokedTokenRetentionService interface {
	Cleanup(context.Context, time.Time) (int64, error)
}

// RevokedTokenCleanupWorker periodically deletes expired revoked-token entries.
type RevokedTokenCleanupWorker struct {
	service  revokedTokenRetentionService
	interval time.Duration
	logger   *logrus.Logger
	now      func() time.Time
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRevokedTokenCleanupWorker(service revokedTokenRetentionService, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &RevokedTokenCleanupWorker{
		service:  service,
		interval: interval,
		logger:   logger,
		now:      time.Now,
	}
}

func (w *RevokedTokenCleanupWorker) Name() string { return "revoked-token-cleanup" }

func (w *RevokedTokenCleanupWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *RevokedTokenCleanupWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.service == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, func(ctx context.Context) error {
			deleted, err := w.service.Cleanup(ctx, w.now().UTC())
			if err != nil {
				return err
			}
			if deleted > 0 && w.logger != nil {
				w.logger.Infof("revoked-token cleanup worker: deleted %d expired revoked tokens", deleted)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *RevokedTokenCleanupWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type customerRetentionService interface {
	// ScrubExpiredContent 执行一轮过期客户内容清理（单轮，循环由 worker 驱动）。
	ScrubExpiredContent(ctx context.Context) (*customerapp.RetentionResult, error)
}

// RetentionCleanupWorker 周期执行过期客户内容擦除
// （privacy.retention_days，V1.0 B2-2b）。
type RetentionCleanupWorker struct {
	service  customerRetentionService
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRetentionCleanupWorker(service customerRetentionService, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &RetentionCleanupWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *RetentionCleanupWorker) Name() string { return "customer-retention-cleanup" }

func (w *RetentionCleanupWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *RetentionCleanupWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.service == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, func(ctx context.Context) error {
			result, err := w.service.ScrubExpiredContent(ctx)
			if err != nil {
				return err
			}
			if result != nil && w.logger != nil &&
				(result.MessagesScrubbed > 0 || result.TicketsScrubbed > 0 || result.CommentsScrubbed > 0 || result.FilesDeleted > 0) {
				w.logger.Infof("customer retention worker: scrubbed %d messages / %d tickets / %d comments, deleted %d files (retention=%dd)",
					result.MessagesScrubbed, result.TicketsScrubbed, result.CommentsScrubbed, result.FilesDeleted, result.RetentionDays)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *RetentionCleanupWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
