package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	"servify/apps/server/internal/platform/eventbus"
)

type recordingBus struct {
	handlers map[string]eventbus.Handler
}

func newRecordingBus() *recordingBus {
	return &recordingBus{handlers: map[string]eventbus.Handler{}}
}

func (b *recordingBus) Subscribe(eventName string, handler eventbus.Handler) {
	b.handlers[eventName] = handler
}

func (b *recordingBus) Publish(ctx context.Context, event eventbus.Event) error {
	return nil
}

type recorderRepo struct {
	events []analyticsapp.IncrementEvent
	err    error
}

func (r *recorderRepo) GetDashboardStats(ctx context.Context) (*analyticsapp.DashboardStats, error) {
	return nil, nil
}

func (r *recorderRepo) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticsapp.TimeRangeStats, error) {
	return nil, nil
}

func (r *recorderRepo) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticsapp.AgentPerformanceStats, error) {
	return nil, nil
}

func (r *recorderRepo) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticsapp.CategoryStats, error) {
	return nil, nil
}

func (r *recorderRepo) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticsapp.CategoryStats, error) {
	return nil, nil
}

func (r *recorderRepo) GetCustomerSourceStats(ctx context.Context) ([]analyticsapp.CategoryStats, error) {
	return nil, nil
}

func (r *recorderRepo) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return nil
}

func (r *recorderRepo) IncrementDailyStat(ctx context.Context, event analyticsapp.IncrementEvent) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

func TestEventBusSubscriberRegisterGuards(t *testing.T) {
	var nilBus *recordingBus
	var nilSubscriber *EventBusSubscriber
	// nil bus and nil subscriber must not panic
	nilSubscriber.Register(nilBus)

	bus := newRecordingBus()
	// subscriber with nil service must register nothing
	emptySubscriber := &EventBusSubscriber{}
	emptySubscriber.Register(bus)
	if len(bus.handlers) != 0 {
		t.Fatalf("expected no subscriptions for nil service, got %d", len(bus.handlers))
	}
}

func TestEventBusSubscriberRegisterRegistersNoopWithoutBus(t *testing.T) {
	subscriber := NewEventBusSubscriber(nil)
	if subscriber == nil {
		t.Fatal("expected subscriber instance")
	}
	// service is nil: Register must be a no-op
	subscriber.Register(nil)
}

func TestEventBusSubscriberRegisterSubscribesEvents(t *testing.T) {
	repo := &recorderRepo{}
	subscriber := NewEventBusSubscriber(analyticsapp.NewService(repo))
	bus := newRecordingBus()

	subscriber.Register(bus)

	expected := map[string]analyticsapp.IncrementKind{
		"conversation.created":          analyticsapp.IncrementSessions,
		"conversation.message_received": analyticsapp.IncrementMessages,
		"ticket.created":                analyticsapp.IncrementTickets,
		"ticket.closed":                 analyticsapp.IncrementResolved,
		"sla.violation":                 analyticsapp.IncrementSLA,
	}
	if len(bus.handlers) != len(expected) {
		t.Fatalf("expected %d subscriptions, got %d: %v", len(expected), len(bus.handlers), bus.handlers)
	}

	ctx := context.Background()
	event := eventbus.BaseEvent{EventID: "e-1", EventName: "x", EventOccurredAt: time.Now()}
	for name, kind := range expected {
		handler, ok := bus.handlers[name]
		if !ok {
			t.Fatalf("expected subscription for %q", name)
		}
		before := time.Now()
		if err := handler.Handle(ctx, event); err != nil {
			t.Fatalf("handler for %q returned error: %v", name, err)
		}
		if len(repo.events) == 0 {
			t.Fatalf("handler for %q did not record an event", name)
		}
		got := repo.events[len(repo.events)-1]
		if got.Kind != kind {
			t.Fatalf("event %q mapped to kind %s, want %s", name, got.Kind, kind)
		}
		if got.Date.Before(before) || got.Date.After(time.Now()) {
			t.Fatalf("event %q recorded unexpected date: %v", name, got.Date)
		}
	}
	if len(repo.events) != len(expected) {
		t.Fatalf("expected %d recorded events, got %d", len(expected), len(repo.events))
	}
}

func TestEventBusSubscriberHandlerPropagatesError(t *testing.T) {
	repo := &recorderRepo{err: errors.New("increment failed")}
	subscriber := NewEventBusSubscriber(analyticsapp.NewService(repo))
	bus := newRecordingBus()

	subscriber.Register(bus)

	handler, ok := bus.handlers["ticket.created"]
	if !ok {
		t.Fatal("expected subscription for ticket.created")
	}
	err := handler.Handle(context.Background(), eventbus.BaseEvent{EventID: "e-2"})
	if err == nil || err.Error() != "increment failed" {
		t.Fatalf("expected propagated error, got %v", err)
	}
}
