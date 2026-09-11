package eventbus

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func TestCastToString(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  string
	}{
		{"nil", nil, ""},
		{"string", "abc", "abc"},
		{"int", 42, "42"},
		{"int64", int64(7), "7"},
	}
	for _, tc := range cases {
		if got := castToString(tc.value); got != tc.want {
			t.Fatalf("%s: castToString(%v) = %q, want %q", tc.name, tc.value, got, tc.want)
		}
	}
}

func TestCastToInt64(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  int64
	}{
		{"nil", nil, 0},
		{"int64", int64(99), 99},
		{"int", 12, 12},
		{"float64", 3.9, 3},
		{"numeric string", "456", 456},
		{"non-numeric string", "abc", 0},
		{"unsupported bool", true, 0},
	}
	for _, tc := range cases {
		if got := castToInt64(tc.value); got != tc.want {
			t.Fatalf("%s: castToInt64(%v) = %d, want %d", tc.name, tc.value, got, tc.want)
		}
	}
}

func TestFormatAndParseDispatchPayload(t *testing.T) {
	if got := formatDispatchPayload("stream", ""); got != "stream" {
		t.Fatalf("formatDispatchPayload without id = %q", got)
	}
	if got := formatDispatchPayload("stream", "1-1"); got != "stream|1-1" {
		t.Fatalf("formatDispatchPayload with id = %q", got)
	}

	key, id := parseDispatchPayload("servify:events:ticket.created|123-4")
	if key != "servify:events:ticket.created" || id != "123-4" {
		t.Fatalf("parseDispatchPayload = %q/%q", key, id)
	}

	key, id = parseDispatchPayload("servify:events:ticket.created")
	if key != "servify:events:ticket.created" || id != "" {
		t.Fatalf("parseDispatchPayload without id = %q/%q", key, id)
	}
}

func TestStreamEventAccessors(t *testing.T) {
	occurred := time.Unix(1710000000, 0)
	e := &streamEvent{
		id:          "evt-1",
		name:        "ticket.created",
		data:        "{}",
		occurredAt:  occurred,
		tenantID:    "tenant-1",
		aggregateID: "agg-1",
	}
	if e.ID() != "evt-1" || e.Name() != "ticket.created" || e.TenantID() != "tenant-1" || e.AggregateID() != "agg-1" {
		t.Fatalf("accessors = %q/%q/%q/%q", e.ID(), e.Name(), e.TenantID(), e.AggregateID())
	}
	if !e.OccurredAt().Equal(occurred) {
		t.Fatalf("OccurredAt() = %v, want %v", e.OccurredAt(), occurred)
	}
}

// unserializableEvent implements Event but cannot be JSON-marshalled.
type unserializableEvent struct {
	BaseEvent
	Ch chan int `json:"ch"`
}

func TestRedisBusPublishNilEvent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	if err := bus.Publish(context.Background(), nil); err != nil {
		t.Fatalf("Publish(nil) error = %v", err)
	}
}

func TestRedisBusPublishUnserializableEvent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	event := unserializableEvent{Ch: make(chan int)}
	event.EventID = "evt-bad"
	event.EventName = "ticket.bad"
	if err := bus.Publish(context.Background(), event); err == nil {
		t.Fatal("Publish() expected marshal error")
	}
}

func TestRedisBusPublishStreamError(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	mr.SetError("XADD forced failure")

	event := BaseEvent{EventID: "evt-x", EventName: "ticket.x"}
	if err := bus.Publish(context.Background(), event); err == nil {
		t.Fatal("Publish() expected stream error")
	}
}

func TestRedisBusDispatchMessageInvalidData(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	handlerCalled := false
	bus.Subscribe("ticket.invalid", HandlerFunc(func(ctx context.Context, e Event) error {
		handlerCalled = true
		return nil
	}))

	msg := redis.XMessage{ID: "1-1", Values: map[string]interface{}{"id": "evt-1", "occurred_at": "not-a-number"}}
	// data key missing => invalid event data path
	bus.dispatchMessage(context.Background(), "ticket.invalid", msg, nil)

	if handlerCalled {
		t.Fatal("handler should not be called for invalid data")
	}
}

func TestRedisBusDispatchMessageHandlerError(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	sent := make(chan struct{}, 2)
	handlers := []Handler{
		HandlerFunc(func(ctx context.Context, e Event) error {
			sent <- struct{}{}
			return errors.New("handler boom")
		}),
		HandlerFunc(func(ctx context.Context, e Event) error {
			sent <- struct{}{}
			return nil
		}),
	}

	msg := redis.XMessage{
		ID: "1-1",
		Values: map[string]interface{}{
			"id":          "evt-1",
			"data":        `{"event_id":"evt-1"}`,
			"occurred_at": 1710000000,
			"tenant_id":   "tenant-1",
			"aggregate":   "agg-1",
		},
	}
	bus.dispatchMessage(context.Background(), "ticket.created", msg, handlers)

	if len(sent) != 2 {
		t.Fatalf("expected both handlers to run despite first error, got %d", len(sent))
	}
}

func TestRedisBusDispatchFromStreamNoHandlers(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	// No handlers registered: dispatchFromStream should return early without error.
	bus.dispatchFromStream(context.Background(), "servify:events:ticket.nohandler|1-1")
}

func TestRedisBusDispatchFromStreamMissingMessage(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	received := make(chan Event, 1)
	bus.Subscribe("ticket.missing", HandlerFunc(func(ctx context.Context, e Event) error {
		received <- e
		return nil
	}))

	// Message id references a stream entry that does not exist: redis.Nil path is tolerated.
	bus.dispatchFromStream(context.Background(), "servify:events:ticket.missing|999-999")

	select {
	case <-received:
		t.Fatal("handler should not be called for missing message")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRedisBusReadMessagesWithoutMessageID(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	event := BaseEvent{EventID: "evt-fallback", EventName: "ticket.fallback"}
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// Fallback path (payload without message id) reads the stream tail; may return no messages.
	streamKey := fmt.Sprintf(eventStreamPattern, event.Name())
	if _, err := bus.readMessages(context.Background(), streamKey, ""); err != nil && err != redis.Nil {
		t.Fatalf("readMessages without id error = %v", err)
	}
}

func TestNewInMemoryBusWithLogger(t *testing.T) {
	logger := logrus.New()
	bus := NewInMemoryBusWithLogger(logger)
	if bus == nil {
		t.Fatal("NewInMemoryBusWithLogger() returned nil")
	}
	if bus.logger != logger {
		t.Fatal("NewInMemoryBusWithLogger() did not apply custom logger")
	}
	if health := bus.Health(); health.PublishedCount != 0 {
		t.Fatalf("new bus PublishedCount = %d, want 0", health.PublishedCount)
	}
}

func TestInMemoryBusSubscribeIgnoresInvalidInput(t *testing.T) {
	bus := NewInMemoryBusWithLogger(logrus.New())
	bus.Subscribe("", HandlerFunc(func(ctx context.Context, e Event) error { return nil }))
	bus.Subscribe("ticket.created", nil)

	if err := bus.Publish(context.Background(), BaseEvent{EventID: "evt-1", EventName: "ticket.created"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}
