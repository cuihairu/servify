package application

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

type Repository interface {
	CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error)
	GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error)
	GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*AgentRuntimeDTO, error)
	ListActiveAgentRuntimes(ctx context.Context) ([]AgentRuntimeDTO, error)
	ListAgents(ctx context.Context, limit int) ([]models.Agent, error)
	UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error
	UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error
	// Persisted runtime metadata methods (replace in-memory registry)
	UpdateLastActivity(ctx context.Context, userID uint) error
	SetConnectedTime(ctx context.Context, userID uint) error
	ClearConnectedTime(ctx context.Context, userID uint) error
	GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error)
	AssignSession(ctx context.Context, sessionID string, agentUserID uint) error
	ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error
	GetStats(ctx context.Context, agentUserID *uint) (*AgentStatsDTO, error)
	RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error)
	// GetLastAgentForCustomer 亲和路由：返回客户在 since 之后最近一次由坐席服务的
	// agent_id（users.id）；没有则返回 (nil, nil)。
	GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error)
	// GetAgentGroup 取单个组（软删过滤）；不存在返回 gorm.ErrRecordNotFound。
	GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error)
	// ListAgentGroups 返回全部组（软删过滤，按 priority/created_at 排序）。
	ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error)
	// CreateAgentGroup 创建组（tenant/workspace 从 ctx scope 注入）。
	CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error
	// UpdateAgentGroup 更新组基础字段。
	UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error
	// DeleteAgentGroup 软删组并清空成员。
	DeleteAgentGroup(ctx context.Context, id uint) error
	// ReplaceGroupMembers 全量替换组成员（事务）。
	ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error
	// ListEnabledGroupMemberIDs 返回启用组的成员 user_id；组不存在或禁用返回空。
	ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error)
}

type RuntimeRegistry interface {
	GoOnline(profile agentdomain.AgentProfile) (AgentRuntimeDTO, error)
	GoOffline(userID uint)
	UpdateStatus(userID uint, status agentdomain.PresenceStatus)
	AssignSession(userID uint, session *models.Session) (AgentRuntimeDTO, error)
	ReleaseSession(userID uint, sessionID string) (AgentRuntimeDTO, bool)
	ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint)
	Get(userID uint) (AgentRuntimeDTO, bool)
	List() []AgentRuntimeDTO
}
