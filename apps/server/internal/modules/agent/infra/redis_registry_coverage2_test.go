package infra

import (
	"context"
	"encoding/json"
	"fmt"
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

func agentRuntimeKey(userID uint) string {
	return fmt.Sprintf(agentRuntimeKeyPattern, userID)
}

func newCoverageRedisRegistry(t *testing.T) (*RedisRegistry, *miniredis.Miniredis, *gorm.DB, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db := newAgentCoverageDB(t)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	registry := NewRedisRegistry(client, db, logger)
	t.Cleanup(func() {
		registry.cancel()
		client.Close()
	})
	return registry, mr, db, client
}

func seedCoverageAgent(t *testing.T, db *gorm.DB, userID uint) {
	t.Helper()
	seedCoverageUser(t, db, userID, fmt.Sprintf("redis-agent-%d", userID))
	require.NoError(t, db.Create(&models.Agent{UserID: userID, Status: "offline", MaxConcurrent: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
}

func agentRow(t *testing.T, db *gorm.DB, userID uint) models.Agent {
	t.Helper()
	var agent models.Agent
	require.NoError(t, db.First(&agent, "user_id = ?", userID).Error)
	return agent
}

func redisAgentData(t *testing.T, client *redis.Client, userID uint) agentapp.AgentRuntimeDTO {
	t.Helper()
	raw, err := client.HGet(context.Background(), agentRuntimeKey(userID), "data").Result()
	require.NoError(t, err)
	var dto agentapp.AgentRuntimeDTO
	require.NoError(t, json.Unmarshal([]byte(raw), &dto))
	return dto
}

func waitEventually(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

func TestRedisRegistryGoOnlineSyncsAllStores(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)

	dto, err := registry.GoOnline(agentdomain.AgentProfile{
		UserID: 7, Username: "redis-agent", Name: "Redis Agent", Department: "support",
		Skills: []string{"billing"}, MaxChatConcurrency: 2, Rating: 4.5, AvgResponseTime: 30,
	})
	require.NoError(t, err)
	require.Equal(t, uint(7), dto.UserID)
	require.Equal(t, "Redis Agent", dto.Name)
	require.Equal(t, "support", dto.Department)
	require.Equal(t, []string{"billing"}, dto.Skills)
	require.Equal(t, string(agentdomain.PresenceStatusOnline), dto.Status)
	require.Equal(t, 2, dto.MaxChatConcurrency)
	require.Equal(t, 4.5, dto.Rating)
	require.False(t, dto.LastActivity.IsZero())
	require.False(t, dto.ConnectedAt.IsZero())

	stored := agentRow(t, db, 7)
	require.Equal(t, "online", stored.Status)
	require.NotNil(t, stored.ConnectedAt)

	storedDTO := redisAgentData(t, client, 7)
	require.Equal(t, uint(7), storedDTO.UserID)
	require.Equal(t, string(agentdomain.PresenceStatusOnline), storedDTO.Status)
	isMember, err := client.SIsMember(context.Background(), agentSetKey, 7).Result()
	require.NoError(t, err)
	require.True(t, isMember)

	got, ok := registry.Get(7)
	require.True(t, ok)
	require.Equal(t, uint(7), got.UserID)
}

func TestRedisRegistryGoOnlinePersistFailure(t *testing.T) {
	registry, _, db, _ := newCoverageRedisRegistry(t)
	require.NoError(t, db.Migrator().DropTable("agents"))

	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7})
	require.ErrorContains(t, err, "persist agent state")
}

func TestRedisRegistryGoOnlineToleratesRedisFailure(t *testing.T) {
	registry, mr, db, _ := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	mr.Close()

	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err, "DB write succeeded, redis failure must be tolerated")

	require.Equal(t, "online", agentRow(t, db, 7).Status)
	registry.localCache.GoOffline(7) // force the redis fallback path
	_, ok := registry.Get(7)
	require.False(t, ok, "redis lookup must fail when redis is down")
}

func TestRedisRegistryGoOfflineClearsAllStores(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)

	registry.GoOffline(7)

	stored := agentRow(t, db, 7)
	require.Equal(t, "offline", stored.Status)
	require.Nil(t, stored.ConnectedAt)

	exists, err := client.Exists(context.Background(), agentRuntimeKey(7)).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
	isMember, err := client.SIsMember(context.Background(), agentSetKey, 7).Result()
	require.NoError(t, err)
	require.False(t, isMember)

	_, ok := registry.Get(7)
	require.False(t, ok)
}

