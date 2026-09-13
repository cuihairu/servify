package eventbus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// TestRedisBusAwaitSubscriptionRetriesUntilConfirmed 确定性地覆盖
// awaitSubscriptionConfirmed 的轮询等待分支：首次 NUMSUB 报告 0 个订阅者
// （未确认），经 ticker 重试后确认。生产下首次轮询结果取决于 SUBSCRIBE
// 命令何时被服务端处理，是天然竞态，因此经 seam 注入固定序列。
//
// 注入按 ctx 区分调用方：更早测试遗留的 subscribeLoop 协程可能仍在并发
// 轮询，它们被立即"确认"以免吞掉属于本测试的首轮 0 订阅结果；ctx 断言
// 保证重试路径由本测试的协程执行。
func TestRedisBusAwaitSubscriptionRetriesUntilConfirmed(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	myCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var myCalls atomic.Int32
	previous := pubSubNumSubFn
	pubSubNumSubMu.Lock()
	pubSubNumSubFn = func(ctx context.Context, c *redis.Client, ch string) (map[string]int64, error) {
		if ctx != myCtx {
			return map[string]int64{ch: 1}, nil
		}
		if myCalls.Add(1) == 1 {
			return map[string]int64{}, nil
		}
		return map[string]int64{ch: 1}, nil
	}
	pubSubNumSubMu.Unlock()
	t.Cleanup(func() {
		pubSubNumSubMu.Lock()
		pubSubNumSubFn = previous
		pubSubNumSubMu.Unlock()
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
		bus.awaitSubscriptionConfirmed()
	}()

	select {
	case <-bus.readyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription was never confirmed after retries")
	}
	<-done

	if got := myCalls.Load(); got < 2 {
		t.Fatalf("NUMSUB poll count = %d, want >= 2 (retry path not exercised)", got)
	}
}
