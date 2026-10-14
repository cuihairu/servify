package infra

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

// 本文件是 docs/multi-instance-boundary.md 第 1 节（Agent presence / load /
// assignment —— 多实例安全）的测试锚点。原始锚定测试
// services/agent_service_redis_integration_test.go 已随 P3-2 internal/services
// 移除而删除，这里按模块现结构补回等价证据：两个 RedisRegistry 实例共享同一
// Redis 与同一 DB 时，互相观测 presence 与 transfer load 的行为必须成立。
// 若未来把运行态退回进程内存储（例如换回 InMemoryRegistry 或不再共享 Redis
// 键），本文件会失败，届时必须同步更新边界文档与 deployment.md 部署口径。

// multiInstanceCluster 是「多副本共享同一 Redis + DB」的最小夹具：mr 供所有
// 实例共用，每个实例各自持有独立 redis 连接（对齐真实多副本：每进程自己的
// 连接池与后台 goroutine）。
type multiInstanceCluster struct {
	mr      *miniredis.Miniredis
	db      *gorm.DB
	agentA  *RedisRegistry
	agentB  *RedisRegistry
	clients []*redis.Client
}

func newMultiInstanceCluster(t *testing.T) *multiInstanceCluster {
	t.Helper()
	cluster := &multiInstanceCluster{mr: miniredis.RunT(t), db: newAgentCoverageDB(t)}
	cluster.agentA = cluster.newInstance(t)
	cluster.agentB = cluster.newInstance(t)
	return cluster
}

// newInstance 按一个新副本装配 RedisRegistry。
func (c *multiInstanceCluster) newInstance(t *testing.T) *RedisRegistry {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: c.mr.Addr()})
	c.clients = append(c.clients, client)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	registry := NewRedisRegistry(client, c.db, logger)
	t.Cleanup(func() {
		registry.cancel()
		_ = client.Close() // 测试收尾，关闭错误无从上报
	})
	return registry
}

// waitSubscribed 等到 count 个实例的 syncLoop 都已在状态频道上订阅就绪，
// 之后调用方再触发写入。订阅就绪前发布的消息不会送达（见 syncLoop 的
// Receive 确认），未就绪即断言传播会把「订阅没建立」误判成「语义变了」。
func (c *multiInstanceCluster) waitSubscribed(t *testing.T, count int) {
	t.Helper()
	waitEventually(t, 5*time.Second, func() bool {
		return c.mr.PubSubNumSub(agentStatusChannel)[agentStatusChannel] == count
	}, "instances never subscribed to the agent status channel")
}

// localCached 返回实例本地缓存中的运行态（不经 Redis 回落），只有状态频道
// 消息与启动全量补同步会写这里：断言这一层才能把「跨实例传播」与「读共享键」
// 两条路径分开。
func localCached(registry *RedisRegistry, userID uint) (agentapp.AgentRuntimeDTO, bool) {
	return registry.localCache.Get(userID)
}

