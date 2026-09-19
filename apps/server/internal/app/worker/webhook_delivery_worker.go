package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

type webhookDeliveryProcessor interface {
	ProcessDueDeliveries(ctx context.Context, now time.Time) int
}

// WebhookDeliveryWorker 周期领取到期投递并投递（退避重试由 webhook 服务控制）。
type WebhookDeliveryWorker struct {
	service  webhookDeliveryProcessor
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewWebhookDeliveryWorker(service webhookDeliveryProcessor, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &WebhookDeliveryWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *WebhookDeliveryWorker) Name() string { return "webhook-delivery" }

func (w *WebhookDeliveryWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *WebhookDeliveryWorker) Start() error {
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
			processed := w.service.ProcessDueDeliveries(ctx, time.Now())
			if processed > 0 && w.logger != nil {
				w.logger.Debugf("webhook-delivery worker: processed %d deliveries", processed)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *WebhookDeliveryWorker) Stop(ctx context.Context) error {
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
