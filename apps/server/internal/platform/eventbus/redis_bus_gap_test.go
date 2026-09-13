package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// TestRedisBusSubscribeLoopExitsWhenPubSubChannelCloses 覆盖 subscribeLoop 的
// 消息通道被关闭(而非 ctx 取消)而退出的分支:go-redis 在连接池被关闭后
// 让 pubsub.Receive 返回 pool.ErrClosed,消息通道随之关闭。
func TestRedisBusSubscribeLoopExitsWhenPubSubChannelCloses(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := &RedisBus{
		client:   client,
		logger:   logrus.New(),
		handlers: make(map[string][]Handler),
		ctx:      ctx,
		cancel:   cancel,
		readyCh:  make(chan struct{}),
	}

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		bus.subscribeLoop()
	}()

	// readyCh 关闭是确定性同步点:订阅已确认,循环已进入 select。
	select {
	case <-bus.readyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription was never confirmed")
	}

	// 关闭客户端连接池 → pubsub 消息通道关闭 → 循环必须经由 !ok 分支退出。
	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() error = %v", err)
	}
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("subscribeLoop did not exit after the pubsub channel closed")
	}
}
