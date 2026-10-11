package infra

import (
	"testing"

	"github.com/stretchr/testify/require"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

// Redis 连接失效后，本地缓存镜像写（storeInRedis/removeFromRedis）只告警不阻断：
// 主路径数据在本地缓存与数据库，Redis 仅做跨实例同步（2026-10-11 errcheck 收敛）。
// 各分支必须真实执行——用 client.Close() 注入确定性故障。
func TestRedisRegistryMirrorWritesWarnOnRedisFailure(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	seedCoverageAgent(t, db, 8)

	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, Username: "a7", Name: "A7", MaxChatConcurrency: 2})
	require.NoError(t, err)
	_, err = registry.GoOnline(agentdomain.AgentProfile{UserID: 8, Username: "a8", Name: "A8", MaxChatConcurrency: 2})
	require.NoError(t, err)

	_, err = registry.AssignSession(7, &models.Session{ID: "sess-t"})
	require.NoError(t, err)

	require.NoError(t, client.Close()) // 之后所有 Redis 操作确定性失败

	from := uint(7)
	registry.ApplyTransfer("sess-t", &from, 8) // 双端 storeInRedis 告警
	_, err = registry.AssignSession(8, &models.Session{ID: "sess-2"})
	require.NoError(t, err)
	_, ok := registry.ReleaseSession(8, "sess-2")
	require.True(t, ok)
	registry.UpdateStatus(7, agentdomain.PresenceStatusOffline)
	registry.ApplyTransfer("sess-3", nil, 8)
	registry.GoOffline(7) // removeFromRedis 管道 Exec 告警
}
