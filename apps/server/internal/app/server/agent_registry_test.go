package server

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	agentinfra "servify/apps/server/internal/modules/agent/infra"
)

// TestNewAgentRegistry 覆盖 registry 选择的两个分支：
// 无 Redis → 单机内存态；有 Redis → 多实例共享 registry。
func TestNewAgentRegistry(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	db := newRuntimeTestDB(t)

	if _, ok := newAgentRegistry(db, nil, logger).(*agentinfra.InMemoryRegistry); !ok {
		t.Fatal("expected in-memory registry without redis client")
	}

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close() //nolint:errcheck

	if _, ok := newAgentRegistry(db, client, logger).(*agentinfra.RedisRegistry); !ok {
		t.Fatal("expected redis-backed registry with redis client")
	}
}
