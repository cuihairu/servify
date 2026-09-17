// Package async provides observability for asynchronous processing in Servify,
// including event bus middleware, worker tracking, and dead letter recording.
package async

import (
	"context"
	"time"

	"servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/observability/telemetry"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// BusMetrics holds Prometheus collectors for event bus observability.
type BusMetrics struct {
	published  *prometheus.CounterVec
	handled    *prometheus.CounterVec
	failed     *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	deadLetter *prometheus.CounterVec
}

// NewBusMetrics creates and registers event bus metric collectors.
func NewBusMetrics(reg *metrics.Registry) *BusMetrics {
	m := &BusMetrics{
		published: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricEventBusPublished,
			Help: "Total number of events published.",
		}, []string{telemetry.LabelEventType, telemetry.LabelOutcome}),
		handled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricEventBusHandled,
			Help: "Total number of events successfully handled.",
		}, []string{telemetry.LabelEventType}),
		failed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricEventBusFailed,
			Help: "Total number of event handler failures.",
		}, []string{telemetry.LabelEventType}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    telemetry.MetricEventBusDuration,
			Help:    "Event handler execution duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{telemetry.LabelEventType}),
		deadLetter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricEventBusDeadLetter,
			Help: "Total number of events dead-lettered.",
		}, []string{telemetry.LabelEventType}),
	}
	reg.MustRegister(m.published, m.handled, m.failed, m.duration, m.deadLetter)
	return m
}

// RecordPublished increments the published counter.
func (m *BusMetrics) RecordPublished(eventType, outcome string) {
	if m != nil && m.published != nil {
		m.published.WithLabelValues(eventType, outcome).Inc()
	}
}

// RecordDeadLetter increments the dead letter counter.
func (m *BusMetrics) RecordDeadLetter(eventType string) {
	if m != nil && m.deadLetter != nil {
		m.deadLetter.WithLabelValues(eventType).Inc()
	}
}

// BusMiddleware wraps an eventbus.Handler to record timing, success/failure,
// and dead-lettering. It implements the eventbus.Handler interface.
type BusMiddleware struct {
	inner    eventbus.Handler
	event    string
	metrics  *BusMetrics
	recorder DeadLetterRecorder
	logger   *logrus.Logger
}

// WrapHandler returns a new BusMiddleware that decorates the given handler.
func WrapHandler(eventName string, handler eventbus.Handler, busMetrics *BusMetrics, dlr DeadLetterRecorder, logger *logrus.Logger) eventbus.Handler {
	return &BusMiddleware{
		inner:    handler,
		event:    eventName,
		metrics:  busMetrics,
		recorder: dlr,
		logger:   logger,
	}
}

// Handle implements eventbus.Handler with observability.
func (m *BusMiddleware) Handle(ctx context.Context, event eventbus.Event) error {
	start := time.Now()

	err := m.inner.Handle(ctx, event)

	duration := time.Since(start).Seconds()
	eventType := event.Name()

	if m.metrics != nil {
		m.metrics.duration.WithLabelValues(eventType).Observe(duration)
	}

	if err != nil {
		if m.metrics != nil {
			m.metrics.failed.WithLabelValues(eventType).Inc()
		}
		if m.recorder != nil {
			dlErr := m.recorder.Record(ctx, DeadLetterEntry{
				EventID:    event.ID(),
				EventType:  eventType,
				Error:      err.Error(),
				OccurredAt: time.Now(),
				Retries:    0,
			})
			if dlErr != nil && m.logger != nil {
				m.logger.WithError(dlErr).Warn("failed to record dead letter entry")
			} else if dlErr == nil {
				m.metrics.RecordDeadLetter(eventType)
			}
		}
		return err
	}

	if m.metrics != nil {
		m.metrics.handled.WithLabelValues(eventType).Inc()
	}
	return nil
}
