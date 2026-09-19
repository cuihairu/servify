package delivery

import (
	"context"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"

	"github.com/sirupsen/logrus"
)

// HandlerServiceAdapter 是 agent module 的 delivery 入口：实现 HandlerService、
// AgentGroupService 契约与 routing / workspace 依赖的窄接口，全部转发到单一
// application.Service 实例（无独立状态，避免多实例 split-brain）。
type HandlerServiceAdapter struct {
	module *agentapp.Service
	logger *logrus.Logger
}

// NewHandlerServiceAdapter 包装 application.Service；logger 为 nil 时取默认。
func NewHandlerServiceAdapter(module *agentapp.Service, logger *logrus.Logger) *HandlerServiceAdapter {
	if logger == nil {
		logger = logrus.New()
	}
	return &HandlerServiceAdapter{module: module, logger: logger}
}

var (
	_ HandlerService   = (*HandlerServiceAdapter)(nil)
	_ AgentGroupService = (*HandlerServiceAdapter)(nil)
)

// CreateAgent 创建客服；Skills 契约为逗号分隔字符串，此处拆分后由 module 去重清洗。
func (s *HandlerServiceAdapter) CreateAgent(ctx context.Context, req *AgentCreateRequest) (*models.Agent, error) {
	return s.module.CreateAgent(ctx, agentapp.CreateAgentCommand{
		UserID:             req.UserID,
		Department:         req.Department,
		Skills:             parseSkills(req.Skills),
		MaxChatConcurrency: req.MaxConcurrent,
	})
}

func (s *HandlerServiceAdapter) GetAgentByUserID(ctx context.Context, userID uint) (*models.Agent, error) {
	return s.module.GetAgentByUserID(ctx, userID)
}

func (s *HandlerServiceAdapter) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	return s.module.ListAgents(ctx, limit)
}

func (s *HandlerServiceAdapter) AgentGoOnline(ctx context.Context, userID uint) error {
	if err := s.module.GoOnline(ctx, userID); err != nil {
		return err
	}
	s.logger.Infof("Agent %d went online", userID)
	return nil
}

func (s *HandlerServiceAdapter) AgentGoOffline(ctx context.Context, userID uint) error {
	if err := s.module.GoOffline(ctx, userID); err != nil {
		return err
	}
	s.logger.Infof("Agent %d went offline", userID)
	return nil
}

func (s *HandlerServiceAdapter) UpdateAgentStatus(ctx context.Context, userID uint, status string) error {
	if err := s.module.UpdateStatus(ctx, userID, status); err != nil {
		return err
	}
	s.logger.Infof("Agent %d status updated to %s", userID, status)
	return nil
}

func (s *HandlerServiceAdapter) RevokeAgentTokens(ctx context.Context, userID uint) (int, error) {
	version, err := s.module.RevokeUserTokens(ctx, userID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	s.logger.Infof("Revoked tokens for agent %d, new token version %d", userID, version)
	return version, nil
}

func (s *HandlerServiceAdapter) AssignSessionToAgent(ctx context.Context, sessionID string, agentID uint) error {
	if err := s.module.AssignSession(ctx, sessionID, agentID); err != nil {
		return err
	}
	s.logger.Infof("Assigned session %s to agent %d", sessionID, agentID)
	return nil
}

func (s *HandlerServiceAdapter) ReleaseSessionFromAgent(ctx context.Context, sessionID string, agentID uint) error {
	if err := s.module.ReleaseSession(ctx, sessionID, agentID); err != nil {
		return err
	}
	s.logger.Infof("Released session %s from agent %d", sessionID, agentID)
	return nil
}

func (s *HandlerServiceAdapter) FindAvailableAgent(ctx context.Context, skills []string, priority string) (*AgentInfo, error) {
	runtime, err := s.module.FindAvailableAgent(ctx, skills, priority)
	if err != nil {
		return nil, err
	}
	return AgentInfoFromRuntime(runtime), nil
}

// SelectAgent 三级分配管线（亲和 → 组内 → 全局池），供 routing 等调用方使用。
func (s *HandlerServiceAdapter) SelectAgent(ctx context.Context, req SelectionRequest) (*SelectionResult, error) {
	return s.module.SelectAgent(ctx, req)
}

// GetOnlineAgent 单个在线坐席查询；miss 返回 (nil, false)。
func (s *HandlerServiceAdapter) GetOnlineAgent(ctx context.Context, userID uint) (*AgentInfo, bool) {
	runtime, err := s.module.GetOnlineAgent(ctx, userID)
	if err != nil {
		return nil, false
	}
	return AgentInfoFromRuntime(runtime), true
}

// GetOnlineAgents 在线坐席列表（workspace 概览等只读视图消费）。
func (s *HandlerServiceAdapter) GetOnlineAgents(ctx context.Context) []*AgentInfo {
	runtimes := s.module.GetOnlineAgents(ctx)
	out := make([]*AgentInfo, 0, len(runtimes))
	for _, item := range runtimes {
		copy := item
		out = append(out, AgentInfoFromRuntime(&copy))
	}
	return out
}

// AgentStats 坐席统计视图。
type AgentStats = agentapp.AgentStatsDTO

func (s *HandlerServiceAdapter) GetAgentStats(ctx context.Context, agentID *uint) (*AgentStats, error) {
	return s.module.GetStats(ctx, agentID)
}

// ---- 坐席组管理（实现 AgentGroupService）----

func (s *HandlerServiceAdapter) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return s.module.ListAgentGroups(ctx)
}

func (s *HandlerServiceAdapter) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return s.module.GetAgentGroup(ctx, id)
}

func (s *HandlerServiceAdapter) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return s.module.CreateAgentGroup(ctx, group)
}

func (s *HandlerServiceAdapter) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return s.module.UpdateAgentGroup(ctx, group)
}

func (s *HandlerServiceAdapter) DeleteAgentGroup(ctx context.Context, id uint) error {
	return s.module.DeleteAgentGroup(ctx, id)
}

func (s *HandlerServiceAdapter) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return s.module.ReplaceGroupMembers(ctx, groupID, agentUserIDs)
}

// ListGroupMembers 组成员读取（含禁用组——管理面需要完整视图）。
func (s *HandlerServiceAdapter) ListGroupMembers(ctx context.Context, groupID uint) ([]uint, error) {
	return s.module.ListGroupMembers(ctx, groupID)
}

// ApplySessionTransfer 应用会话转接结果到坐席运行态；失败只记日志不中断转接主流程。
func (s *HandlerServiceAdapter) ApplySessionTransfer(ctx context.Context, sessionID string, fromAgentID *uint, toAgentID uint) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.module.ApplySessionTransfer(ctx, sessionID, fromAgentID, toAgentID); err != nil {
		s.logger.Warnf("apply session transfer to agent module failed: %v", err)
	}
}

// parseSkills 拆分逗号分隔的技能串并去空白；去重归 module 的 sanitizeSkills。
func parseSkills(skillsStr string) []string {
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
