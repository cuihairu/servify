package async

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
)

type failingRecorder struct{ err error }

func (f *failingRecorder) Record(_ context.Context, _ DeadLetterEntry) error { return f.err }

func (f *failingRecorder) List(_ context.Context, _ string, _ int) ([]DeadLetterEntry, error) {
	return nil, nil
}

func newTestEvent(id, name string) *testEvent {
	return &testEvent{base: eventbus.BaseEvent{
		EventID:         id,
		EventName:       name,
		EventOccurredAt: time.Now(),
	}}
}

func counterTotal(t *testing.T, reg *metrics.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("unexpected gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			total := 0.0
			for _, m := range mf.GetMetric() {
				total += m.GetCounter().GetValue()
			}
			return total
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func TestBusMetrics_RecordPublished(t *testing.T) {
	reg := metrics.NewRegistry()
	bm := NewBusMetrics(reg)

	bm.RecordPublished("order.created", "success")
	bm.RecordPublished("order.created", "failed")
	bm.RecordPublished("order.created", "failed")

	if v := counterTotal(t, reg, "eventbus_published_total"); v != 3 {
		t.Fatalf("expected published total 3, got %v", v)
	}
}

func TestBusMetrics_RecordPublished_NilGuards(t *testing.T) {
	var nilBM *BusMetrics
	nilBM.RecordPublished("a", "b")

	reg := metrics.NewRegistry()
	bm := NewBusMetrics(reg)
	bm.published = nil
	bm.RecordPublished("a", "b")
}

func TestBusMiddleware_Failure_DeadLetterRecordError_WithLogger(t *testing.T) {
	reg := metrics.NewRegistry()
	bm := NewBusMetrics(reg)

	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)

	handler := eventbus.HandlerFunc(func(ctx context.Context, event eventbus.Event) error {
		return errors.New("boom")
	})

	wrapped := WrapHandler("test.event", handler, bm, &failingRecorder{err: errors.New("dl store down")}, logger)

	err := wrapped.Handle(context.Background(), newTestEvent("evt-3", "test.event"))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected handler error, got %v", err)
	}
	if !strings.Contains(buf.String(), "failed to record dead letter entry") {
		t.Fatalf("expected warn log about dead letter failure, got %q", buf.String())
	}
	if v := counterTotal(t, reg, "eventbus_failed_total"); v != 1 {
		t.Fatalf("expected failed total 1, got %v", v)
	}
}

func TestBusMiddleware_Failure_NilMetrics_FailingRecorder_NilLogger(t *testing.T) {
	handler := eventbus.HandlerFunc(func(ctx context.Context, event eventbus.Event) error {
		return errors.New("boom2")
	})

	wrapped := WrapHandler("test.event", handler, nil, &failingRecorder{err: errors.New("dl down")}, nil)

	err := wrapped.Handle(context.Background(), newTestEvent("evt-4", "test.event"))
	if err == nil || err.Error() != "boom2" {
		t.Fatalf("expected handler error, got %v", err)
	}
}

func TestBusMiddleware_Success_NilMetrics(t *testing.T) {
	called := false
	handler := eventbus.HandlerFunc(func(ctx context.Context, event eventbus.Event) error {
		called = true
		return nil
	})

	wrapped := WrapHandler("test.event", handler, nil, nil, nil)

	if err := wrapped.Handle(context.Background(), newTestEvent("evt-5", "test.event")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
}
