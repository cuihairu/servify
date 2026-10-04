package application

// 覆盖补充（B2-1 评分引擎）：输入时钟注入、全字段权重覆盖合并、忙态/
// 过载/高价值降权/渠道不匹配/SLA 无余量分支、tier 枚举两端。

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDefaultScorerNowAndFullWeightOverride(t *testing.T) {
	scorer := NewDefaultScorer(DefaultWeightSet)
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	deadline := now.Add(10 * time.Minute) // 进入紧急窗口但候选满载 → SLA 因子清零
	input := ScoringInput{
		SessionID:  "conv-1",
		Now:        &now,
		DeadlineAt: &deadline,
		Weights: &WeightSet{ // 全字段非零：覆盖 mergeWeights 的每个分支
			Skill: 0.1, Language: 0.1, Availability: 0.1, Workload: 0.1,
			Priority: 0.15, Tier: 0.15, Channel: 0.15, SLA: 0.15,
		},
		Candidates: []AgentCandidate{{
			AgentID: 1, Status: "online",
			MaxConcurrent: 2, CurrentLoad: 3, // 过载 → remaining<0 截 0
		}},
	}
	out, err := scorer.Score(context.Background(), input)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("out = %d", len(out))
	}
	// 过载：workload=0；无技能要求 skill=1；在线 availability=1。
	got := out[0].Factors
	if got["workload"] != 0 || got["skill"] != 1 || got["availability"] != 1 {
		t.Fatalf("factors = %v", got)
	}
	if got["sla"] != 0 {
		t.Fatalf("满载 + 临近 SLA 应清零 sla 因子，got %v", got["sla"])
	}
}

func TestDefaultScorerBusyReasonAndChannelMismatch(t *testing.T) {
	scorer := NewDefaultScorer(DefaultWeightSet)
	input := ScoringInput{
		Channel: "phone",
		Candidates: []AgentCandidate{{
			AgentID: 4, Status: "Busy",
			Channels: []string{"web"}, // 声明偏好但不含 phone → 清零 + 理由
		}},
	}
	out, err := scorer.Score(context.Background(), input)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if out[0].Factors["availability"] != 0.3 {
		t.Fatalf("busy availability = %v want 0.3", out[0].Factors["availability"])
	}
	if out[0].Factors["channel"] != 0 {
		t.Fatalf("channel mismatch = %v want 0", out[0].Factors["channel"])
	}
	if !containsReason(out[0].Reasons, "坐席忙") || !containsReason(out[0].Reasons, "渠道") {
		t.Fatalf("reasons = %v", out[0].Reasons)
	}
}

func TestDefaultScorerChannelMatchAndTierPenalty(t *testing.T) {
	scorer := NewDefaultScorer(DefaultWeightSet)
	input := ScoringInput{
		Channel:      "web",
		CustomerTier: "vip",
		Skills:       []string{"billing", "logistics"}, // 部分匹配 → tier 降权
		Candidates: []AgentCandidate{{
			AgentID: 9, Status: "online",
			Skills:   []string{"billing"},
			Channels: []string{"web"},
		}},
	}
	out, err := scorer.Score(context.Background(), input)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if out[0].Factors["channel"] != 1 {
		t.Fatalf("channel match = %v want 1", out[0].Factors["channel"])
	}
	if out[0].Factors["tier"] != 0.5 {
		t.Fatalf("vip 且技能未全匹配应降权到 0.5，got %v", out[0].Factors["tier"])
	}
	if !containsReason(out[0].Reasons, "未全量匹配技能") {
		t.Fatalf("reasons = %v", out[0].Reasons)
	}
}

func TestIsHighValueTierEnumeration(t *testing.T) {
	for _, tier := range []string{"vip", "VIP", " platinum ", "enterprise", "Enterprise"} {
		if !isHighValueTier(tier) {
			t.Errorf("%q should be high value", tier)
		}
	}
	for _, tier := range []string{"", "normal", "普通", "gold"} {
		if isHighValueTier(tier) {
			t.Errorf("%q should not be high value", tier)
		}
	}
}

func containsReason(reasons []string, sub string) bool {
	for _, r := range reasons {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}
