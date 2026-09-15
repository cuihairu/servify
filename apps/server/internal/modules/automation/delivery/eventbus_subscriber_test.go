package delivery

import (
	"context"
	automationdomain "servify/apps/server/internal/modules/automation/domain"
	"testing"

	automationapp "servify/apps/server/internal/modules/automation/application"
	"servify/apps/server/internal/platform/eventbus"
)

type recordingBus struct {
	handlers map[string][]eventbus.Handler
}

func newRecordingBus() *recordingBus {
	return &recordingBus{handlers: map[string][]eventbus.Handler{}}
}

func (b *recordingBus) Subscribe(eventName string, handler eventbus.Handler) {
	b.handlers[eventName] = append(b.handlers[eventName], handler)
}

func (b *recordingBus) Publish(ctx context.Context, evt eventbus.Event) error {
	return nil
}

func TestSubscriberRegisterNilBus(t *testing.T) {
	repo := &stubAutomationRepo{}
	sub := NewEventBusSubscriber(automationapp.NewService(repo))
	sub.Register(nil)
	if len(repo.listedEvents) != 0 {
		t.Fatalf("expected no event handling, got %v", repo.listedEvents)
	}
}

func TestSubscriberRegisterNilReceiver(t *testing.T) {
	var sub *EventBusSubscriber
	bus := newRecordingBus()
	sub.Register(bus)
	if len(bus.handlers) != 0 {
		t.Fatalf("expected no subscriptions, got %v", bus.handlers)
	}
}

func TestSubscriberRegisterNilService(t *testing.T) {
	bus := newRecordingBus()
	sub := NewEventBusSubscriber(nil)
	sub.Register(bus)
	if len(bus.handlers) != 0 {
		t.Fatalf("expected no subscriptions, got %v", bus.handlers)
	}
}

func TestSubscriberRegisterSubscribesAllEvents(t *testing.T) {
	bus := newRecordingBus()
	repo := &stubAutomationRepo{}
	NewEventBusSubscriber(automationapp.NewService(repo)).Register(bus)

	want := []string{
		"ticket.created",
		"ticket.updated",
		"ticket.assigned",
		"ticket.closed",
		"conversation.created",
		"conversation.message_received",
		"routing.agent_assigned",
		"routing.transfer_completed",
	}
	if len(bus.handlers) != len(want) {
		t.Fatalf("expected %d subscriptions, got %d", len(want), len(bus.handlers))
	}
	for _, name := range want {
		if len(bus.handlers[name]) != 1 {
			t.Fatalf("expected one handler for %q, got %d", name, len(bus.handlers[name]))
		}
	}
}

func TestSubscriberHandlerDispatchesToService(t *testing.T) {
	bus := newRecordingBus()
	repo := &stubAutomationRepo{
		triggers: []automationdomain.AutomationTrigger{{ID: 1, Event: "ticket.created", Active: true}},
	}
	NewEventBusSubscriber(automationapp.NewService(repo)).Register(bus)

	handler := bus.handlers["ticket.created"][0]
	evt := eventbus.BaseEvent{
		EventID:          "e-1",
		EventName:        "ticket.created",
		EventTenantID:    "tenant-a",
		EventAggregateID: "7",
	}
	if err := handler.Handle(context.Background(), evt); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if len(repo.listedEvents) != 1 || repo.listedEvents[0] != "ticket.created" {
		t.Fatalf("unexpected listed events: %v", repo.listedEvents)
	}
	if len(repo.fetched) != 1 || repo.fetched[0] != 7 {
		t.Fatalf("expected ticket 7 fetched, got %v", repo.fetched)
	}

	handler = bus.handlers["ticket.updated"][0]
	evt = eventbus.BaseEvent{EventName: "ticket.updated", EventAggregateID: "not-a-number"}
	if err := handler.Handle(context.Background(), evt); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if len(repo.listedEvents) != 2 || repo.listedEvents[1] != "ticket.updated" {
		t.Fatalf("unexpected listed events: %v", repo.listedEvents)
	}
	if len(repo.fetched) != 1 {
		t.Fatalf("expected no extra ticket fetch, got %v", repo.fetched)
	}
}
