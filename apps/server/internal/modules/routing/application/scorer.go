package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Scorer 是路由打分引擎的接入点（V1.0 B2-1，docs/v1-convergence-plan.md
// §6.2）：输入会话上下文与候选坐席，输出按分排序的候选序列。评分只影响
// 「自动推荐候选序列」，不自动执行分配（与 handoff 语义一致，人工优先）。
type Scorer interface {
	// Strategy 返回评分策略标识/版本，随分数落 routing_assignments.strategy。
	Strategy() string
	Score(ctx context.Context, input ScoringInput) ([]ScoredCandidate, error)
}

// AgentCandidate 是参与评分的候选坐席快照（由调用方从 agent 模块投影，
// routing 不反向依赖 agent 模块）。
type AgentCandidate struct {
	AgentID       uint
	UserID        uint
	Status        string   // online / busy / offline
	Skills        []string
	Languages     []string
	Channels      []string // 空 = 不限渠道
	CurrentLoad   int
	MaxConcurrent int
}

// ScoringInput 携带一次评分的会话侧上下文与候选列表。
type ScoringInput struct {
	SessionID    string
	Channel      string
	Skills       []string // 会话要求技能
	Language     string   // 会话语言（空 = 不限）
	Priority     string   // urgent / high / normal / low
	CustomerTier string   // 客户分级（vip 等，空 = 普通）
	DeadlineAt   *time.Time
	Candidates   []AgentCandidate
	// Weights 覆盖默认权重；nil 用 DefaultWeightSet。
	Weights *WeightSet
	// Now 供测试注入时钟；nil 用 time.Now。
	Now *time.Time
}

// ScoredCandidate 是评分后的候选：总分 + 因子明细 + 人读理由。
type ScoredCandidate struct {
	AgentCandidate
	Total   float64            `json:"total_score"`
	Factors map[string]float64 `json:"factors"`
	Reasons []string           `json:"reasons"`
}

// WeightSet 是多因子权重表；合计应为 1（不强制，超 1 时总分可能超 1）。
type WeightSet struct {
	Skill        float64
	Language     float64
	Availability float64
	Workload     float64
	Priority     float64
	Tier         float64
	Channel      float64
	SLA          float64
}

// DefaultWeightSet 默认加权：技能与可用性主导，负载次之，其余为微调。
var DefaultWeightSet = WeightSet{
	Skill:        0.30,
	Language:     0.20,
	Availability: 0.20,
	Workload:     0.15,
	Priority:     0.05,
	Tier:         0.03,
	Channel:      0.04,
	SLA:          0.03,
}

// SLAUrgentWindow 会话剩余时限小于该窗口时按紧急处理（SLA 因子生效）。
const SLAUrgentWindow = 30 * time.Minute

// DefaultScorer 是计划书 §6.2 的默认多因子加权聚合。
type DefaultScorer struct {
	weights WeightSet
}

// NewDefaultScorer 用给定权重构造；零值字段回落 DefaultWeightSet 对应项。
func NewDefaultScorer(weights WeightSet) *DefaultScorer {
	merged := weights
	if merged.Skill == 0 {
		merged.Skill = DefaultWeightSet.Skill
	}
	if merged.Language == 0 {
		merged.Language = DefaultWeightSet.Language
	}
	if merged.Availability == 0 {
		merged.Availability = DefaultWeightSet.Availability
	}
	if merged.Workload == 0 {
		merged.Workload = DefaultWeightSet.Workload
	}
	if merged.Priority == 0 {
		merged.Priority = DefaultWeightSet.Priority
	}
	if merged.Tier == 0 {
		merged.Tier = DefaultWeightSet.Tier
	}
	if merged.Channel == 0 {
		merged.Channel = DefaultWeightSet.Channel
	}
	if merged.SLA == 0 {
		merged.SLA = DefaultWeightSet.SLA
	}
	return &DefaultScorer{weights: merged}
}

func (s *DefaultScorer) Strategy() string { return "default_weighted_v1" }

