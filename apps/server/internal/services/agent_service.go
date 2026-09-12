package services

import (
	"context"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// AgentService 人工客服服务兼容层。
//
// 它目前同时承担两类职责：
// 1. 作为 HTTP handler 的兼容 contract 实现
// 2. 维护尚未迁出的 runtime 状态同步逻辑（onlineAgents）
//
// 后续迁移中，HTTP 层应只依赖 modules/agent/delivery.HandlerService，而不是直接依赖此具体类型。
type AgentService struct {
	db     *gorm.DB
	logger *logrus.Logger
	module *agentapp.Service
}

// NewAgentService 创建人工客服服务。
func NewAgentService(db *gorm.DB, logger *logrus.Logger) *AgentService {
	return BuildAgentServiceAssembly(db, logger, nil).Service
}

func NewAgentServiceWithDependencies(deps AgentServiceDependencies) *AgentService {
	logger := deps.Logger
	if logger == nil {
		logger = logrus.New()
	}

	service := &AgentService{
		db:     deps.DB,
		logger: logger,
		module: deps.Module,
	}
	return service
}

// AgentInfo 在线客服信息。
type AgentInfo = agentdelivery.AgentInfo

// AgentCreateRequest 创建客服请求。
type AgentCreateRequest = agentdelivery.AgentCreateRequest

// AgentUpdateRequest 更新客服请求。
type AgentUpdateRequest struct {
	Department    *string `json:"department"`
	Skills        *string `json:"skills"`
	Status        *string `json:"status"`
	MaxConcurrent *int    `json:"max_concurrent"`
}

func (s *AgentService) CreateAgent(ctx context.Context, req *AgentCreateRequest) (*models.Agent, error) {
	return s.module.CreateAgent(ctx, agentapp.CreateAgentCommand{
		UserID:             req.UserID,
		Department:         req.Department,
		Skills:             s.parseSkills(req.Skills),
		MaxChatConcurrency: req.MaxConcurrent,
	})
}

func (s *AgentService) GetAgentByUserID(ctx context.Context, userID uint) (*models.Agent, error) {
	return s.module.GetAgentByUserID(ctx, userID)
}

func (s *AgentService) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	return s.module.ListAgents(ctx, limit)
}

func (s *AgentService) AgentGoOnline(ctx context.Context, userID uint) error {
	if err := s.module.GoOnline(ctx, userID); err != nil {
		return err
	}
	s.logger.Infof("Agent %d went online", userID)
	return nil
}

func (s *AgentService) AgentGoOffline(ctx context.Context, userID uint) error {
	if err := s.module.GoOffline(ctx, userID); err != nil {
		return err
	}
	s.logger.Infof("Agent %d went offline", userID)
	return nil
}

func (s *AgentService) UpdateAgentStatus(ctx context.Context, userID uint, status string) error {
	if err := s.module.UpdateStatus(ctx, userID, status); err != nil {
		return err
	}
	s.logger.Infof("Agent %d status updated to %s", userID, status)
	return nil
}

func (s *AgentService) RevokeAgentTokens(ctx context.Context, userID uint) (int, error) {
	version, err := s.module.RevokeUserTokens(ctx, userID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	s.logger.Infof("Revoked tokens for agent %d, new token version %d", userID, version)
	return version, nil
}

func (s *AgentService) AssignSessionToAgent(ctx context.Context, sessionID string, agentID uint) error {
	if err := s.module.AssignSession(ctx, sessionID, agentID); err != nil {
		return err
	}
	s.logger.Infof("Assigned session %s to agent %d", sessionID, agentID)
	return nil
}

func (s *AgentService) ReleaseSessionFromAgent(ctx context.Context, sessionID string, agentID uint) error {
	if err := s.module.ReleaseSession(ctx, sessionID, agentID); err != nil {
		return err
	}
	s.logger.Infof("Released session %s from agent %d", sessionID, agentID)
	return nil
}

func (s *AgentService) FindAvailableAgent(ctx context.Context, skills []string, priority string) (*AgentInfo, error) {
	runtime, err := s.module.FindAvailableAgent(ctx, skills, priority)
	if err != nil {
		return nil, err
	}
	return mapRuntimeToLegacy(runtime), nil
}

// SelectAgent 三级分配管线（亲和 → 组内 → 全局池），供 routing 等调用方使用。
func (s *AgentService) SelectAgent(ctx context.Context, req agentdelivery.SelectionRequest) (*agentdelivery.SelectionResult, error) {
	return s.module.SelectAgent(ctx, req)
}

// ---- 坐席组管理（实现 agentdelivery.AgentGroupService）----

func (s *AgentService) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return s.module.ListAgentGroups(ctx)
}

