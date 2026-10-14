package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

const (
	// Redis key patterns
	agentRuntimeKeyPattern = "servify:agent:runtime:%d" // Hash per agent
	agentSetKey            = "servify:agent:online"     // Set of online user IDs
	agentStatusChannel     = "servify:agent:status"     // Pub/Sub for status changes
	agentHeartbeatTTL      = 30 * time.Second
)

// RedisRegistry implements RuntimeRegistry with Redis backing.
// It provides multi-instance synchronization by storing state in Redis
// and publishing status changes via Pub/Sub.
type RedisRegistry struct {
	client     *redis.Client
	db         *gorm.DB
	logger     *logrus.Logger
	localCache *InMemoryRegistry // For fast local reads

	// statusCh 为测试注入点：非 nil 时替代 pubsub.Channel() 作为状态消息
	// 来源，用于确定性覆盖通道关闭（!ok）分支；生产恒为 nil。
	statusCh <-chan *redis.Message

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRedisRegistry creates a new Redis-backed registry.
func NewRedisRegistry(client *redis.Client, db *gorm.DB, logger *logrus.Logger) *RedisRegistry {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	ctx, cancel := context.WithCancel(context.Background())

	registry := &RedisRegistry{
		client:     client,
		db:         db,
		logger:     logger,
		localCache: NewInMemoryRegistry(),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start background routines
	go registry.syncLoop()
	go registry.heartbeatLoop()

	return registry
}

// GoOnline marks an agent as online.
func (r *RedisRegistry) GoOnline(profile agentdomain.AgentProfile) (agentapp.AgentRuntimeDTO, error) {
	now := time.Now()
	dto := r.buildRuntimeDTO(profile, now)

	// 1. Persist to database
	if err := r.persistOnlineState(profile.UserID, now); err != nil {
		return agentapp.AgentRuntimeDTO{}, fmt.Errorf("persist agent state: %w", err)
	}

	// 2. Store in Redis + 4. 镜像本地缓存，整体持 r.mu：syncLoop 的状态合并
	//    也持 r.mu，本实例写共享状态时不可被回灌/重建穿插。
	r.mu.Lock()
	if err := r.storeInRedis(profile.UserID, dto); err != nil {
		r.logger.Warnf("redis write failed: %v", err)
		// Don't fail - DB write succeeded
	}
	_, _ = r.localCache.GoOnline(profile) // 内存表永不报错，best-effort 检查
	r.mu.Unlock()

	// 3. Publish status change
	r.publishStatusChange("online", profile.UserID)

	return dto, nil
}

// GoOffline marks an agent as offline.
func (r *RedisRegistry) GoOffline(userID uint) {
	now := time.Now()

	// 1. Persist to database
	r.db.Model(&models.Agent{}).
		Where("user_id = ?", userID).
		Updates(map[string]interface{}{
			"status":           string(agentdomain.PresenceStatusOffline),
			"last_activity_at": now,
			"connected_at":     nil,
		})

	// 2. Remove from Redis + 4. 清本地缓存，整体持 r.mu：否则并发 AssignSession
	//    的 storeInRedis 可在下线删除之后把运行态复活回共享键。
	r.mu.Lock()
	r.removeFromRedis(userID)
	r.localCache.GoOffline(userID)
	r.mu.Unlock()

	// 3. Publish status change
	r.publishStatusChange("offline", userID)
}

// UpdateStatus updates an agent's presence status.
func (r *RedisRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {
	now := time.Now()

	// 1. Persist to database
	r.db.Model(&models.Agent{}).
		Where("user_id = ?", userID).
		Updates(map[string]interface{}{
			"status":           string(status),
			"last_activity_at": now,
		})

	// 2. Update in Redis (if online) + 3. 更新本地缓存，整体持 r.mu
	//    （与 syncLoop 状态合并互斥，见 GoOnline 注释）。
	r.mu.Lock()
	if dto, ok := r.localCache.Get(userID); ok {
		dto.Status = string(status)
		dto.LastActivity = now
		if err := r.storeInRedis(userID, dto); err != nil {
			r.logger.Warnf("redis write failed: %v", err)
		}
	}
	r.localCache.UpdateStatus(userID, status)
	r.mu.Unlock()
}

// AssignSession assigns a session to an agent.
func (r *RedisRegistry) AssignSession(userID uint, session *models.Session) (agentapp.AgentRuntimeDTO, error) {
	// 本地变更与 Redis 同步整体持 r.mu：syncLoop 的状态合并读共享键，若在
	// 两者之间穿插，会把尚未同步的负载增量重置成过期快照（见 MergeRemote）。
	r.mu.Lock()
	dto, err := r.localCache.AssignSession(userID, session)
	if err != nil {
		r.mu.Unlock()
		return dto, err
	}
	if err := r.storeInRedis(userID, dto); err != nil {
		r.mu.Unlock()
		r.logger.Warnf("redis write failed: %v", err)
		return dto, nil
	}
	r.mu.Unlock()
	return dto, nil
}

// ReleaseSession releases a session from an agent.
func (r *RedisRegistry) ReleaseSession(userID uint, sessionID string) (agentapp.AgentRuntimeDTO, bool) {
	// 持锁口径同 AssignSession。
	r.mu.Lock()
	dto, ok := r.localCache.ReleaseSession(userID, sessionID)
	if !ok {
		r.mu.Unlock()
		return dto, false
	}
	if err := r.storeInRedis(userID, dto); err != nil {
		r.mu.Unlock()
		r.logger.Warnf("redis write failed: %v", err)
		return dto, true
	}
	r.mu.Unlock()
	return dto, true
}

// ApplyTransfer transfers a session between agents.
func (r *RedisRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {
	// 持锁口径同 AssignSession：转接的双 agent 记账与同步不可被状态合并穿插。
	r.mu.Lock()
	defer r.mu.Unlock()
	r.localCache.ApplyTransfer(sessionID, fromAgentID, toAgentID)

	// Sync both agents to Redis
	if fromAgentID != nil {
		if dto, ok := r.localCache.Get(*fromAgentID); ok {
			if err := r.storeInRedis(*fromAgentID, dto); err != nil {
				r.logger.Warnf("redis write failed: %v", err)
			}
		}
	}
	if dto, ok := r.localCache.Get(toAgentID); ok {
		if err := r.storeInRedis(toAgentID, dto); err != nil {
			r.logger.Warnf("redis write failed: %v", err)
		}
	}
}

// Get retrieves agent runtime state.
func (r *RedisRegistry) Get(userID uint) (agentapp.AgentRuntimeDTO, bool) {
	// Try local cache first for speed
	if dto, ok := r.localCache.Get(userID); ok {
		return dto, true
	}

	// Fallback to Redis
	dto, err := r.getFromRedis(userID)
	if err != nil {
		return agentapp.AgentRuntimeDTO{}, false
	}
	return *dto, true
}

// List returns all online agents.
func (r *RedisRegistry) List() []agentapp.AgentRuntimeDTO {
	// Try Redis for consistency across instances
	userIDs, err := r.client.SMembers(r.ctx, agentSetKey).Result()
	if err != nil {
		// Fallback to local cache
		return r.localCache.List()
	}

	var result []agentapp.AgentRuntimeDTO
	for _, userIDStr := range userIDs {
		var userID uint
		if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err == nil {
			if dto, ok := r.Get(userID); ok {
				result = append(result, dto)
			}
		}
	}
	return result
}

// syncLoop listens for status changes from other instances.
func (r *RedisRegistry) syncLoop() {
	pubsub := r.client.Subscribe(r.ctx, agentStatusChannel)
	defer func() { _ = pubsub.Close() }() // 订阅收尾，关闭错误无从上报

	// 等待订阅确认：确认前发布的消息不会送达本实例。
	if _, err := pubsub.Receive(r.ctx); err != nil {
		r.logger.Warnf("redis registry subscribe: %v", err)
		return
	}

	// 订阅就绪后全量补同步一次，覆盖订阅建立前其他实例写入的状态，
	// 否则启动窗口内的 online/offline 事件会永久丢失。
	r.syncFromRedis()

	ch := r.statusCh
	if ch == nil {
		ch = pubsub.Channel()
	}
	for {
		select {
		case <-r.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			r.handleStatusChange(msg.Payload)
		}
	}
}

// syncFromRedis 拉取 Redis 中所有在线 agent 到本地缓存，作为启动兜底。
func (r *RedisRegistry) syncFromRedis() {
	ids, err := r.client.SMembers(r.ctx, agentSetKey).Result()
	if err != nil {
		r.logger.Warnf("redis registry full sync: %v", err)
		return
	}
	for _, idStr := range ids {
		var userID uint
		if _, err := fmt.Sscanf(idStr, "%d", &userID); err != nil {
			continue
		}
		if dto, err := r.getFromRedis(userID); err == nil {
			r.mu.Lock()
			r.localCache.MergeRemote(*dto)
			r.mu.Unlock()
		}
	}
}

// handleStatusChange processes a status change message from another instance.
func (r *RedisRegistry) handleStatusChange(payload string) {
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return
	}
	action := parts[0]
	var userID uint
	if _, err := fmt.Sscanf(parts[1], "%d", &userID); err != nil {
		return
	}

	switch action {
	case "online":
		// Fetch from Redis and merge into local cache：presence 元数据以远端
		// 为准，本地负载/会话保留（发布者自己也收到自己的 online 回灌，
		// 整条重建会把未同步到 Redis 的本地记账重置成过期快照）。
		if dto, err := r.getFromRedis(userID); err == nil {
			r.mu.Lock()
			r.localCache.MergeRemote(*dto)
			r.mu.Unlock()
		}
	case "offline":
		// Update local cache without triggering another publish
		r.mu.Lock()
		r.localCache.GoOffline(userID)
		r.mu.Unlock()
	}
}

// heartbeatLoop periodically refreshes Redis TTL for local agents.
func (r *RedisRegistry) heartbeatLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.refreshTTLs()
		}
	}
}

