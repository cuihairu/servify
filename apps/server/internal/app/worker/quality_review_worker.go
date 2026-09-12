package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"

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
		initialDelay := jitter(w.interval, 0.1)
		if initialDelay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(initialDelay):
			}
		}
		run := func() {
			processed, err := w.service.RunScan(ctx)
			if err != nil && w.logger != nil {
				w.logger.WithError(err).Warn("quality-review worker: scan failed")
				return
			}
			if processed > 0 && w.logger != nil {
				w.logger.Debugf("quality-review worker: advanced %d sessions", processed)
			}
		}
		run()
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
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
