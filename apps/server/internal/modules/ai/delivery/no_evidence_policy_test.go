package delivery

import (
	"context"
	"errors"
	"testing"

	knowledgeprovider "servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

// noEvidenceService constructs a service with the given policy, knowledge
// provider, and mock LLM provider.
func noEvidenceService(t *testing.T, policy NoEvidencePolicy, kp *mockkp.Provider) (*OrchestratedEnhancedAIService, *llmmock.Provider) {
	t.Helper()
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "ungrounded answer"}}
	svc := NewOrchestratedEnhancedAIService(base, provider, kp, "", nil)
	return svc.WithRuntimeParams(AIRuntimeParams{NoEvidence: policy}), provider
}

// policy returns the four canonical actions.
func policy(zero, low NoEvidenceAction, threshold float64) NoEvidencePolicy {
	return NoEvidencePolicy{ZeroHitAction: zero, LowConfidenceAction: low, ConfidenceThreshold: threshold}
}

// hits returns one confident knowledge hit.
func hits() []knowledgeprovider.KnowledgeHit {
	return []knowledgeprovider.KnowledgeHit{{DocumentID: "doc-1", Title: "Billing", Content: "Billing details", Score: 0.91}}
}

// drainAll drains a stream and returns all events.
func drainAll(t *testing.T, stream <-chan AIStreamEvent) []AIStreamEvent {
	t.Helper()
	var events []AIStreamEvent
	for event := range stream {
		events = append(events, event)
	}
	return events
}

func finalOf(t *testing.T, events []AIStreamEvent) *AIResponse {
	t.Helper()
	for _, event := range events {
		if event.Done {
			return event.Final
		}
	}
	t.Fatal("stream must contain one terminal event with Final")
	return nil
}

func TestNoEvidenceDefaultKeepsLegacyDirectAnswer(t *testing.T) {
	svc, _ := noEvidenceService(t, NoEvidencePolicy{}, &mockkp.Provider{})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-1")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.Content != "ungrounded answer" {
		t.Fatalf("legacy zero-hit direct answer must stay, got %q", resp.Content)
	}
	if resp.Reason != "" || resp.NextAction != "" || resp.HandoffReason != "" {
		t.Fatalf("legacy direct must not inject policy metadata, got %+v", resp.AIResponse)
	}
}

func TestNoEvidenceZeroHitClarify(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceClarify, NoEvidenceDirect, 0), &mockkp.Provider{})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-2")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.NextAction != "clarify" || resp.Reason != "zero_hit" || resp.HandoffReason != "" {
		t.Fatalf("clarify output = %+v", resp.AIResponse)
	}
	if resp.Content == "ungrounded answer" || resp.Content == "" {
		t.Fatalf("clarify must use fixed copy, got %q", resp.Content)
	}
}

func TestNoEvidenceZeroHitRefuse(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceRefuse, NoEvidenceDirect, 0), &mockkp.Provider{})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-3")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.Reason != "zero_hit" || resp.NextAction != "" || resp.HandoffReason != "" {
		t.Fatalf("refuse output = %+v", resp.AIResponse)
	}
	if resp.Content == "ungrounded answer" {
		t.Fatalf("refuse must replace model answer, got %q", resp.Content)
	}
}

func TestNoEvidenceZeroHitHandoff(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceHandoff, NoEvidenceDirect, 0), &mockkp.Provider{})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-4")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.NextAction != "handoff" || resp.Reason != "zero_hit" || resp.HandoffReason != "zero_hit" {
		t.Fatalf("handoff output = %+v", resp.AIResponse)
	}
}

func TestNoEvidenceLowConfidenceAppliesLowAction(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceDirect, NoEvidenceRefuse, 0.95), &mockkp.Provider{Hits: hits()})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "billing", "sess-5")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	// hits score=0.91 -> confidenceFromSources=0.91 < threshold 0.95 -> low.
	if resp.Reason != "low_confidence" || resp.NextAction != "" || resp.HandoffReason != "" {
		t.Fatalf("low-confidence refuse output = %+v", resp.AIResponse)
	}
	if resp.Content == "ungrounded answer" {
		t.Fatalf("low-confidence refuse must replace answer, got %q", resp.Content)
	}
	if resp.Strategy != "refuse" {
		t.Fatalf("Strategy = %q, want refuse", resp.Strategy)
	}
}

func TestNoEvidenceLowConfidenceDisabledKeepsHitAnswer(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceDirect, NoEvidenceRefuse, 0), &mockkp.Provider{Hits: hits()})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "billing", "sess-6")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.Content != "ungrounded answer" || resp.Reason != "" {
		t.Fatalf("disabled low gate must keep hit answer, got %+v", resp.AIResponse)
	}
}

func TestNoEvidenceStreamingZeroHitClarify(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{StreamChunks: []llm.ChatChunk{{ContentDelta: "guess"}, {Done: true}}}
	svc := NewOrchestratedEnhancedAIService(base, provider, &mockkp.Provider{}, "", nil).
		WithRuntimeParams(AIRuntimeParams{NoEvidence: policy(NoEvidenceClarify, NoEvidenceDirect, 0)})

	stream, err := svc.ProcessQueryStream(context.Background(), "unknown question", "sess-stream-1")
	if err != nil {
		t.Fatalf("ProcessQueryStream: %v", err)
	}
	final := finalOf(t, drainAll(t, stream))
	if final.NextAction != "clarify" || final.Reason != "zero_hit" {
		t.Fatalf("streaming clarify final = %+v", final)
	}
	if final.Content == "guess" || final.Content == "" {
		t.Fatalf("streaming clarify must replace model answer, got %q", final.Content)
	}
}