// Score 逐候选计算八因子加权总分，按总分降序（同分按 AgentID 升序保证可复现）。
func (s *DefaultScorer) Score(_ context.Context, input ScoringInput) ([]ScoredCandidate, error) {
	if len(input.Candidates) == 0 {
		return nil, nil
	}
	now := time.Now()
	if input.Now != nil {
		now = *input.Now
	}
	weights := s.weights
	if input.Weights != nil {
		weights = mergeWeights(s.weights, *input.Weights)
	}

	out := make([]ScoredCandidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		scored := scoreCandidate(candidate, input, weights, now)
		out = append(out, scored)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out, nil
}

func mergeWeights(base, override WeightSet) WeightSet {
	merged := base
	if override.Skill != 0 {
		merged.Skill = override.Skill
	}
	if override.Language != 0 {
		merged.Language = override.Language
	}
	if override.Availability != 0 {
		merged.Availability = override.Availability
	}
	if override.Workload != 0 {
		merged.Workload = override.Workload
	}
	if override.Priority != 0 {
		merged.Priority = override.Priority
	}
	if override.Tier != 0 {
		merged.Tier = override.Tier
	}
	if override.Channel != 0 {
		merged.Channel = override.Channel
	}
	if override.SLA != 0 {
		merged.SLA = override.SLA
	}
	return merged
}

// scoreCandidate 单候选评分：八因子独立计算（0-1），加权求和；
// 非满分因子落人读理由（分配理由可见，计划书 §6.3-3）。
func scoreCandidate(candidate AgentCandidate, input ScoringInput, weights WeightSet, now time.Time) ScoredCandidate {
	factors := map[string]float64{}
	reasons := make([]string, 0, 4)

	// skill：要求技能的交集比例；无要求 = 满分。
	skillScore := 1.0
	if len(input.Skills) > 0 {
		matched := 0
		for _, required := range input.Skills {
			for _, owned := range candidate.Skills {
				if strings.EqualFold(strings.TrimSpace(required), strings.TrimSpace(owned)) {
					matched++
					break
				}
			}
		}
		skillScore = float64(matched) / float64(len(input.Skills))
		if skillScore < 1 {
			reasons = append(reasons, fmt.Sprintf("技能匹配 %d/%d", matched, len(input.Skills)))
		}
	}
	factors["skill"] = skillScore

	// language：会话语言命中候选语种；会话无语言或候选未声明语种（通用
	// 坐席）均满分，不惩罚。
	languageScore := 1.0
	if lang := strings.TrimSpace(input.Language); lang != "" && len(candidate.Languages) > 0 {
		languageScore = 0
		for _, owned := range candidate.Languages {
			if strings.EqualFold(strings.TrimSpace(owned), lang) {
				languageScore = 1
				break
			}
		}
		if languageScore == 0 {
			reasons = append(reasons, fmt.Sprintf("语种 %s 不匹配", lang))
		}
	}
	factors["language"] = languageScore

	// availability：在线状态直接映射。
	availabilityScore := 0.0
	switch strings.ToLower(strings.TrimSpace(candidate.Status)) {
	case "online":
		availabilityScore = 1.0
	case "busy":
		availabilityScore = 0.3
		reasons = append(reasons, "坐席忙（降权）")
	default:
		reasons = append(reasons, "坐席离线")
	}
	factors["availability"] = availabilityScore

	// workload：空闲容量比例；无上限配置按中位处理。
	workloadScore := 0.5
	if candidate.MaxConcurrent > 0 {
		remaining := candidate.MaxConcurrent - candidate.CurrentLoad
		if remaining < 0 {
			remaining = 0
		}
		workloadScore = float64(remaining) / float64(candidate.MaxConcurrent)
		if workloadScore < 1 && candidate.CurrentLoad > 0 {
			reasons = append(reasons, fmt.Sprintf("负载 %d/%d", candidate.CurrentLoad, candidate.MaxConcurrent))
		}
	}
	factors["workload"] = workloadScore

	hasCapacity := candidate.MaxConcurrent <= 0 || candidate.CurrentLoad < candidate.MaxConcurrent

	// priority：紧急/高优会话必须还有接单容量，否则清零该因子。
	priorityScore := 1.0
	switch strings.ToLower(strings.TrimSpace(input.Priority)) {
	case "urgent", "high":
		if !hasCapacity {
			priorityScore = 0
			reasons = append(reasons, "无余量承接高优先级会话")
		}
	}
	factors["priority"] = priorityScore

	// tier：高价值客户要求技能全匹配，否则降权（引导高分坐席承接）。
	tierScore := 1.0
	if isHighValueTier(input.CustomerTier) && skillScore < 1 {
		tierScore = 0.5
		reasons = append(reasons, fmt.Sprintf("%s 客户未全量匹配技能", strings.ToUpper(input.CustomerTier)))
	}
	factors["tier"] = tierScore

	// channel：候选声明了渠道偏好但不含会话渠道 → 清零；无偏好 = 满分。
	channelScore := 1.0
	if channel := strings.TrimSpace(input.Channel); channel != "" && len(candidate.Channels) > 0 {
		channelScore = 0
		for _, owned := range candidate.Channels {
			if strings.EqualFold(strings.TrimSpace(owned), channel) {
				channelScore = 1
				break
			}
		}
		if channelScore == 0 {
			reasons = append(reasons, fmt.Sprintf("渠道 %s 不在坐席偏好内", channel))
		}
	}
	factors["channel"] = channelScore

	// sla：剩余时限进入紧急窗口时要求有余量；无 deadline = 满分。
	slaScore := 1.0
	if input.DeadlineAt != nil {
		if remaining := input.DeadlineAt.Sub(now); remaining > 0 && remaining < SLAUrgentWindow && !hasCapacity {
			slaScore = 0
			reasons = append(reasons, "SLA 临近但无余量")
		}
	}
	factors["sla"] = slaScore

	total := factors["skill"]*weights.Skill +
		factors["language"]*weights.Language +
		factors["availability"]*weights.Availability +
		factors["workload"]*weights.Workload +
		factors["priority"]*weights.Priority +
		factors["tier"]*weights.Tier +
		factors["channel"]*weights.Channel +
		factors["sla"]*weights.SLA

	return ScoredCandidate{
		AgentCandidate: candidate,
		Total:          total,
		Factors:        factors,
		Reasons:        reasons,
	}
}

func isHighValueTier(tier string) bool {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "vip", "platinum", "enterprise":
		return true
	}
	return false
}