func TestRedisRegistryUpdateStatusSyncsStores(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)

	registry.UpdateStatus(7, agentdomain.PresenceStatusBusy)

	require.Equal(t, "busy", agentRow(t, db, 7).Status)
	require.Equal(t, string(agentdomain.PresenceStatusBusy), redisAgentData(t, client, 7).Status)

	// Unknown agent: no cache entry, so only the DB update path runs.
	registry.UpdateStatus(999, agentdomain.PresenceStatusAway)
	require.Equal(t, "busy", agentRow(t, db, 7).Status, "unrelated row must stay untouched")
}

func TestRedisRegistryAssignSession(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)

	dto, err := registry.AssignSession(7, &models.Session{ID: "sess-1"})
	require.NoError(t, err)
	require.Equal(t, 1, dto.CurrentChatLoad)
	require.Equal(t, 1, redisAgentData(t, client, 7).CurrentChatLoad)

	_, err = registry.AssignSession(999, &models.Session{ID: "sess-2"})
	require.EqualError(t, err, "agent 999 is not online")
}

func TestRedisRegistryReleaseSession(t *testing.T) {
	registry, _, db, _ := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)
	_, err = registry.AssignSession(7, &models.Session{ID: "sess-1"})
	require.NoError(t, err)

	dto, ok := registry.ReleaseSession(7, "sess-1")
	require.True(t, ok)
	require.Equal(t, 0, dto.CurrentChatLoad)

	_, ok = registry.ReleaseSession(999, "sess-1")
	require.False(t, ok)
}

func TestRedisRegistryApplyTransferSyncsBothAgents(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	seedCoverageAgent(t, db, 8)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)
	_, err = registry.GoOnline(agentdomain.AgentProfile{UserID: 8, MaxChatConcurrency: 2})
	require.NoError(t, err)
	waitForRegistryPubsubDrain(t, client)
	_, err = registry.AssignSession(7, &models.Session{ID: "sess-1"})
	require.NoError(t, err)

	from := uint(7)
	registry.ApplyTransfer("sess-1", &from, 8)

	source, ok := registry.Get(7)
	require.True(t, ok)
	require.Equal(t, 0, source.CurrentChatLoad)
	target, ok := registry.Get(8)
	require.True(t, ok)
	require.Equal(t, 1, target.CurrentChatLoad)
	require.Equal(t, 0, redisAgentData(t, client, 7).CurrentChatLoad)
	require.Equal(t, 1, redisAgentData(t, client, 8).CurrentChatLoad)

	// Missing source agent and missing target must not panic.
	missing := uint(999)
	registry.ApplyTransfer("sess-1", &missing, 8)
	registry.ApplyTransfer("sess-1", nil, 999)
}

func TestRedisRegistryGetFallsBackToRedis(t *testing.T) {
	registry, _, _, _ := newCoverageRedisRegistry(t)

	// State stored only in redis, as if another instance wrote it.
	require.NoError(t, registry.storeInRedis(7, agentapp.AgentRuntimeDTO{UserID: 7, Username: "redis-agent", MaxChatConcurrency: 2}))

	dto, ok := registry.Get(7)
	require.True(t, ok)
	require.Equal(t, uint(7), dto.UserID)
	require.Equal(t, "redis-agent", dto.Username)

	_, ok = registry.Get(42)
	require.False(t, ok, "unknown agent must not be found")

	// Corrupted payload must surface as a miss.
	require.NoError(t, registry.client.HSet(context.Background(), agentRuntimeKey(7), "data", "not-json").Err())
	registry.localCache.GoOffline(7)
	_, ok = registry.Get(7)
	require.False(t, ok)
}

func TestRedisRegistryList(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	seedCoverageAgent(t, db, 8)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)
	_, err = registry.GoOnline(agentdomain.AgentProfile{UserID: 8, MaxChatConcurrency: 2})
	require.NoError(t, err)

	listed := registry.List()
	require.Len(t, listed, 2)

	// Non-numeric members are skipped.
	require.NoError(t, client.SAdd(context.Background(), agentSetKey, "abc").Err())
	listed = registry.List()
	require.Len(t, listed, 2)
}

