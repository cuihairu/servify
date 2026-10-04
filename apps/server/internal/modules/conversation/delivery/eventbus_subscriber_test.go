package delivery

import (
	"context"
	"encoding/json"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/modules/conversation/domain"
	routingapp "servify/apps/server/internal/modules/routing/application"
	"servify/apps/server/internal/platform/eventbus"
)

type fakeEventRepo struct {
	appended []domain.ConversationEvent
}

func (f *fakeEventRepo) Append(_ context.Context, event domain.ConversationEvent) error {
	f.appended = append(f.appended, event)
	return nil
}

func (f *fakeEventRepo) ListByConversation(context.Context, string, int, int) ([]domain.ConversationEvent, error) {
	return nil, nil
}

func TestEventBusSubscriber_ProjectsTimelineEvents(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	repo := &fakeEventRepo{}
	NewEventBusSubscriber(repo).Register(bus)

	ctx := context.Background()
	_ = bus.Publish(ctx, conversationapp.NewConversationEvent(
		conversationapp.ConversationCreatedEventName, "conv-1", nil))
	_ = bus.Publish(ctx, routingapp.NewRoutingEvent(
		routingapp.RoutingAgentAssignedEventName, "conv-1", nil))
	_ = bus.Publish(ctx, routingapp.NewRoutingEvent(
		routingapp.RoutingTransferCompletedEventName, "conv-1", nil))

	if len(repo.appended) != 3 {
		t.Fatalf("expected 3 projections, got %d", len(repo.appended))
	}

	byType := map[string]domain.ConversationEvent{}
	for _, e := range repo.appended {
		byType[e.EventType] = e
	}

	created := byType[conversationapp.ConversationCreatedEventName]
	if created.ConversationID != "conv-1" || created.ActorType != "system" || created.Summary != "会话创建" {
		t.Errorf("created projection wrong: %+v", created)
	}
	assigned := byType[routingapp.RoutingAgentAssignedEventName]
	if assigned.ConversationID != "conv-1" || assigned.ActorType != "routing" || assigned.Summary != "分配坐席" {
		t.Errorf("assigned projection wrong: %+v", assigned)
	}
	transferred := byType[routingapp.RoutingTransferCompletedEventName]
	if transferred.ConversationID != "conv-1" || transferred.ActorType != "routing" {
		t.Errorf("transfer projection wrong: %+v", transferred)
	}
	if transferred.OccurredAt.IsZero() {
		t.Error("OccurredAt should be populated from the event")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(transferred.Payload), &payload); err != nil {
		t.Fatalf("payload should be JSON-encodable: %v", err)
	}
	if payload["event_id"] == "" {
		t.Error("payload should archive the original event_id")
	}
}

func TestEventBusSubscriber_SkipsUnknownAggregatePrefix(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	repo := &fakeEventRepo{}
	NewEventBusSubscriber(repo).Register(bus)

	ctx := context.Background()
	// ticket:* / 空聚合 ID 事件应被跳过（B1-2 才接入 ticket 投影）。
	event := routingapp.NewRoutingEvent(routingapp.RoutingAgentAssignedEventName, "conv-9", nil)
	event.EventAggregateID = ""
	_ = bus.Publish(ctx, event)

	if len(repo.appended) != 0 {
		t.Fatalf("expected no projections for foreign aggregate, got %d", len(repo.appended))
	}
}

func TestEventBusSubscriber_IgnoresNilBus(t *testing.T) {
	// nil bus 注册不 panic（装配早期路径防御）。
	NewEventBusSubscriber(&fakeEventRepo{}).Register(nil)
}

func TestTrimAggregatePrefix(t *testing.T) {
	cases := map[string]string{
		"conversation:conv-1": "conv-1",
		"routing:conv-1":      "",
		"conversation:":       "",
		"":                    "",
		"ticket:t-1":          "",
		"conversationX:conv":  "",
	}
	for input, want := range cases {
		if got := trimAggregatePrefix(input, "conversation:"); got != want {
			t.Errorf("trimAggregatePrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSubscriberRegistrationsTimelineAssertion(t *testing.T) {
	// 防回归：三笔注册的 EventType 必须与事件名一致，Timeline 展示按 EventType 分组。
	bus := eventbus.NewInMemoryBus()
	repo := &fakeEventRepo{}
	NewEventBusSubscriber(repo).Register(bus)
	_ = bus.Publish(context.Background(), conversationapp.NewConversationEvent(
		conversationapp.ConversationCreatedEventName, "conv-1", nil))
	if len(repo.appended) == 0 {
		t.Fatal("expected projection")
	}
	if repo.appended[0].EventType != conversationapp.ConversationCreatedEventName {
		t.Errorf("EventType should equal event name, got %q", repo.appended[0].EventType)
	}
}

