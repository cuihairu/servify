package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/modules/ticket/outbox"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

// TicketOutboxWorker 周期领取工单可靠事件 outbox 的到期行并投递到事件总线
// （退避重试由 outbox.ProcessDueEvents 控制；同 event_id 全局唯一去重保证
// 崩溃重试不重复发布）。
type TicketOutboxWorker struct {
	service  outbox.WorkerContract
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewTicketOutboxWorker(service outbox.WorkerContract, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &TicketOutboxWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *TicketOutboxWorker) Name() string { return "ticket-outbox" }

func (w *TicketOutboxWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *TicketOutboxWorker) Start() error {
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
			processed := w.service.ProcessDueEvents(ctx, time.Now())
			if processed > 0 && w.logger != nil {
				w.logger.Debugf("ticket-outbox worker: processed %d events", processed)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *TicketOutboxWorker) Stop(ctx context.Context) error {
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
