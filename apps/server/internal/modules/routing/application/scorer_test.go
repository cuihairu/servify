package application

import (
	"context"
	"math"
	"testing"
	"time"
)

func candidate(id uint, status string, skills, languages []string, load, max int) AgentCandidate {
	return AgentCandidate{
		AgentID:       id,
		UserID:        id,
		Status:        status,
		Skills:        skills,
		Languages:     languages,
		CurrentLoad:   load,
		MaxConcurrent: max,
	}
}

// 计划书 §6.3 例 1：两组不同 skill/language 的坐席，分配结果可复现符合
// 预期权重——全匹配候选必须稳定压过半匹配/语种不符候选。
func TestDefaultScorerSkillLanguageReproducible(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{})
	input := ScoringInput{
		SessionID: "s-1",
		Skills:    []string{"billing"},
		Language:  "zh",
		Candidates: []AgentCandidate{
			candidate(1, "online", []string{"billing"}, []string{"zh"}, 0, 5), // 全匹配
			candidate(2, "online", []string{"billing"}, []string{"en"}, 0, 5), // 语种不符
			candidate(3, "online", []string{"general"}, []string{"zh"}, 0, 5), // 技能半缺
		},
	}
	first, err := scorer.Score(context.Background(), input)
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if len(first) != 3 || first[0].AgentID != 1 {
		t.Fatalf("expected agent 1 first, got %+v", first)
	}

	// 可复现：同输入重复评分，总分逐位一致且顺序稳定。
	second, err := scorer.Score(context.Background(), input)
	if err != nil {
		t.Fatalf("Score() replay error = %v", err)
	}
	for i := range first {
		if first[i].AgentID != second[i].AgentID ||
			math.Abs(first[i].Total-second[i].Total) > 1e-9 {
			t.Fatalf("score not reproducible at %d: %+v vs %+v", i, first[i], second[i])
		}
	}

	// 权重语义：全匹配满分；语种不符扣 language 因子；技能半缺扣 skill 因子。
	// 手算总分 = 1.0*(0.30+0.20+0.20+0.15+0.05+0.03+0.04+0.03) = 1.0
	if math.Abs(first[0].Total-1.0) > 1e-9 {
		t.Fatalf("full match total = %v, want 1.0", first[0].Total)
	}
	byID := map[uint]ScoredCandidate{}
	for _, item := range first {
		byID[item.AgentID] = item
	}
	// 语种不符 = 1.0 - 0.20 = 0.80
	if math.Abs(byID[2].Total-0.80) > 1e-9 {
		t.Fatalf("language miss total = %v, want 0.80", byID[2].Total)
	}
	// 技能半缺 = 1.0 - 0.30 = 0.70
	if math.Abs(byID[3].Total-0.70) > 1e-9 {
		t.Fatalf("skill half total = %v, want 0.70", byID[3].Total)
	}
	if len(byID[2].Reasons) == 0 || len(byID[3].Reasons) == 0 {
		t.Fatalf("expected reasons for non-full candidates: %+v", byID)
	}
}

// 计划书 §6.3 例 2：空闲坐席优先于满载坐席（负载因子）。
func TestDefaultScorerIdleBeatsLoaded(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{})
	idle := candidate(1, "online", []string{"billing"}, []string{"zh"}, 0, 5)
	loaded := candidate(2, "online", []string{"billing"}, []string{"zh"}, 5, 5) // 满载
	out, err := scorer.Score(context.Background(), ScoringInput{
		SessionID:  "s-2",
		Skills:     []string{"billing"},
		Language:   "zh",
		Candidates: []AgentCandidate{loaded, idle},
	})
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if len(out) != 2 || out[0].AgentID != 1 {
		t.Fatalf("expected idle agent 1 first, got %+v", out)
	}
	if math.Abs(out[0].Factors["workload"]-1.0) > 1e-9 {
		t.Fatalf("idle workload factor = %v, want 1.0", out[0].Factors["workload"])
	}
	if out[1].Factors["workload"] != 0 {
		t.Fatalf("loaded workload factor = %v, want 0", out[1].Factors["workload"])
	}
}

// 高优先级会话：满载坐席 priority 因子清零，进一步压低排序。
func TestDefaultScorerUrgentRequiresCapacity(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{})
	out, err := scorer.Score(context.Background(), ScoringInput{
		SessionID:  "s-3",
		Priority:   "urgent",
		Candidates: []AgentCandidate{candidate(1, "online", nil, nil, 5, 5)},
	})
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if out[0].Factors["priority"] != 0 {
		t.Fatalf("urgent full-load priority factor = %v, want 0", out[0].Factors["priority"])
	}
}

// SLA 临近 + 无余量 → sla 因子清零。
func TestDefaultScorerSLAUrgentWindow(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{})
	deadline := time.Now().Add(10 * time.Minute)
	out, err := scorer.Score(context.Background(), ScoringInput{
		SessionID:  "s-4",
		DeadlineAt: &deadline,
		Candidates: []AgentCandidate{candidate(1, "online", nil, nil, 5, 5)},
	})
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if out[0].Factors["sla"] != 0 {
		t.Fatalf("sla factor = %v, want 0 in urgent window without capacity", out[0].Factors["sla"])
	}
}

// 离线坐席 availability 清零；同分候选按 AgentID 升序保证可复现。
func TestDefaultScorerOfflineAndTieBreak(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{})
	out, err := scorer.Score(context.Background(), ScoringInput{
		SessionID: "s-5",
		Candidates: []AgentCandidate{
			candidate(9, "offline", nil, nil, 0, 5),
			candidate(4, "offline", nil, nil, 0, 5),
		},
	})
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if out[0].AgentID != 4 || out[1].AgentID != 9 {
		t.Fatalf("tie break broken: %+v", out)
	}
	if out[0].Factors["availability"] != 0 {
		t.Fatalf("offline availability = %v, want 0", out[0].Factors["availability"])
	}
}

// 权重覆盖：把 skill 权重放大后，技能不匹配的排序惩罚同步放大。
func TestDefaultScorerWeightOverride(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{Skill: 0.6, Language: 0.1, Availability: 0.1, Workload: 0.05, Priority: 0.05, Tier: 0.03, Channel: 0.04, SLA: 0.03})
	out, err := scorer.Score(context.Background(), ScoringInput{
		SessionID:  "s-6",
		Skills:     []string{"billing"},
		Candidates: []AgentCandidate{candidate(1, "online", []string{"general"}, nil, 0, 5)},
	})
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	// 技能半缺扣 0.6，其余满分。
	if math.Abs(out[0].Total-0.40) > 1e-9 {
		t.Fatalf("total = %v, want 0.40 with skill=0.6", out[0].Total)
	}
}

func TestDefaultScorerStrategyAndEmptyInput(t *testing.T) {
	scorer := NewDefaultScorer(WeightSet{})
	if scorer.Strategy() == "" {
		t.Fatal("strategy label must not be empty")
	}
	out, err := scorer.Score(context.Background(), ScoringInput{SessionID: "s-7"})
	if err != nil || out != nil {
		t.Fatalf("empty candidates = (%v, %v), want (nil, nil)", out, err)
	}
}
