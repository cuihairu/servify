package delivery

// 覆盖补充（Service Timeline 投影）：无来源会话的工单事件跳过投影、
// 载荷编码失败降级为空串（经 seam 注入）。

import (
	"context"
	"errors"
	"testing"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	routingapp "servify/apps/server/internal/modules/routing/application"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/platform/eventbus"
)

func TestEventBusSubscriberSkipsTicketEventWithoutSession(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	repo := &fakeEventRepo{}
	NewEventBusSubscriber(repo).Register(bus)

	// 邮件建单等无来源会话：SessionID 为 nil，投影跳过（Timeline 只覆盖
	// 会话视角）。
	if err := bus.Publish(context.Background(), ticketapp.NewTicketEvent(
		ticketapp.TicketCreatedEventName, ticketapp.TicketDTO{ID: 8})); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(repo.appended) != 0 {
		t.Fatalf("session-less ticket event must be skipped, got %+v", repo.appended)
	}
}

func TestEncodeEventPayloadMarshalFailureFallsBackToEmpty(t *testing.T) {
	orig := eventPayloadMarshal
	defer func() { eventPayloadMarshal = orig }()
	eventPayloadMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }

	event := ticketapp.NewTicketEvent(ticketapp.TicketClosedEventName, ticketapp.TicketDTO{ID: 1})
	if got := encodeEventPayload(event); got != "" {
		t.Fatalf("encode failure must fall back to empty, got %q", got)
	}
}

func TestEventBusSubscriberZeroOccurredAtFallsBackToNow(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	repo := &fakeEventRepo{}
	NewEventBusSubscriber(repo).Register(bus)

	// 时钟不可靠的来源可能给出零值时间戳：投影侧回退 time.Now，不落零值行。
	zero := routingapp.RoutingEvent{BaseEvent: eventbus.BaseEvent{
		EventName:        routingapp.RoutingAgentAssignedEventName,
		EventAggregateID: "routing:conv-zero",
	}}
	if err := bus.Publish(context.Background(), zero); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := bus.Publish(context.Background(), conversationapp.NewConversationEvent(
		conversationapp.ConversationCreatedEventName, "conv-zero", nil)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(repo.appended) != 2 {
		t.Fatalf("appended = %+v", repo.appended)
	}
	if repo.appended[0].OccurredAt.IsZero() {
		t.Fatalf("zero occurred_at must fall back to now: %+v", repo.appended[0])
	}
	// 回退值即投影时刻，与真实事件时间同处当前时钟附近。
	if delta := repo.appended[0].OccurredAt.Sub(repo.appended[1].OccurredAt); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("fallback time far from now: %v vs %v",
			repo.appended[0].OccurredAt, repo.appended[1].OccurredAt)
	}
}
