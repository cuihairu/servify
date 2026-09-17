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

	// 不能用 waitUntilReady：它走 PUBSUB NUMSUB 口径，只保证"channel 上已有
	// 订阅者"，不区分是哪个连接——busB 的 ready 可能被 busA 的订阅满足，
	// Publish 会跑在 busB 的 SUBSCRIBE 被 server 处理之前，广播通知被丢弃
	//（CI 慢环境下必现的订阅竞态）。这里直接轮询到 channel 上出现两个
	// 订阅者，即两个实例的 SUBSCRIBE 都被 server 确认后再发布。
	subscribeDeadline := time.Now().Add(5 * time.Second)
	for {
		subs, err := client.PubSubNumSub(context.Background(), eventPubSubChannel).Result()
		if err != nil {
			t.Fatalf("pubsub numsub: %v", err)
		}
		if subs[eventPubSubChannel] >= 2 {
			break
		}
		if time.Now().After(subscribeDeadline) {
			t.Fatalf("timed out waiting for both instances to subscribe: %v", subs)
		}
		time.Sleep(2 * time.Millisecond)
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
