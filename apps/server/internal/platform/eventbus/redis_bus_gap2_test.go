package eventbus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// TestRedisBusAwaitSubscriptionRetriesUntilConfirmed 确定性地覆盖
// awaitSubscriptionConfirmed 的重试等待分支：首次 Receive 报告未确认
// （如启动期 Redis 短暂不可达），经 ticker 重试后拿到服务端确认。生产下
// SUBSCRIBE 确认何时到达是天然竞态，因此经 seam 注入固定序列。
//
// 注入按 ctx 区分调用方：更早测试遗留的 subscribeLoop 协程可能仍在并发
// 等待确认，它们被立即"确认"以免吞掉属于本测试的首轮失败结果；ctx 断言
// 保证重试路径由本测试的协程执行。
func TestRedisBusAwaitSubscriptionRetriesUntilConfirmed(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	myCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var myCalls atomic.Int32
	previous := pubSubReceiveFn
	pubSubReceiveMu.Lock()
	pubSubReceiveFn = func(ctx context.Context, _ *redis.PubSub) (interface{}, error) {
		if ctx != myCtx {
			return &redis.Subscription{}, nil
		}
		if myCalls.Add(1) == 1 {
			return nil, errors.New("subscription not confirmed yet")
		}
		return &redis.Subscription{}, nil
	}
	pubSubReceiveMu.Unlock()
	t.Cleanup(func() {
		pubSubReceiveMu.Lock()
		pubSubReceiveFn = previous
		pubSubReceiveMu.Unlock()
	})

	bus := &RedisBus{
		client:   client,
		logger:   logrus.New(),
		handlers: make(map[string][]Handler),
		ctx:      myCtx,
		cancel:   cancel,
		readyCh:  make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		bus.awaitSubscriptionConfirmed(nil)
	}()

	select {
	case <-bus.readyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription was never confirmed after retries")
	}
	<-done

	if got := myCalls.Load(); got < 2 {
		t.Fatalf("Receive call count = %d, want >= 2 (retry path not exercised)", got)
	}
}
