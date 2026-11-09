package delivery

// 无依据策略的兜底层（finalize 阶段）覆盖：编排策略缺位时（runtimeParams
// 直设、未经 WithRuntimeParams 挂 orchestrator 预检），finalize 仍按策略
// 拦截零命中/低置信直答；转接端口报错时吞错告警不失败。

import (
	"context"
	"errors"
	"testing"

	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

func TestNoEvidenceLegacySafetyNetZeroHit(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(base, &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "ungrounded answer"}}, &mockkp.Provider{}, "", nil)
	// 绕过 WithRuntimeParams（不挂 orchestrator 预检），验证 finalize 兜底。
	svc.runtimeParams = AIRuntimeParams{NoEvidence: policy(NoEvidenceClarify, NoEvidenceDirect, 0)}

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-legacy-zero")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.Strategy != "clarify" || resp.Reason != "zero_hit" || resp.NextAction != "clarify" {
		t.Fatalf("legacy zero-hit safety net = %+v", resp.AIResponse)
	}
	if resp.Content == "ungrounded answer" || resp.Content == "" {
		t.Fatalf("safety net must use fixed copy, got %q", resp.Content)
	}
}

func TestNoEvidenceLegacySafetyNetLowConfidence(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(base, &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "ungrounded answer"}}, &mockkp.Provider{Hits: hits()}, "", nil)
	// 阈值 0.95 高于命中分 0.91：finalize 低置信门拦截（refuse）。
	svc.runtimeParams = AIRuntimeParams{NoEvidence: policy(NoEvidenceDirect, NoEvidenceRefuse, 0.95)}

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "question about Billing", "sess-legacy-low")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.Strategy != "refuse" || resp.Reason != "low_confidence" {
		t.Fatalf("legacy low-confidence safety net = %+v", resp.AIResponse)
	}
	if resp.Content == "ungrounded answer" || resp.Content == "" {
		t.Fatalf("refuse must use fixed copy, got %q", resp.Content)
	}
}

type failHandoffPort struct{}

func (failHandoffPort) RequestHandoff(ctx context.Context, conversationID, reason string) (map[string]interface{}, error) {
	return nil, errors.New("handoff queue down")
}

func TestNoEvidenceHandoffPortErrorSwallowed(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(base, &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "guess"}}, &mockkp.Provider{}, "", nil,
		AgentToolPorts{Handoff: failHandoffPort{}}).
		WithRuntimeParams(AIRuntimeParams{NoEvidence: policy(NoEvidenceHandoff, NoEvidenceDirect, 0)})

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-handoff-err")
	if err != nil {
		t.Fatalf("handoff port failure must not fail the response: %v", err)
	}
	if resp.NextAction != "handoff" || resp.HandoffReason != "zero_hit" {
		t.Fatalf("handoff metadata must survive port failure, got %+v", resp.AIResponse)
	}
	if resp.Content == "" {
		t.Fatal("handoff fixed copy must stay")
	}
}
