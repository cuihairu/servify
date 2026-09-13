package infra

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	agentapp "servify/apps/server/internal/modules/agent/application"
)

// syncFromRedis 拉取失败必须只记 warn 并返回，不影响本地缓存。
func TestRedisRegistrySyncFromRedisRedisError(t *testing.T) {
	registry, mr, _, _ := newCoverageRedisRegistry(t)
	mr.Close()

	registry.syncFromRedis() // must not panic
	require.Empty(t, registry.localCache.List())
}

// 集合里的非数字成员在启动兜底同步时被跳过；数字成员则从 Redis 拉回本地缓存。
func TestRedisRegistrySyncFromRedisSkipsMalformedMembers(t *testing.T) {
	registry, _, _, client := newCoverageRedisRegistry(t)
	require.NoError(t, client.SAdd(context.Background(), agentSetKey, "abc").Err())
	// 另一实例写入的在线状态：syncFromRedis 应把它恢复进本地缓存。
	require.NoError(t, registry.storeInRedis(8, agentapp.AgentRuntimeDTO{UserID: 8, Username: "remote-agent", MaxChatConcurrency: 2}))
	require.NoError(t, client.SAdd(context.Background(), agentSetKey, 8).Err())

	registry.syncFromRedis()

	dto, ok := registry.localCache.Get(8)
	require.True(t, ok, "numeric member must be restored into the local cache")
	require.Equal(t, uint(8), dto.UserID)
	require.Equal(t, "remote-agent", dto.Username)
	_, ok = registry.localCache.Get(0)
	require.False(t, ok, "malformed member must be skipped")

	members, err := client.SMembers(context.Background(), agentSetKey).Result()
	require.NoError(t, err)
	require.Len(t, members, 2)
}
