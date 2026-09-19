package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

type emailPollProcessor interface {
	PollOnce(ctx context.Context) (int, error)
}

// EmailPollWorker 周期轮询 IMAP 邮箱并把新邮件归并进会话（email.enabled 时注册）。
type EmailPollWorker struct {
	processor emailPollProcessor
	interval  time.Duration
	logger    *logrus.Logger
	metrics   *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewEmailPollWorker(processor emailPollProcessor, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = time.Minute
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &EmailPollWorker{
		processor: processor,
		interval:  interval,
		logger:    logger,
	}
}

func (w *EmailPollWorker) Name() string { return "email-poll" }

func (w *EmailPollWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *EmailPollWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.processor == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, func(ctx context.Context) error {
			produced, err := w.processor.PollOnce(ctx)
			if err != nil {
				return err
			}
			if produced > 0 {
				w.logger.Debugf("email-poll worker: ingested %d emails", produced)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *EmailPollWorker) Stop(ctx context.Context) error {
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
