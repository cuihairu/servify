package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// TestRedisBusBroadcastsToEverySubscribingInstance 是
// docs/multi-instance-boundary.md 第 3 节的测试锚点：RedisBus 的投递
// 语义是"广播"——两个 bus 实例共享同一 Redis 时，同一条事件两边的
// 订阅方都各收到一次（不是 consumer group 式的竞争消费）。
//
// 该语义直接影响多实例部署下的副作用评估（webhook 入队 ×N 等），
// 未来若改为竞争消费（启用 XREADGROUP），本测试会失败，
// 届时必须同步更新边界文档与部署口径。
func TestRedisBusBroadcastsToEverySubscribingInstance(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	busA := NewRedisBus(client, logrus.New())
	defer busA.Close()
	busB := NewRedisBus(client, logrus.New())
	defer busB.Close()

	event := BaseEvent{
		EventID:          "evt-multi-instance-1",
		EventName:        "ticket.created",
		EventOccurredAt:  time.Now(),
		EventTenantID:    "tenant-1",
		EventAggregateID: "ticket-1",
	}

	receivedA := make(chan Event, 1)
	receivedB := make(chan Event, 1)
	handler := func(sink chan Event) HandlerFunc {
		return HandlerFunc(func(ctx context.Context, got Event) error {
			sink <- got
			return nil
		})
	}
	busA.Subscribe(event.Name(), handler(receivedA))
	busB.Subscribe(event.Name(), handler(receivedB))

	// 修复后 waitUntilReady 走本连接的 SUBSCRIBE 确认口径（pubsub.Receive
	// 返回 *redis.Subscription 即 server 已处理本实例的 SUBSCRIBE），不再有
	// 早期 NUMSUB 口径"channel 计数不分连接"的歧义：两个实例各自 ready 即
	// 两边订阅都已生效，Publish 广播必达。
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readyCancel()
	if !busA.waitUntilReady(readyCtx) {
		t.Fatal("instance A subscription was never confirmed")
	}
	if !busB.waitUntilReady(readyCtx) {
		t.Fatal("instance B subscription was never confirmed")
	}

	if err := busA.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	waitRecv := func(name string, ch chan Event) {
		t.Helper()
		select {
		case got := <-ch:
			if got.ID() != event.ID() {
				t.Fatalf("%s: unexpected event id: got %s want %s", name, got.ID(), event.ID())
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: timed out waiting for broadcast delivery", name)
		}
	}
	waitRecv("instance A", receivedA)
	waitRecv("instance B", receivedB)
}
