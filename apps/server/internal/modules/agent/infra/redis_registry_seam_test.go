package infra

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	agentapp "servify/apps/server/internal/modules/agent/application"
)

func TestStoreInRedisMarshalFailure(t *testing.T) {
	orig := marshalAgentRuntime
	marshalAgentRuntime = func(v interface{}) ([]byte, error) { return nil, errors.New("marshal boom") }
	defer func() { marshalAgentRuntime = orig }()

	// marshal 失败在触达 Redis 客户端之前返回，零值 registry 即可。
	r := &RedisRegistry{}
	err := r.storeInRedis(7, agentapp.AgentRuntimeDTO{UserID: 7})
	if err == nil || !strings.Contains(err.Error(), "marshal boom") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestSyncLoopExitsWhenStatusChannelCloses(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ctx, cancel := context.WithCancel(context.Background())
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	// 直接构造（不走 NewRedisRegistry）避免后台 goroutine 提前启动，
	// 使 statusCh 注入先于 syncLoop 读取（无数据竞争）。
	r := &RedisRegistry{
		client:     client,
		logger:     logger,
		localCache: NewInMemoryRegistry(),
		ctx:        ctx,
		cancel:     cancel,
	}
	t.Cleanup(func() {
		cancel()
		client.Close()
	})

	// 注入已关闭的状态通道：ctx 未取消时 select 的唯一就绪分支是
	// ch 关闭，syncLoop 必须经 !ok 分支退出（与 go-redis 在连接池
	// 关闭后关闭消息通道的行为一致）。
	closed := make(chan *redis.Message)
	close(closed)
	r.statusCh = closed

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.syncLoop()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("syncLoop did not exit via the closed status channel")
	}
}