func (s *AgentService) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return s.module.GetAgentGroup(ctx, id)
}

func (s *AgentService) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return s.module.CreateAgentGroup(ctx, group)
}

func (s *AgentService) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return s.module.UpdateAgentGroup(ctx, group)
}

func (s *AgentService) DeleteAgentGroup(ctx context.Context, id uint) error {
	return s.module.DeleteAgentGroup(ctx, id)
}

func (s *AgentService) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return s.module.ReplaceGroupMembers(ctx, groupID, agentUserIDs)
}

// ListGroupMembers 组成员读取（含禁用组——管理面需要完整视图）。
func (s *AgentService) ListGroupMembers(ctx context.Context, groupID uint) ([]uint, error) {
	return s.module.ListGroupMembers(ctx, groupID)
}

func (s *AgentService) GetOnlineAgents(ctx context.Context) []*AgentInfo {
	runtimes := s.module.GetOnlineAgents(ctx)
	out := make([]*AgentInfo, 0, len(runtimes))
	for _, item := range runtimes {
		copy := item
		out = append(out, mapRuntimeToLegacy(&copy))
	}
	return out
}

func (s *AgentService) GetOnlineAgent(ctx context.Context, userID uint) (*AgentInfo, bool) {
	runtime, err := s.module.GetOnlineAgent(ctx, userID)
	if err != nil {
		return nil, false
	}
	return mapRuntimeToLegacy(runtime), true
}

func (s *AgentService) GetAgentStats(ctx context.Context, agentID *uint) (*AgentStats, error) {
	stats, err := s.module.GetStats(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return &AgentStats{
		Total:           stats.Total,
		Online:          stats.Online,
		Busy:            stats.Busy,
		AvgResponseTime: stats.AvgResponseTime,
		AvgRating:       stats.AvgRating,
	}, nil
}

func (s *AgentService) parseSkills(skillsStr string) []string {
	if skillsStr == "" {
		return []string{}
	}
	skills := []string{}
	for _, skill := range strings.Split(skillsStr, ",") {
		skill = strings.TrimSpace(skill)
		if skill != "" {
			skills = append(skills, skill)
		}
	}
	return skills
}

func (s *AgentService) ApplySessionTransfer(ctx context.Context, sessionID string, fromAgentID *uint, toAgentID uint) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.module.ApplySessionTransfer(ctx, sessionID, fromAgentID, toAgentID); err != nil {
		s.logger.Warnf("apply session transfer to agent module failed: %v", err)
	}
}

func mapRuntimeToLegacy(runtime *agentapp.AgentRuntimeDTO) *AgentInfo {
	if runtime == nil {
		return nil
	}
	return &AgentInfo{
		UserID:          runtime.UserID,
		Username:        runtime.Username,
		Name:            runtime.Name,
		Department:      runtime.Department,
		Skills:          append([]string(nil), runtime.Skills...),
		Status:          runtime.Status,
		MaxConcurrent:   runtime.MaxChatConcurrency,
		CurrentLoad:     runtime.CurrentChatLoad,
		Rating:          runtime.Rating,
		AvgResponseTime: runtime.AvgResponseTime,
		LastActivity:    runtime.LastActivity,
		ConnectedAt:     runtime.ConnectedAt,
		Sessions:        make(map[string]*models.Session),
	}
}

// AgentStats 客服统计信息。
type AgentStats = agentdelivery.AgentStats