// onlineMembers 返回共享 set 中的在线 user_id（跨实例唯一的 presence 真源）。
func (c *multiInstanceCluster) onlineMembers(t *testing.T) map[string]struct{} {
	t.Helper()
	ids, err := c.clients[0].SMembers(context.Background(), agentSetKey).Result()
	require.NoError(t, err)
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

// sharedLoad 直接读共享键里的 current_load，绕开任何实例的本地缓存，
// 用于断言「负载记账的写侧是共享状态而非进程内计数」。
func (c *multiInstanceCluster) sharedLoad(t *testing.T, userID uint) int {
	t.Helper()
	raw, err := c.clients[0].HGet(context.Background(), agentRuntimeKey(userID), "data").Result()
	require.NoError(t, err)
	var dto agentapp.AgentRuntimeDTO
	require.NoError(t, json.Unmarshal([]byte(raw), &dto))
	return dto.CurrentChatLoad
}

func multiInstanceProfile(userID uint) agentdomain.AgentProfile {
	return agentdomain.AgentProfile{
		UserID:             userID,
		Username:           "redis-agent",
		Name:               "Redis Agent",
		Department:         "support",
		Skills:             []string{"billing"},
		MaxChatConcurrency: 3,
		Rating:             4.5,
		AvgResponseTime:    30,
	}
}

// TestRedisRegistryPresenceIsObservedAcrossInstances 锁定 presence 的跨实例
// 可见性：A 上线/下线都经状态频道推到 B 的本地缓存（不只是 B 读共享键时的
// 回落），共享 set、DB 行与 B 的 List() 三条面同步收敛。
func TestRedisRegistryPresenceIsObservedAcrossInstances(t *testing.T) {
	cluster := newMultiInstanceCluster(t)
	seedCoverageAgent(t, cluster.db, 7)

	// 两实例都订阅就绪后再写入：此时 Redis 为空，B 的启动全量补同步无事可做，
	// B 本地缓存里出现 7 只能来自 A 发布的状态消息。
	cluster.waitSubscribed(t, 2)
	time.Sleep(100 * time.Millisecond)

	_, err := cluster.agentA.GoOnline(multiInstanceProfile(7))
	require.NoError(t, err)

	waitEventually(t, 5*time.Second, func() bool {
		_, ok := localCached(cluster.agentB, 7)
		return ok
	}, "instance B never received the online status change of agent 7")

	got, ok := cluster.agentB.Get(7)
	require.True(t, ok)
	require.Equal(t, uint(7), got.UserID)
	require.Equal(t, string(agentdomain.PresenceStatusOnline), got.Status)
	require.Equal(t, "support", got.Department)
	require.Equal(t, []string{"billing"}, got.Skills)

	// 共享 set 与 DB 行是跨实例的权威 presence 真源。
	_, online := cluster.onlineMembers(t)["7"]
	require.True(t, online, "online agent must be visible in the shared online set")
	require.Equal(t, "online", agentRow(t, cluster.db, 7).Status)

	// B 的 List() 走共享 set，两实例看到的在线视图一致。
	var listed []uint
	for _, dto := range cluster.agentB.List() {
		listed = append(listed, dto.UserID)
	}
	require.Contains(t, listed, uint(7))

	// 下线同样跨实例收敛：本地缓存清空、共享 set 移除、DB 行落 offline。
	cluster.agentA.GoOffline(7)
	waitEventually(t, 5*time.Second, func() bool {
		_, ok := localCached(cluster.agentB, 7)
		return !ok
	}, "instance B still keeps agent 7 cached after it went offline")

	_, ok = cluster.agentB.Get(7)
	require.False(t, ok)
	_, online = cluster.onlineMembers(t)["7"]
	require.False(t, online, "offline agent must leave the shared online set")
	require.Equal(t, "offline", agentRow(t, cluster.db, 7).Status)
}

// TestRedisRegistryTransferLoadIsSharedAcrossInstances 锁定会话转接后的负载
// 记账落在共享 Redis：分配 / 转接 / 释放产生的 current_load 对其他实例可见
// （没有本地缓存的实例读路径回落到共享键，后加入副本的启动全量补同步即走
// 这条路径）。
func TestRedisRegistryTransferLoadIsSharedAcrossInstances(t *testing.T) {
	cluster := newMultiInstanceCluster(t)
	seedCoverageAgent(t, cluster.db, 7)
	seedCoverageAgent(t, cluster.db, 8)

	_, err := cluster.agentA.GoOnline(multiInstanceProfile(7))
	require.NoError(t, err)
	_, err = cluster.agentA.GoOnline(multiInstanceProfile(8))
	require.NoError(t, err)

	assigned, err := cluster.agentA.AssignSession(7, &models.Session{ID: "s-1", Status: "active"})
	require.NoError(t, err)
	require.Equal(t, 1, assigned.CurrentChatLoad)
	_, err = cluster.agentA.AssignSession(7, &models.Session{ID: "s-2", Status: "active"})
	require.NoError(t, err)
	require.Equal(t, 2, cluster.sharedLoad(t, 7))

	// 转接：来源坐席减 1、目标坐席加 1（负载随会话迁移，不是每实例各自计数）。
	fromAgent, toAgent := uint(7), uint(8)
	cluster.agentA.ApplyTransfer("s-2", &fromAgent, toAgent)
	require.Equal(t, 1, cluster.sharedLoad(t, 7))
	require.Equal(t, 1, cluster.sharedLoad(t, 8))

	// 后加入的实例本地缓存为空，读路径回落到共享 Redis。
	observer := cluster.newInstance(t)
	waitEventually(t, 5*time.Second, func() bool {
		dto, ok := observer.Get(7)
		return ok && dto.CurrentChatLoad == 1
	}, "another instance never observed the transferred load of agent 7")
	got8, ok := observer.Get(8)
	require.True(t, ok)
	require.Equal(t, 1, got8.CurrentChatLoad, "transfer target load must be visible to other instances")

	// 释放同样写回共享状态。
	released, ok := cluster.agentA.ReleaseSession(7, "s-1")
	require.True(t, ok)
	require.Equal(t, 0, released.CurrentChatLoad)
	waitEventually(t, 5*time.Second, func() bool {
		return cluster.sharedLoad(t, 7) == 0
	}, "shared runtime state never reflected the released load")
}

// TestRedisRegistryCrashedInstanceStateExpiresByTTL 锁定文档的崩溃收敛声明：
// 实例崩溃（心跳停止）后其运行态键随 TTL 过期自动消失，任何之后读共享状态的
// 实例都读不到该运行态即视为不在线，不依赖额外清理动作（agentRuntimeMaintenance
// 的 cleanupInactiveAgents 只是幂等兜底）。
//
// 断言用「后加入的实例」而非崩溃时已在运行的实例：后者的本地缓存读优先
// （Get 先命中 localCache），只有 presence 事件会刷新它，而崩溃方已无事件可发。
// 那是当前实现的既定读路径，本测试不去锁它。
func TestRedisRegistryCrashedInstanceStateExpiresByTTL(t *testing.T) {
	cluster := newMultiInstanceCluster(t)
	seedCoverageAgent(t, cluster.db, 9)

	cluster.waitSubscribed(t, 2)

	_, err := cluster.agentA.GoOnline(multiInstanceProfile(9))
	require.NoError(t, err)

	key := agentRuntimeKey(9)
	ttl, err := cluster.clients[0].TTL(context.Background(), key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0), "runtime key must carry a heartbeat TTL")

	// 模拟实例崩溃：心跳 goroutine 停止续期。
	cluster.agentA.cancel()
	cluster.mr.FastForward(agentHeartbeatTTL*2 + time.Second)

	exists, err := cluster.clients[0].Exists(context.Background(), key).Result()
	require.NoError(t, err)
	require.Zero(t, exists, "crashed instance runtime key must expire by TTL")

	// 后加入的实例：启动全量补同步读不到已过期的运行态。
	survivor := cluster.newInstance(t)
	_, ok := survivor.Get(9)
	require.False(t, ok, "an instance reading shared state must not serve expired runtime state")
	for _, dto := range survivor.List() {
		require.NotEqual(t, uint(9), dto.UserID, "expired agent must not appear in the shared online view")
	}
}
