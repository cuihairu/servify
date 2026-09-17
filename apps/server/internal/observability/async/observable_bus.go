package async

import (
	"context"

	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
)

// ObservableBus 包装 eventbus.Bus，把发布与订阅处理统一接入可观测中间件：
// 发布按 event_type/outcome 计数（eventbus_published_total），订阅 handler
// 经 WrapHandler 包装（handled/failed/duration/dead_letter）。入口装配处
// （cmd/server）用它替换原始 bus，一处接线覆盖所有订阅方。
type ObservableBus struct {
	inner    eventbus.Bus
	metrics  *BusMetrics
	recorder DeadLetterRecorder
	logger   *logrus.Logger
}

// NewObservableBus wraps the given bus with event bus observability.
func NewObservableBus(inner eventbus.Bus, m *BusMetrics, recorder DeadLetterRecorder, logger *logrus.Logger) *ObservableBus {
	return &ObservableBus{inner: inner, metrics: m, recorder: recorder, logger: logger}
}

// Subscribe registers the handler wrapped with BusMiddleware.
func (b *ObservableBus) Subscribe(eventName string, handler eventbus.Handler) {
	b.inner.Subscribe(eventName, WrapHandler(eventName, handler, b.metrics, b.recorder, b.logger))
}

// Publish delegates to the inner bus and records the publish outcome.
func (b *ObservableBus) Publish(ctx context.Context, event eventbus.Event) error {
	err := b.inner.Publish(ctx, event)
	if err != nil {
		b.metrics.RecordPublished(event.Name(), "error")
		return err
	}
	b.metrics.RecordPublished(event.Name(), "success")
	return nil
}
