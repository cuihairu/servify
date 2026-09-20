package eventbus

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// newUnconfirmedRedisBus 构造一个永远不会自行确认订阅的 bus（不启动
// subscribeLoop），供就绪门控测试精确控制 readyCh 的关闭时机。
func newUnconfirmedRedisBus(t *testing.T, addr string) *RedisBus {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	return &RedisBus{
		client:   client,
		logger:   logrus.New(),
		handlers: make(map[string][]Handler),
		ctx:      context.Background(),
		cancel:   func() {},
		readyCh:  make(chan struct{}),
	}
}

// TestRedisBusPublishFailsWhenBusNotReady 覆盖 Publish 的未就绪错误分支：
// 订阅未确认且调用方 ctx 到期时，Publish 必须报错而不是继续发布——否则
// 事件写入 stream 却无 pub/sub 通知，本地 handler 永不分发。
func TestRedisBusPublishFailsWhenBusNotReady(t *testing.T) {
	mr := miniredis.RunT(t)
	bus := newUnconfirmedRedisBus(t, mr.Addr())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := bus.Publish(ctx, BaseEvent{EventID: "evt-notready", EventName: "ticket.notready"})
	if err == nil {
		t.Fatal("Publish() expected not-ready error")
	}
	if !strings.Contains(err.Error(), "event bus not ready") {
		t.Fatalf("unexpected error: %v", err)
	}

	count, err := bus.client.XLen(context.Background(), "servify:events:ticket.notready").Result()
	if err != nil || count != 0 {
		t.Fatalf("stream should be untouched before readiness, got %d entries (err=%v)", count, err)
	}
}

// TestRedisBusPublishBlocksUntilSubscriptionConfirmed 锁死门控行为本身：
// 订阅确认前 Publish 不得返回；确认（readyCh 关闭）后放行并完成 stream 写入。
func TestRedisBusPublishBlocksUntilSubscriptionConfirmed(t *testing.T) {
	mr := miniredis.RunT(t)
	bus := newUnconfirmedRedisBus(t, mr.Addr())

	event := BaseEvent{EventID: "evt-blocked", EventName: "ticket.blocked"}
	published := make(chan error, 1)
	go func() {
		published <- bus.Publish(context.Background(), event)
	}()

	select {
	case err := <-published:
		t.Fatalf("Publish returned before subscription confirmation: %v", err)
	case <-time.After(50 * time.Millisecond):
		// 仍在等待确认，符合预期。
	}

	close(bus.readyCh) // 模拟 subscribeLoop 拿到服务端确认

	select {
	case err := <-published:
		if err != nil {
			t.Fatalf("Publish() after readiness error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Publish did not resume after the subscription was confirmed")
	}

	streamKey := fmt.Sprintf(eventStreamPattern, event.Name())
	messages, err := bus.client.XRange(context.Background(), streamKey, "-", "+").Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("expected exactly 1 stream message after release, got %d (err=%v)", len(messages), err)
	}
}

// TestRedisBusPublishBeforeSubscriptionConfirmedStillDelivers 是订阅确认
// 竞态的端到端回归锚点：NewRedisBus 之后不等待就绪立即发布的事件，也必须
// 最终被本实例的 handler 收到（修复前 pub/sub 通知可能先于订阅生效而被
// 丢弃，事件写入了 stream 却无人分发）。Publish 现在内部等待确认，此测试
// 不再依赖时序运气。
func TestRedisBusPublishBeforeSubscriptionConfirmedStillDelivers(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	bus := NewRedisBus(client, logrus.New())
	defer bus.Close()

	event := BaseEvent{
		EventID:          "evt-race-1",
		EventName:        "ticket.race",
		EventOccurredAt:  time.Now(),
		EventTenantID:    "tenant-1",
		EventAggregateID: "ticket-race-1",
	}

	received := make(chan Event, 1)
	bus.Subscribe(event.Name(), HandlerFunc(func(ctx context.Context, got Event) error {
		received <- got
		return nil
	}))

	// 故意不等待订阅确认，立即发布。
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.ID() != event.ID() {
			t.Fatalf("unexpected event id: got %s want %s", got.ID(), event.ID())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event published before subscription confirmation was never dispatched")
	}
}
