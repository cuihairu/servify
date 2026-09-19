package async

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/eventbus"
)

func obsEvent(id, name string) eventbus.Event {
	return eventbus.BaseEvent{
		EventID:         id,
		EventName:       name,
		EventOccurredAt: time.Now(),
	}
}

type recordingHandler struct {
	err   error
	calls int
}

func (h *recordingHandler) Handle(ctx context.Context, event eventbus.Event) error {
	h.calls++
	return h.err
}

func asyncMetricValue(t *testing.T, reg *metrics.Registry, name string, want map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if len(labels) != len(want) {
				continue
			}
			matched := true
			for k, v := range want {
				if labels[k] != v {
					matched = false
					break
				}
			}
			if matched {
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
				if m.GetHistogram() != nil {
					return float64(m.GetHistogram().GetSampleCount())
				}
			}
		}
	}
	// 无样本视为 0（counter vec 惰性创建，未打点时 family 不出现）。
	return 0
}

func TestObservableBusPublishAndHandle(t *testing.T) {
	reg := metrics.NewRegistry()
	bus := eventbus.NewInMemoryBus()
	obs := NewObservableBus(bus, NewBusMetrics(reg), NewInMemoryDeadLetterRecorder(10), nil)

	okHandler := &recordingHandler{}
	obs.Subscribe("ticket.created", okHandler)

	if err := obs.Publish(context.Background(), obsEvent("evt-1", "ticket.created")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if okHandler.calls != 1 {
		t.Fatalf("handler calls = %d, want 1", okHandler.calls)
	}
	if got := asyncMetricValue(t, reg, "eventbus_published_total", map[string]string{
		"event_type": "ticket.created", "outcome": "success",
	}); got != 1 {
		t.Fatalf("published success = %v, want 1", got)
	}
	if got := asyncMetricValue(t, reg, "eventbus_handled_total", map[string]string{
		"event_type": "ticket.created",
	}); got != 1 {
		t.Fatalf("handled = %v, want 1", got)
	}
	if got := asyncMetricValue(t, reg, "eventbus_handle_duration_seconds", map[string]string{
		"event_type": "ticket.created",
	}); got != 1 {
		t.Fatalf("duration observations = %v, want 1", got)
	}
}

func TestObservableBusHandlerFailureDeadLetters(t *testing.T) {
	reg := metrics.NewRegistry()
	bus := eventbus.NewInMemoryBus()
	recorder := NewInMemoryDeadLetterRecorder(10)
	obs := NewObservableBus(bus, NewBusMetrics(reg), recorder, nil)

	failHandler := &recordingHandler{err: errors.New("boom")}
	obs.Subscribe("conversation.message", failHandler)

	// InMemoryBus 同步分发：handler 失败会让 Publish 返回错误。
	if err := obs.Publish(context.Background(), obsEvent("evt-2", "conversation.message")); err == nil {
		t.Fatal("expected handler error to propagate")
	}
	// 同步总线：handler 失败从 Publish 冒出，published outcome 记 error
	//（吞吐面板口径：error = 分发未成功，含总线与 handler 失败；
	// handler 失败另有 failed_total / dead_letter_total 单独计数）。
	if got := asyncMetricValue(t, reg, "eventbus_published_total", map[string]string{
		"event_type": "conversation.message", "outcome": "error",
	}); got != 1 {
		t.Fatalf("published error = %v, want 1", got)
	}
	if got := asyncMetricValue(t, reg, "eventbus_failed_total", map[string]string{
		"event_type": "conversation.message",
	}); got != 1 {
		t.Fatalf("failed = %v, want 1", got)
	}
	if got := asyncMetricValue(t, reg, "eventbus_dead_letter_total", map[string]string{
		"event_type": "conversation.message",
	}); got != 1 {
		t.Fatalf("dead letter = %v, want 1", got)
	}
	entries, err := recorder.List(context.Background(), "conversation.message", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].EventID != "evt-2" {
		t.Fatalf("dead letter entries = %+v", entries)
	}
}

func TestObservableBusPublishErrorOutcome(t *testing.T) {
	reg := metrics.NewRegistry()
	obs := NewObservableBus(&failingBus{}, NewBusMetrics(reg), nil, nil)

	if err := obs.Publish(context.Background(), obsEvent("evt-3", "x.y")); err == nil {
		t.Fatal("expected publish error")
	}
	if got := asyncMetricValue(t, reg, "eventbus_published_total", map[string]string{
		"event_type": "x.y", "outcome": "error",
	}); got != 1 {
		t.Fatalf("published error = %v, want 1", got)
	}
}

type failingBus struct{}

func (b *failingBus) Subscribe(eventName string, handler eventbus.Handler) {}
func (b *failingBus) Publish(ctx context.Context, event eventbus.Event) error {
	return errors.New("bus down")
}

// TestWireDefaultObservableBus 覆盖入口共享装配 helper：包装后订阅/发布
// 链路可用且默认 registry 上有打点（发布计数经 DefaultRegistry 读回）。
func TestWireDefaultObservableBus(t *testing.T) {
	bus := WireDefaultObservableBus(eventbus.NewInMemoryBus(), nil)

	okHandler := &recordingHandler{}
	bus.Subscribe("ticket.created", okHandler)

	if err := bus.Publish(context.Background(), obsEvent("evt-wire", "ticket.created")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if okHandler.calls != 1 {
		t.Fatalf("handler calls = %d, want 1", okHandler.calls)
	}
	if got := asyncMetricValue(t, metrics.DefaultRegistry, "eventbus_published_total", map[string]string{
		"event_type": "ticket.created", "outcome": "success",
	}); got != 1 {
		t.Fatalf("default registry published success = %v, want 1", got)
	}
}
