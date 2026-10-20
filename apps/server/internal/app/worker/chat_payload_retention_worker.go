package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
)

type chatPayloadPurger interface {
	Purge(ctx context.Context, now time.Time) (int, error)
}

// ChatPayloadRetentionWorker 周期清除过期的聊天原始报文留存副本
// （契约：本地加密存储最长保留 30 天；删除失败的对象留下侧车下轮重试）。
type ChatPayloadRetentionWorker struct {
	store    chatPayloadPurger
	interval time.Duration
	logger   *logrus.Logger
	metrics  *async.WorkerMetrics

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewChatPayloadRetentionWorker(store chatPayloadPurger, interval time.Duration, logger *logrus.Logger) bootstrap.Worker {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &ChatPayloadRetentionWorker{
		store:    store,
		interval: interval,
		logger:   logger,
	}
}

func (w *ChatPayloadRetentionWorker) Name() string { return "chat-payload-retention" }

func (w *ChatPayloadRetentionWorker) setJobMetrics(m *async.WorkerMetrics) { w.metrics = m }

func (w *ChatPayloadRetentionWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.store == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	go func() {
		defer close(done)
		newPeriodicJob(w.Name(), w.interval, w.logger, w.metrics, func(ctx context.Context) error {
			removed, err := w.store.Purge(ctx, time.Now())
			if err != nil {
				return err
			}
			if removed > 0 && w.logger != nil {
				w.logger.Debugf("chat-payload-retention worker: removed %d expired objects", removed)
			}
			return nil
		}).loop(ctx)
	}()
	return nil
}

func (w *ChatPayloadRetentionWorker) Stop(ctx context.Context) error {
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