func TestRedisRegistryListFallsBackToLocalCacheOnRedisError(t *testing.T) {
	registry, mr, db, _ := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)
	mr.Close()

	listed := registry.List()
	require.Len(t, listed, 1)
	require.Equal(t, uint(7), listed[0].UserID)
}

func TestRedisRegistrySyncLoopMirrorsRemoteChanges(t *testing.T) {
	registry1, _, db, client := newCoverageRedisRegistry(t)
	registry2 := NewRedisRegistry(client, db, nil) // nil logger falls back to the standard logger
	t.Cleanup(registry2.cancel)
	seedCoverageAgent(t, db, 7)

	_, err := registry1.GoOnline(agentdomain.AgentProfile{UserID: 7, Username: "redis-agent", MaxChatConcurrency: 2})
	require.NoError(t, err)

	waitEventually(t, 3*time.Second, func() bool {
		_, ok := registry2.localCache.Get(7)
		return ok
	}, "second instance must mirror online event")
	dto, ok := registry2.Get(7)
	require.True(t, ok)
	require.Equal(t, "redis-agent", dto.Username)

	registry1.GoOffline(7)
	waitEventually(t, 3*time.Second, func() bool {
		_, ok := registry2.localCache.Get(7)
		return !ok
	}, "second instance must mirror offline event")
}

func TestRedisRegistryHandleStatusChangeOnlineBranches(t *testing.T) {
	registry, _, _, _ := newCoverageRedisRegistry(t)

	// Not stored in redis yet: the online event must be ignored.
	registry.handleStatusChange("online:5")
	_, ok := registry.localCache.Get(5)
	require.False(t, ok)

	// Malformed user id must be ignored.
	registry.handleStatusChange("online:abc")
	require.Empty(t, registry.localCache.List())

	require.NoError(t, registry.storeInRedis(5, agentapp.AgentRuntimeDTO{UserID: 5, Username: "remote-agent", MaxChatConcurrency: 2}))
	registry.handleStatusChange("online:5")
	dto, ok := registry.localCache.Get(5)
	require.True(t, ok)
	require.Equal(t, uint(5), dto.UserID)
	require.Equal(t, "remote-agent", dto.Username)
}

func TestRedisRegistryRefreshTTLs(t *testing.T) {
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)

	// Shrink the TTL, then let the heartbeat refresh restore it.
	require.NoError(t, client.Expire(context.Background(), agentRuntimeKey(7), 10*time.Second).Err())
	registry.refreshTTLs()

	ttl, err := client.TTL(context.Background(), agentRuntimeKey(7)).Result()
	require.NoError(t, err)
	require.Greater(t, int64(ttl/time.Second), int64(50), "expected heartbeat TTL to be refreshed")
}

func TestRedisRegistryHeartbeatLoopRefreshesTTL(t *testing.T) {
	t.Parallel()
	registry, _, db, client := newCoverageRedisRegistry(t)
	seedCoverageAgent(t, db, 7)
	_, err := registry.GoOnline(agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2})
	require.NoError(t, err)

	// The ticker fires every 10s; wait past it and confirm the TTL was renewed.
	time.Sleep(11 * time.Second)
	ttl, err := client.TTL(context.Background(), agentRuntimeKey(7)).Result()
	require.NoError(t, err)
	require.Greater(t, int64(ttl/time.Second), int64(50), "expected heartbeat to renew the TTL")
}

// waitForRegistryPubsubDrain blocks until the registry's syncLoop has consumed
// all queued status messages. It publishes a sentinel on the status channel and
// waits for it on a test subscriber; since pubsub delivery is ordered, the
// sentinel trailing the "online" messages lets us know the queue is drained.
func waitForRegistryPubsubDrain(t *testing.T, client *redis.Client) {
	t.Helper()
	pubsub := client.Subscribe(context.Background(), agentStatusChannel)
	defer pubsub.Close()
	require.NoError(t, client.Publish(context.Background(), agentStatusChannel, "sentinel:0").Err())
	msgCh := pubsub.Channel()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-msgCh:
			if msg.Payload == "sentinel:0" {
				time.Sleep(20 * time.Millisecond)
				return
			}
		case <-timeout:
			return
		}
	}
}
