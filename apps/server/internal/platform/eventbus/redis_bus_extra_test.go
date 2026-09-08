package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func TestNewRedisBusWithNilLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, nil)
	defer bus.Close()

	if bus.logger != logrus.StandardLogger() {
		t.Fatal("expected nil logger to fall back to the standard logger")
	}
	waitForSubscription(t, bus)
}

func TestWaitUntilReadyReturnsFalseWhenContextExpires(t *testing.T) {
	// A bus pointed at an unreachable server never confirms its subscription,
	// so a caller with an already-expiring context must observe false.
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
	})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if bus.waitUntilReady(ctx) {
		t.Fatal("expected waitUntilReady to return false on context expiry")
	}
}

func TestRedisBusDispatchFromStreamReadError(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	received := make(chan struct{}, 1)
	bus.Subscribe("ticket.readerr", HandlerFunc(func(ctx context.Context, e Event) error {
		received <- struct{}{}
		return nil
	}))
	waitForSubscription(t, bus)

	mr.SetError("XRANGE forced failure")
	bus.dispatchFromStream(context.Background(), "servify:events:ticket.readerr|1-1")

	select {
	case <-received:
		t.Fatal("handler should not run when the stream read fails")
	case <-time.After(100 * time.Millisecond):
	}
}

// failPublishHook forces PUBLISH commands to fail while letting other
// commands (XADD, etc.) pass through untouched.
type failPublishHook struct{}

func (failPublishHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (failPublishHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.FullName() == "publish" {
			return errors.New("pubsub publish forced failure")
		}
		return next(ctx, cmd)
	}
}

func (failPublishHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestRedisBusPublishToleratesPubSubFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	client.AddHook(failPublishHook{})

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()
	waitForSubscription(t, bus)

	event := BaseEvent{EventID: "evt-pubsub-fail", EventName: "ticket.pubsubfail"}
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish() should tolerate pub/sub failure once the stream write succeeds: %v", err)
	}

	streamKey := "servify:events:" + event.Name()
	messages, err := client.XRange(context.Background(), streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected persisted stream message, got %d", len(messages))
	}
}

func TestInMemoryBusPublishNilEvent(t *testing.T) {
	bus := NewInMemoryBusWithLogger(logrus.New())
	if err := bus.Publish(context.Background(), nil); err != nil {
		t.Fatalf("Publish(nil) error = %v", err)
	}
	if health := bus.Health(); health.PublishedCount != 0 {
		t.Fatalf("Publish(nil) should not count, got %d", health.PublishedCount)
	}
}

func TestRedisBusSubscribeLoopStopsOnChannelClose(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	waitForSubscription(t, bus)

	// Closing the bus cancels the loop context; the loop must exit promptly.
	done := make(chan struct{})
	go func() {
		_ = bus.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bus did not close in time")
	}
}