// refreshTTLs refreshes TTL for all agents in local cache.
func (r *RedisRegistry) refreshTTLs() {
	for _, dto := range r.localCache.List() {
		key := fmt.Sprintf(agentRuntimeKeyPattern, dto.UserID)
		r.client.Expire(r.ctx, key, agentHeartbeatTTL*2)
	}
}

// Helper methods

func (r *RedisRegistry) buildRuntimeDTO(profile agentdomain.AgentProfile, now time.Time) agentapp.AgentRuntimeDTO {
	return agentapp.AgentRuntimeDTO{
		UserID:              profile.UserID,
		Username:            profile.Username,
		Name:                profile.Name,
		Department:          profile.Department,
		Skills:              append([]string(nil), profile.Skills...),
		Status:              string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency:  profile.MaxChatConcurrency,
		MaxVoiceConcurrency: profile.MaxVoiceConcurrency,
		CurrentChatLoad:     profile.CurrentChatLoad,
		CurrentVoiceLoad:    profile.CurrentVoiceLoad,
		Rating:              profile.Rating,
		AvgResponseTime:     profile.AvgResponseTime,
		LastActivity:        now,
		ConnectedAt:         now,
	}
}

func (r *RedisRegistry) persistOnlineState(userID uint, t time.Time) error {
	return r.db.Model(&models.Agent{}).
		Where("user_id = ?", userID).
		Updates(map[string]interface{}{
			"status":           string(agentdomain.PresenceStatusOnline),
			"last_activity_at": t,
			"connected_at":     t,
		}).Error
}

