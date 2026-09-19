package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	automationapp "servify/apps/server/internal/modules/automation/application"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

// AutomationTimerWorker 周期扫描到期的 delay 执行单并执行
// （抢占由 CompleteTimer 的乐观翻转保证，多实例下恰好一次；
// 失败不重试，只写 last_error 与 failed 审计）。
type AutomationTimerWorker struct {
	service  automationapp.TimerProcessor
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewAutomationTimerWorker(service automationapp.TimerProcessor, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &AutomationTimerWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *AutomationTimerWorker) Name() string { return "automation-timer" }

func (w *AutomationTimerWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *AutomationTimerWorker) Start() error {
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
			processed := w.service.ProcessDueTimers(ctx, time.Now())
			if processed > 0 && w.logger != nil {
				w.logger.Debugf("automation-timer worker: processed %d timers", processed)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *AutomationTimerWorker) Stop(ctx context.Context) error {
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