func TestNoEvidenceStreamingZeroHitRefuse(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{StreamChunks: []llm.ChatChunk{{ContentDelta: "guess"}, {Done: true}}}
	svc := NewOrchestratedEnhancedAIService(base, provider, &mockkp.Provider{}, "", nil).
		WithRuntimeParams(AIRuntimeParams{NoEvidence: policy(NoEvidenceRefuse, NoEvidenceDirect, 0)})

	stream, err := svc.ProcessQueryStream(context.Background(), "unknown question", "sess-stream-2")
	if err != nil {
		t.Fatalf("ProcessQueryStream: %v", err)
	}
	final := finalOf(t, drainAll(t, stream))
	if final.Reason != "zero_hit" || final.NextAction != "" {
		t.Fatalf("streaming refuse final = %+v", final)
	}
	if final.Content == "guess" {
		t.Fatalf("streaming refuse must replace answer, got %q", final.Content)
	}
}

func TestNoEvidenceStreamingLowConfidenceHandoff(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{StreamChunks: []llm.ChatChunk{{ContentDelta: "answer"}, {Done: true}}}
	svc := NewOrchestratedEnhancedAIService(base, provider, &mockkp.Provider{Hits: hits()}, "", nil).
		WithRuntimeParams(AIRuntimeParams{NoEvidence: policy(NoEvidenceDirect, NoEvidenceHandoff, 0.95)})

	stream, err := svc.ProcessQueryStream(context.Background(), "billing", "sess-stream-3")
	if err != nil {
		t.Fatalf("ProcessQueryStream: %v", err)
	}
	final := finalOf(t, drainAll(t, stream))
	if final.NextAction != "handoff" || final.Reason != "low_confidence" {
		t.Fatalf("streaming low-confidence handoff final = %+v", final)
	}
}

func TestNoEvidenceKnowledgeServiceFailureFallsBack(t *testing.T) {
	// 检索故障：配置了非直答策略时按 knowledge_unavailable 响应，不吞成
	// 零命中、也不让模型继续编造。
	svc, _ := noEvidenceService(t, policy(NoEvidenceRefuse, NoEvidenceDirect, 0), &mockkp.Provider{SearchError: errors.New("retrieval down")})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "any question", "sess-err")
	if err != nil {
		t.Fatalf("knowledge unavailable must be served by policy, got %v", err)
	}
	if resp.Reason != "knowledge_unavailable" || resp.Content == "" {
		t.Fatalf("knowledge-unavailable refuse output = %+v", resp.AIResponse)
	}
}

func TestNoEvidenceKnowledgeServiceFailureDirectKeepsFallback(t *testing.T) {
	// 未配置策略（direct）：知识故障仍走历史 fallback 兜底（不报错）。
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "unused"}}
	svc := NewOrchestratedEnhancedAIService(base, provider, &mockkp.Provider{SearchError: errors.New("retrieval down")}, "", nil).
		WithRuntimeParams(AIRuntimeParams{NoEvidence: policy(NoEvidenceDirect, NoEvidenceDirect, 0)})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "any question", "sess-err2")
	if err != nil {
		t.Fatalf("direct policy must keep fallback serving, got %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("Strategy = %q, want fallback", resp.Strategy)
	}
}

func TestNoEvidenceStreamingKnowledgeUnavailableRefuse(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	provider := &llmmock.Provider{}
	svc := NewOrchestratedEnhancedAIService(base, provider, &mockkp.Provider{SearchError: errors.New("retrieval down")}, "", nil).
		WithRuntimeParams(AIRuntimeParams{NoEvidence: policy(NoEvidenceRefuse, NoEvidenceDirect, 0)})
	stream, err := svc.ProcessQueryStream(context.Background(), "any question", "sess-stream-err")
	if err != nil {
		t.Fatalf("knowledge-unavailable stream must be served by policy, got %v", err)
	}
	final := finalOf(t, drainAll(t, stream))
	if final.Reason != "knowledge_unavailable" || final.Content == "" {
		t.Fatalf("streaming knowledge-unavailable refuse final = %+v", final)
	}
}

func TestNoEvidenceExplicitTransferWins(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceRefuse, NoEvidenceDirect, 0), &mockkp.Provider{})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "我要转人工", "sess-transfer")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.NextAction != "" || resp.Reason != "" || resp.Content == "" || resp.Source != "system" {
		t.Fatalf("explicit transfer short-circuit must win, got %+v", resp.AIResponse)
	}
}

func TestNoEvidenceNormalizeUnknownAction(t *testing.T) {
	svc, _ := noEvidenceService(t, policy(NoEvidenceAction("bogus"), NoEvidenceAction("bogus"), 0), &mockkp.Provider{})
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "unknown question", "sess-normalize")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp.Content != "ungrounded answer" || resp.Reason != "" {
		t.Fatalf("unknown action must normalize to direct, got %+v", resp.AIResponse)
	}
}
