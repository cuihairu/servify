package infra

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
)

// 本文件锁定状态消息的合并语义：syncLoop 的 handleStatusChange / syncFromRedis
// 拿到的 Redis 快照只是 presence 通知，不得覆盖本实例的负载记账与会话表。
// 原实现用 localCache.GoOnline(dtoToProfile(...)) 整条重建，而 pub/sub 广播
// 包含发布者自己：online 回灌落在 AssignSession 的本地变更与其 Redis 同步
// 之间时，本地负载被重置成过期快照，下一次分配把回灌值写回共享键（CI 实证：
// 两次 AssignSession 后共享键 load=1 而非 2），等价于把
// docs/multi-instance-boundary.md 第 1 节的「负载记账是共享状态」静默打破。

// TestStatusEchoDoesNotClobberLocalLoad：发布者自己的 online 回灌到达时，
// 本地已变更但尚未同步到 Redis 的负载与会话必须存活。
func TestStatusEchoDoesNotClobberLocalLoad(t *testing.T) {
	cluster := newMultiInstanceCluster(t)
	seedCoverageAgent(t, cluster.db, 7)

	_, err := cluster.agentA.GoOnline(multiInstanceProfile(7))
	require.NoError(t, err)

	// 只动本地、不同步 Redis：复现「本地变更已完成、Redis 同步尚不可见」
	// 的竞态窗口内状态（此刻共享快照 load=0，本地 load=1）。
	_, err = cluster.agentA.localCache.AssignSession(7, &models.Session{ID: "s-1", Status: "active"})
	require.NoError(t, err)

	// 自己发布的 online 消息回到自己（pub/sub 广播含发布者）。
	cluster.agentA.handleStatusChange("online:7")

	dto, ok := cluster.agentA.Get(7)
	require.True(t, ok)
	require.Equal(t, 1, dto.CurrentChatLoad, "status echo must not clobber local load accounting")

	// 会话表同样保留：第二条分配后本地与共享键都是 2。
	assigned, err := cluster.agentA.AssignSession(7, &models.Session{ID: "s-2", Status: "active"})
	require.NoError(t, err)
	require.Equal(t, 2, assigned.CurrentChatLoad)
	require.Equal(t, 2, cluster.sharedLoad(t, 7))

	released, ok := cluster.agentA.ReleaseSession(7, "s-1")
	require.True(t, ok)
	require.Equal(t, 1, released.CurrentChatLoad, "released session must still be accounted")
}

// TestStatusEchoInsertsUnseenAgentFromSnapshot：本实例从未见过的 agent，
// 状态消息触发从共享快照整条插入（跨实例可见性的既有语义）。
func TestStatusEchoInsertsUnseenAgentFromSnapshot(t *testing.T) {
	cluster := newMultiInstanceCluster(t)

	// 另一实例把 agent 8 的运行态写进共享 Redis（不经 GoOnline，避免
	// pub/sub 异步消息与本测试竞争时序）。
	now := time.Now()
	require.NoError(t, cluster.agentB.storeInRedis(8, agentapp.AgentRuntimeDTO{
		UserID:             8,
		Username:           "redis-agent",
		Name:               "Redis Agent",
		Department:         "support",
		Status:             "online",
		MaxChatConcurrency: 3,
		CurrentChatLoad:    1,
		ConnectedAt:        now,
		LastActivity:       now,
	}))

	cluster.agentA.handleStatusChange("online:8")

	dto, ok := cluster.agentA.Get(8)
	require.True(t, ok)
	require.Equal(t, 1, dto.CurrentChatLoad, "snapshot load must be inserted for an unseen agent")
	require.Equal(t, "support", dto.Department)
	require.Equal(t, "online", dto.Status)
}

// TestMergeRemoteInsertFillsMissingTimestampsAndStatus：插入路径的兜底分支。
func TestMergeRemoteInsertFillsMissingTimestampsAndStatus(t *testing.T) {
	reg := NewInMemoryRegistry()

	reg.MergeRemote(agentapp.AgentRuntimeDTO{UserID: 5})
	inserted, ok := reg.Get(5)
	require.True(t, ok)
	require.False(t, inserted.ConnectedAt.IsZero(), "zero ConnectedAt must fall back to now")
	require.False(t, inserted.LastActivity.IsZero(), "zero LastActivity must fall back to now")
	require.Equal(t, "online", inserted.Status, "empty status must fall back to online")

	connected := time.Unix(1000, 0)
	active := time.Unix(1001, 0)
	reg.MergeRemote(agentapp.AgentRuntimeDTO{UserID: 6, Status: "busy", ConnectedAt: connected, LastActivity: active, CurrentChatLoad: 2})
	other, ok := reg.Get(6)
	require.True(t, ok)
	require.Equal(t, "busy", other.Status)
	require.Equal(t, connected.Unix(), other.ConnectedAt.Unix())
	require.Equal(t, active.Unix(), other.LastActivity.Unix())
	require.Equal(t, 2, other.CurrentChatLoad)
}

// TestMergeRemotePreservesLocalAccounting：合并路径保留本地负载/会话/上线
// 时间，元数据以远端为准。
func TestMergeRemotePreservesLocalAccounting(t *testing.T) {
	reg := NewInMemoryRegistry()
	_, err := reg.GoOnline(multiInstanceProfile(5))
	require.NoError(t, err)
	_, err = reg.AssignSession(5, &models.Session{ID: "s-1", Status: "active"})
	require.NoError(t, err)
	before, _ := reg.Get(5)

	reg.MergeRemote(agentapp.AgentRuntimeDTO{
		UserID:             5,
		Username:           "redis-agent-b",
		Name:               "Redis Agent B",
		Department:         "billing",
		Skills:             []string{"vip"},
		Status:             "busy",
		MaxChatConcurrency: 9,
		Rating:             4.9,
		CurrentChatLoad:    0,
		CurrentVoiceLoad:   0,
		ConnectedAt:        time.Unix(2000, 0),
	})

	merged, ok := reg.Get(5)
	require.True(t, ok)
	require.Equal(t, 1, merged.CurrentChatLoad, "local load must survive the merge")
	require.Equal(t, "busy", merged.Status)
	require.Equal(t, "billing", merged.Department)
	require.Equal(t, []string{"vip"}, merged.Skills)
	require.Equal(t, 9, merged.MaxChatConcurrency)
	require.Equal(t, before.ConnectedAt.Unix(), merged.ConnectedAt.Unix(), "local connected-at must survive the merge")

	released, ok := reg.ReleaseSession(5, "s-1")
	require.True(t, ok)
	require.Equal(t, 0, released.CurrentChatLoad, "sessions must survive the merge")
}
