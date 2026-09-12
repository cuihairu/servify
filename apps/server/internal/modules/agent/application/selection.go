package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

// SelectionSource 标记本次选坐席命中的分配层级。
type SelectionSource string

const (
	SelectionSourceAffinity SelectionSource = "affinity" // 老客户找回原坐席
	SelectionSourceGroup    SelectionSource = "group"    // 指定组内分配
	SelectionSourceGlobal   SelectionSource = "global"   // 全局池
)

// ErrGroupUnavailable 显式指定组但组不可用（不存在、禁用，或 none 溢出策略下无可用成员）。
var ErrGroupUnavailable = errors.New("no available agent in the requested group")

// AffinityHint 亲和路由提示：客户找最近服务过自己的坐席。
type AffinityHint struct {
	CustomerUserID uint // sessions.user_id（= users.id）
	WindowDays     int  // 回看窗口；0 取 DefaultAffinityWindowDays
}

// DefaultAffinityWindowDays 亲和路由默认回看窗口。
const DefaultAffinityWindowDays = 30

// SelectionRequest 选坐席请求。GroupID 为 nil/0 表示不限组。
type SelectionRequest struct {
	Skills   []string
	Priority string
	GroupID  *uint
	Affinity *AffinityHint
}

// SelectionResult 选坐席结果：命中的坐席 + 分配来源。
type SelectionResult struct {
	Agent   *AgentRuntimeDTO
	Source  SelectionSource
	GroupID *uint
}

// SelectAgent 三级分配管线：亲和 → 组内 → 全局池。
// 亲和/组内不命中时依次降级，最终全局池无人则返回与 FindAvailableAgent 相同的错误。
func (s *Service) SelectAgent(ctx context.Context, req SelectionRequest) (*SelectionResult, error) {
	runtimes, err := s.repo.ListActiveAgentRuntimes(ctx)
	if err != nil {
		return nil, err
	}
	candidates := s.mergeRuntimeMetadata(runtimes)

	if agent, ok := s.pickByAffinity(ctx, candidates, req.Affinity); ok {
		return &SelectionResult{Agent: agent, Source: SelectionSourceAffinity}, nil
	}
	if agent, ok, handled := s.pickByGroup(ctx, candidates, req); handled {
		if !ok || agent == nil {
			return nil, ErrGroupUnavailable
		}
		return &SelectionResult{Agent: agent, Source: SelectionSourceGroup, GroupID: req.GroupID}, nil
	}
	agent, err := s.pickGlobal(candidates, req.Skills, req.Priority)
	if err != nil {
		return nil, err
	}
	return &SelectionResult{Agent: agent, Source: SelectionSourceGlobal}, nil
}

// pickByAffinity 硬亲和：客户最近的服务坐席在线且未满载则直接命中，否则降级后续层级。
func (s *Service) pickByAffinity(ctx context.Context, candidates []AgentRuntimeDTO, hint *AffinityHint) (*AgentRuntimeDTO, bool) {
	if hint == nil || hint.CustomerUserID == 0 {
		return nil, false
	}
	windowDays := hint.WindowDays
	if windowDays <= 0 {
		windowDays = DefaultAffinityWindowDays
	}
	lastAgentID, err := s.repo.GetLastAgentForCustomer(ctx, hint.CustomerUserID, time.Now().UTC().AddDate(0, 0, -windowDays))
	if err != nil || lastAgentID == nil || *lastAgentID == 0 {
		return nil, false
	}
	for i := range candidates {
		candidate := candidates[i]
		if candidate.UserID == *lastAgentID && eligible(candidate) {
			return &candidate, true
		}
	}
	return nil, false
}

// pickByGroup 组内分配：成员∩在线未满载取 calculateScore 最高；无可用成员时按溢出策略
// 决定降级全局池（global）还是失败（none）。handled 表示本次是否消费了组分支。
func (s *Service) pickByGroup(ctx context.Context, candidates []AgentRuntimeDTO, req SelectionRequest) (agent *AgentRuntimeDTO, ok bool, handled bool) {
	if req.GroupID == nil || *req.GroupID == 0 {
		return nil, false, false
	}
	memberIDs, err := s.repo.ListEnabledGroupMemberIDs(ctx, *req.GroupID)
	if err != nil || len(memberIDs) == 0 {
		// 组不存在或禁用：视为组不可用，不静默改道全局池。
		return nil, false, true
	}
	members := make(map[uint]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		members[id] = struct{}{}
	}
	requiredSkills := sanitizeSkills(req.Skills)
	bestScore := -1.0
	for i := range candidates {
		candidate := candidates[i]
		if _, isMember := members[candidate.UserID]; !isMember || !eligible(candidate) {
			continue
		}
		if score := calculateScore(candidate, requiredSkills, req.Priority); score > bestScore {
			bestScore = score
			agent = &candidates[i]
			ok = true
		}
	}
	if ok {
		return agent, true, true
	}
	// 组内无人：按溢出策略决定降级还是失败。
	group, err := s.repo.GetAgentGroup(ctx, *req.GroupID)
	if err != nil {
		return nil, false, true
	}
	if group.OverflowPolicy == "none" {
		return nil, false, true
	}
	return nil, false, false // global：继续全局池
}

// pickGlobal 全局池：原有 FindAvailableAgent 逻辑。
func (s *Service) pickGlobal(candidates []AgentRuntimeDTO, skills []string, priority string) (*AgentRuntimeDTO, error) {
	requiredSkills := sanitizeSkills(skills)
	var best *AgentRuntimeDTO
	bestScore := -1.0
	for i := range candidates {
		candidate := candidates[i]
		if !eligible(candidate) {
			continue
		}
		score := calculateScore(candidate, requiredSkills, priority)
		if score > bestScore {
			best = &candidates[i]
			bestScore = score
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no available agent found")
	}
	return best, nil
}

// eligible 在线且未满载。
func eligible(agent AgentRuntimeDTO) bool {
	if agent.Status != string(agentdomain.PresenceStatusOnline) {
		return false
	}
	return agent.CurrentChatLoad < agent.MaxChatConcurrency
}
