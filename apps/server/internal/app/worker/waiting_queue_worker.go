package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"

	"github.com/sirupsen/logrus"
)

// WaitingQueueWorker 周期性 claim-then-process 分派等待队列，
// 补上「ProcessWaitingQueue 仅人工 HTTP 触发」的运营缺口。
type WaitingQueueWorker struct {
	service  *routingdelivery.HandlerServiceAdapter
	interval time.Duration
	logger   *logrus.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewWaitingQueueWorker(service *routingdelivery.HandlerServiceAdapter, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &WaitingQueueWorker{
		service:  service,
		interval: interval,
		logger:   logger,
	}
}

func (w *WaitingQueueWorker) Name() string { return "waiting-queue-dispatch" }

func (w *WaitingQueueWorker) Start() error {
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
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processed, err := w.service.ProcessWaitingQueue(ctx)
				if err != nil {
					// 单轮失败不终止 worker（DB 抖动等），下一轮重试
					if w.logger != nil {
						w.logger.WithError(err).Warn("waiting-queue worker: dispatch failed")
					}
					continue
				}
				if processed > 0 && w.logger != nil {
					w.logger.Infof("waiting-queue worker: dispatched %d waiting sessions", processed)
				}
			}
		}
	}()
	return nil
}

func (w *WaitingQueueWorker) Stop(ctx context.Context) error {
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
