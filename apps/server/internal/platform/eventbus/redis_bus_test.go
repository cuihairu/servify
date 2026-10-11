package eventbus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func TestRedisBusPublishPersistsToStream(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	bus := NewRedisBus(client, logrus.New())
	defer func() { _ = bus.Close() }()

	event := BaseEvent{
		EventID:          "evt-redis-1",
		EventName:        "ticket.created",
		EventOccurredAt:  time.Unix(1710000000, 0),
		EventTenantID:    "tenant-1",
		EventAggregateID: "ticket-1",
	}

	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	streamKey := fmt.Sprintf(eventStreamPattern, event.Name())
	messages, err := client.XRange(context.Background(), streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("read stream failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 stream message, got %d", len(messages))
	}
	if got := messages[0].Values["id"]; got != event.ID() {
		t.Fatalf("unexpected event id: got %v want %s", got, event.ID())
	}
	if got := messages[0].Values["tenant_id"]; got != event.TenantID() {
		t.Fatalf("unexpected tenant id: got %v want %s", got, event.TenantID())
	}
}

func TestRedisBusSubscribeReceivesPublishedEvent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	bus := NewRedisBus(client, logrus.New())
	defer func() { _ = bus.Close() }()

	event := BaseEvent{
		EventID:          "evt-redis-2",
		EventName:        "routing.agent_assigned",
		EventOccurredAt:  time.Now(),
		EventTenantID:    "tenant-1",
		EventAggregateID: "assignment-1",
	}

	received := make(chan Event, 1)
	bus.Subscribe(event.Name(), HandlerFunc(func(ctx context.Context, got Event) error {
		received <- got
		return nil
	}))

	waitForSubscription(t, bus)

	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.ID() != event.ID() {
			t.Fatalf("unexpected event id: got %s want %s", got.ID(), event.ID())
		}
		if got.AggregateID() != event.AggregateID() {
			t.Fatalf("unexpected aggregate id: got %s want %s", got.AggregateID(), event.AggregateID())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redis bus delivery")
	}
}

func TestRedisBusHealth(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	bus := NewRedisBus(client, logrus.New())
	defer func() { _ = bus.Close() }()

	if err := bus.Health(context.Background()); err != nil {
		t.Fatalf("health failed: %v", err)
	}
}

func waitForSubscription(t *testing.T, bus *RedisBus) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if bus.waitUntilReady(ctx) {
		return
	}

	t.Fatal("redis bus subscription did not start")
}
