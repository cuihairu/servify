package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

type qualityScanProcessor interface {
	RunScan(ctx context.Context) (int, error)
}

// QualityReviewWorker 周期扫描已结束会话：规则质检 + 可选 LLM 打分，
// 失败重试与退避由 QualityService 自身记账。
type QualityReviewWorker struct {
	service  qualityScanProcessor
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewQualityReviewWorker(service qualityScanProcessor, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &QualityReviewWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *QualityReviewWorker) Name() string { return "quality-review-scan" }

func (w *QualityReviewWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *QualityReviewWorker) Start() error {
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
			processed, err := w.service.RunScan(ctx)
			if err != nil {
				return err
			}
			if processed > 0 && w.logger != nil {
				w.logger.Debugf("quality-review worker: advanced %d sessions", processed)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *QualityReviewWorker) Stop(ctx context.Context) error {
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
