package application

// 无依据策略预检的应用层覆盖（prior batch 03803b4 留下的门禁缺口）：
// SetNoEvidencePolicy 的 nil 安全、非流式/流式的短路响应（模型不被调用）、
// 策略未命中时正常走模型、KnowledgeUnavailableError 的 Error/Unwrap 分支。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

type stubNoEvidencePolicy struct {
	decision NoEvidenceDecision
	calls    int
}

func (p *stubNoEvidencePolicy) EvaluateNoEvidence(ctx context.Context, req AIRequest, hits []knowledgeprovider.KnowledgeHit) NoEvidenceDecision {
	p.calls++
	return p.decision
}

func TestQueryOrchestratorSetNoEvidencePolicyNilSafe(t *testing.T) {
	var nilOrchestrator *QueryOrchestrator
	nilOrchestrator.SetNoEvidencePolicy(&stubNoEvidencePolicy{}) // nil 接收者：不 panic

	orchestrator := NewQueryOrchestrator(&mockllm.Provider{}, &mockkp.Provider{})
	policy := &stubNoEvidencePolicy{}
	orchestrator.SetNoEvidencePolicy(policy)
	if orchestrator.noEvidence == nil {
		t.Fatal("policy must be attached")
	}
}

func TestQueryOrchestratorNoEvidenceShortCircuitNonStream(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "guess"}}
	orchestrator := NewQueryOrchestrator(provider, &mockkp.Provider{})
	policy := &stubNoEvidencePolicy{decision: NoEvidenceDecision{Apply: true, Action: "handoff", Reason: "zero_hit"}}
	orchestrator.SetNoEvidencePolicy(policy)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query:           "unknown question",
		RetrievalPolicy: RetrievalPolicy{Enabled: true},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp == nil || !resp.NoEvidenceShort || resp.NoEvidenceAction != "handoff" || resp.NoEvidenceReason != "zero_hit" {
		t.Fatalf("expected short-circuit response, got %+v", resp)
	}
	if policy.calls != 1 {
		t.Fatalf("policy must be evaluated exactly once, got %d", policy.calls)
	}
	if len(provider.Requests) != 0 {
		t.Fatalf("model must not be called on short circuit, got %d requests", len(provider.Requests))
	}
}

func TestQueryOrchestratorNoEvidenceShortCircuitStream(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "guess"}}
	orchestrator := NewQueryOrchestrator(provider, &mockkp.Provider{})
	orchestrator.SetNoEvidencePolicy(&stubNoEvidencePolicy{
		decision: NoEvidenceDecision{Apply: true, Action: "refuse", Reason: "zero_hit"},
	})

	events, err := orchestrator.HandleStream(context.Background(), AIRequest{
		Query:           "unknown question",
		RetrievalPolicy: RetrievalPolicy{Enabled: true},
	})
	if err != nil {
		t.Fatalf("HandleStream: %v", err)
	}
	first := <-events
	if !first.Done || first.Response == nil || !first.Response.NoEvidenceShort || first.Response.NoEvidenceAction != "refuse" {
		t.Fatalf("expected terminal short-circuit event, got %+v", first)
	}
	if len(provider.Requests) != 0 {
		t.Fatalf("model must not be called on stream short circuit, got %d requests", len(provider.Requests))
	}
}

func TestQueryOrchestratorNoEvidencePolicyNotApplied(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "grounded answer"}}
	orchestrator := NewQueryOrchestrator(provider, &mockkp.Provider{})
	policy := &stubNoEvidencePolicy{decision: NoEvidenceDecision{}}
	orchestrator.SetNoEvidencePolicy(policy)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{Query: "known question"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp == nil || resp.NoEvidenceShort || resp.Content != "grounded answer" {
		t.Fatalf("policy not applied must reach model, got %+v", resp)
	}
	if policy.calls != 1 || len(provider.Requests) != 1 {
		t.Fatalf("policy evaluated once then model once: calls=%d requests=%d", policy.calls, len(provider.Requests))
	}
}

func TestKnowledgeUnavailableErrorBranches(t *testing.T) {
	if (&KnowledgeUnavailableError{}).Error() != "knowledge service unavailable" {
		t.Fatalf("empty err message wrong: %q", (&KnowledgeUnavailableError{}).Error())
	}
	if got := (&KnowledgeUnavailableError{Err: errors.New("boom")}).Error(); got != "knowledge service unavailable: boom" {
		t.Fatalf("wrapped message wrong: %q", got)
	}
	var nilErr *KnowledgeUnavailableError
	if nilErr.Error() != "knowledge service unavailable" {
		t.Fatalf("nil receiver message wrong: %q", nilErr.Error())
	}
	if nilErr.Unwrap() != nil {
		t.Fatalf("nil receiver unwrap must be nil")
	}
	if (&KnowledgeUnavailableError{}).Unwrap() != nil {
		t.Fatalf("empty unwrap must be nil")
	}
	if (&KnowledgeUnavailableError{Err: errors.New("boom")}).Unwrap() == nil {
		t.Fatalf("unwrap must surface the cause")
	}
}
