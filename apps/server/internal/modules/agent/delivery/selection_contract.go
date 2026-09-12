package delivery

import (
	"context"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
)

// 选坐席（selection）契约：handler/runtime 层经此别名使用 agent application
// 的三级分配管线，避免跨包直接 import application（见 module-boundaries.rules）。

type SelectionRequest = agentapp.SelectionRequest
type SelectionSource = agentapp.SelectionSource
type SelectionResult = agentapp.SelectionResult
type AffinityHint = agentapp.AffinityHint
type AgentRuntimeDTO = agentapp.AgentRuntimeDTO

const (
	SelectionSourceAffinity = agentapp.SelectionSourceAffinity
	SelectionSourceGroup    = agentapp.SelectionSourceGroup
	SelectionSourceGlobal   = agentapp.SelectionSourceGlobal
)

// ErrGroupUnavailable 转发：显式指定组但组不可用。
var ErrGroupUnavailable = agentapp.ErrGroupUnavailable

// 组管理错误转发（handler 侧映射 HTTP 状态用）。
var (
	ErrGroupNameRequired = agentapp.ErrGroupNameRequired
	ErrGroupNotFound     = agentapp.ErrGroupNotFound
	ErrGroupDuplicate    = agentapp.ErrGroupDuplicate
)

// AgentInfoFromRuntime 选坐席结果 → 管理面 AgentInfo（字段对齐 mapRuntimeToLegacy）。
func AgentInfoFromRuntime(dto *AgentRuntimeDTO) *AgentInfo {
	if dto == nil {
		return nil
	}
	return &AgentInfo{
		UserID:          dto.UserID,
		Username:        dto.Username,
		Name:            dto.Name,
		Department:      dto.Department,
		Skills:          append([]string(nil), dto.Skills...),
		Status:          dto.Status,
		MaxConcurrent:   dto.MaxChatConcurrency,
		CurrentLoad:     dto.CurrentChatLoad,
		Rating:          dto.Rating,
		AvgResponseTime: dto.AvgResponseTime,
		LastActivity:    dto.LastActivity,
		ConnectedAt:     dto.ConnectedAt,
	}
}

// AgentGroupService 坐席组管理契约（管理面 CRUD 由 services.AgentService 实现）。
type AgentGroupService interface {
	ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error)
	GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error)
	CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error
	UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error
	DeleteAgentGroup(ctx context.Context, id uint) error
	ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error
	ListGroupMembers(ctx context.Context, groupID uint) ([]uint, error)
}
