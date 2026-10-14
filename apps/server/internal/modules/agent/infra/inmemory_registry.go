package infra

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

type runtimeAgent struct {
	agentapp.AgentRuntimeDTO
	Sessions map[string]*models.Session
}

type InMemoryRegistry struct {
	mu     sync.RWMutex
	agents map[uint]*runtimeAgent
}

func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{agents: make(map[uint]*runtimeAgent)}
}

func (r *InMemoryRegistry) GoOnline(profile agentdomain.AgentProfile) (agentapp.AgentRuntimeDTO, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	item := &runtimeAgent{
		AgentRuntimeDTO: agentapp.AgentRuntimeDTO{
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
		},
		Sessions: make(map[string]*models.Session),
	}
	r.agents[profile.UserID] = item
	return item.AgentRuntimeDTO, nil
}

func (r *InMemoryRegistry) GoOffline(userID uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, userID)
}

func (r *InMemoryRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item, ok := r.agents[userID]; ok {
		item.Status = string(status)
		item.LastActivity = time.Now()
	}
}

func (r *InMemoryRegistry) AssignSession(userID uint, session *models.Session) (agentapp.AgentRuntimeDTO, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.agents[userID]
	if !ok {
		return agentapp.AgentRuntimeDTO{}, fmt.Errorf("agent %d is not online", userID)
	}
	if item.CurrentChatLoad >= item.MaxChatConcurrency {
		return agentapp.AgentRuntimeDTO{}, fmt.Errorf("agent %d is at maximum capacity", userID)
	}
	if item.Status == string(agentdomain.PresenceStatusOffline) {
		return agentapp.AgentRuntimeDTO{}, fmt.Errorf("agent %d is offline", userID)
	}
	item.CurrentChatLoad++
	item.LastActivity = time.Now()
	copySession := *session
	item.Sessions[session.ID] = &copySession
	return item.AgentRuntimeDTO, nil
}

func (r *InMemoryRegistry) ReleaseSession(userID uint, sessionID string) (agentapp.AgentRuntimeDTO, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.agents[userID]
	if !ok {
		return agentapp.AgentRuntimeDTO{}, false
	}
	if item.CurrentChatLoad > 0 {
		item.CurrentChatLoad--
	}
	delete(item.Sessions, sessionID)
	item.LastActivity = time.Now()
	return item.AgentRuntimeDTO, true
}

func (r *InMemoryRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if fromAgentID != nil {
		if item, ok := r.agents[*fromAgentID]; ok {
			if item.CurrentChatLoad > 0 {
				item.CurrentChatLoad--
			}
			delete(item.Sessions, sessionID)
			item.LastActivity = now
		}
	}
	if item, ok := r.agents[toAgentID]; ok {
		item.CurrentChatLoad++
		item.Sessions[sessionID] = &models.Session{ID: sessionID, AgentID: &toAgentID, Status: "active"}
		item.LastActivity = now
	}
}

// MergeRemote 以远端快照合并本地运行态：presence 元数据（身份/技能/并发
// 上限/评分）以远端为准，负载记账、会话表与上线时间保留本地。负载与会话
// 归持有会话的实例所有（AssignSession/ReleaseSession/ApplyTransfer 的写侧），
// 状态消息只是 presence 通知：pub/sub 广播含发布者自己，若用 GoOnline 整条
// 重建，回灌落在本地变更与其 Redis 同步之间时会把未同步的记账重置成过期
// 快照，并在下一次分配时写回共享键。
func (r *InMemoryRegistry) MergeRemote(dto agentapp.AgentRuntimeDTO) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.agents[dto.UserID]
	if !ok {
		// 本实例从未见过的 agent：按远端快照整条插入。
		now := time.Now()
		if dto.ConnectedAt.IsZero() {
			dto.ConnectedAt = now
		}
		if dto.LastActivity.IsZero() {
			dto.LastActivity = now
		}
		if dto.Status == "" {
			dto.Status = string(agentdomain.PresenceStatusOnline)
		}
		r.agents[dto.UserID] = &runtimeAgent{AgentRuntimeDTO: dto, Sessions: map[string]*models.Session{}}
		return
	}
	// 已有本地条目：只刷新元数据，记账字段保留本地。
	chatLoad, voiceLoad := item.CurrentChatLoad, item.CurrentVoiceLoad
	connectedAt, lastActivity := item.ConnectedAt, item.LastActivity
	sessions := item.Sessions
	item.AgentRuntimeDTO = dto
	item.CurrentChatLoad = chatLoad
	item.CurrentVoiceLoad = voiceLoad
	item.ConnectedAt = connectedAt
	item.LastActivity = lastActivity
	item.Sessions = sessions
}

func (r *InMemoryRegistry) Get(userID uint) (agentapp.AgentRuntimeDTO, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.agents[userID]
	if !ok {
		return agentapp.AgentRuntimeDTO{}, false
	}
	return item.AgentRuntimeDTO, true
}

func (r *InMemoryRegistry) List() []agentapp.AgentRuntimeDTO {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]agentapp.AgentRuntimeDTO, 0, len(r.agents))
	for _, item := range r.agents {
		out = append(out, item.AgentRuntimeDTO)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}