// marshalAgentRuntime 为测试注入点（默认即 json.Marshal）：
// AgentRuntimeDTO 为纯数据结构，序列化恒成功，错误分支仅经注入触发。
var marshalAgentRuntime = json.Marshal

func (r *RedisRegistry) storeInRedis(userID uint, dto agentapp.AgentRuntimeDTO) error {
	data, err := marshalAgentRuntime(dto)
	if err != nil {
		return err
	}

	key := fmt.Sprintf(agentRuntimeKeyPattern, userID)
	pipe := r.client.Pipeline()
	pipe.HSet(r.ctx, key, "data", data)
	pipe.HSet(r.ctx, key, "heartbeat", time.Now().Unix())
	pipe.Expire(r.ctx, key, agentHeartbeatTTL*2)
	pipe.SAdd(r.ctx, agentSetKey, userID)

	_, err = pipe.Exec(r.ctx)
	return err
}

func (r *RedisRegistry) removeFromRedis(userID uint) {
	key := fmt.Sprintf(agentRuntimeKeyPattern, userID)
	pipe := r.client.Pipeline()
	pipe.Del(r.ctx, key)
	pipe.SRem(r.ctx, agentSetKey, userID)
	if _, err := pipe.Exec(r.ctx); err != nil {
		r.logger.Warnf("redis remove pipeline failed: %v", err)
	}
}

func (r *RedisRegistry) getFromRedis(userID uint) (*agentapp.AgentRuntimeDTO, error) {
	key := fmt.Sprintf(agentRuntimeKeyPattern, userID)
	data, err := r.client.HGet(r.ctx, key, "data").Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("agent not found in redis")
	}
	if err != nil {
		return nil, err
	}

	var dto agentapp.AgentRuntimeDTO
	if err := json.Unmarshal([]byte(data), &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (r *RedisRegistry) publishStatusChange(action string, userID uint) {
	r.client.Publish(r.ctx, agentStatusChannel, fmt.Sprintf("%s:%d", action, userID))
}

// Ensure RedisRegistry implements RuntimeRegistry
var _ agentapp.RuntimeRegistry = (*RedisRegistry)(nil)
