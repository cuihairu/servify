package application

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

// stepProvider returns canned ChatResponses in sequence, one per call.
type stepProvider struct {
	responses []llm.ChatResponse
	callCount int
}

func (p *stepProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	if p.callCount >= len(p.responses) {
		return p.responses[len(p.responses)-1], nil
	}
	resp := p.responses[p.callCount]
	p.callCount++
	return resp, nil
}

func (p *stepProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return nil, errors.New("not implemented")
}

func (p *stepProvider) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func (p *stepProvider) HealthCheck(_ context.Context) error { return nil }

type stubPolicyHook struct {
	decision PolicyDecision
	err      error
}

func (s stubPolicyHook) Evaluate(ctx context.Context, req AIRequest) (PolicyDecision, error) {
	return s.decision, s.err
}

type stubAuditRecorder struct {
	records []PromptAuditRecord
}

func (s *stubAuditRecorder) RecordPrompt(ctx context.Context, record PromptAuditRecord) error {
	s.records = append(s.records, record)
	return nil
}

func TestQueryOrchestratorHandleWithRetrieval(t *testing.T) {
	llmProvider := &mockllm.Provider{
		ChatResponse: llm.ChatResponse{
			Content:      "answer",
			Provider:     "openai",
			Model:        "mock-model",
			FinishReason: "stop",
			TokenUsage: &llm.TokenUsage{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		},
	}
	knowledgeProvider := &mockkp.Provider{
		Hits: []knowledgeprovider.KnowledgeHit{
			{DocumentID: "doc-1", Title: "Billing", Content: "Billing help content", Score: 0.9},
		},
	}

	orchestrator := NewQueryOrchestrator(llmProvider, knowledgeProvider)
	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		TenantID:     "tenant-1",
		TaskType:     TaskTypeQA,
		Query:        "billing",
		SystemPrompt: "You are helpful.",
		RetrievalPolicy: RetrievalPolicy{
			Enabled: true,
			TopK:    3,
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "answer" {
		t.Fatalf("expected answer, got %s", resp.Content)
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resp.Sources))
	}
	if resp.Model != "mock-model" {
		t.Fatalf("expected mock-model, got %s", resp.Model)
	}
	if resp.Provider != "openai" {
		t.Fatalf("expected openai provider, got %s", resp.Provider)
	}
}

func TestQueryOrchestratorHandleWithoutKnowledgeProvider(t *testing.T) {
	llmProvider := &mockllm.Provider{
		ChatResponse: llm.ChatResponse{
			Content: "fallback answer",
		},
	}

	orchestrator := NewQueryOrchestrator(llmProvider, nil)
	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		TaskType: TaskTypeQA,
		Query:    "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "fallback answer" {
		t.Fatalf("expected fallback answer, got %s", resp.Content)
	}
	if len(resp.Sources) != 0 {
		t.Fatalf("expected no sources, got %d", len(resp.Sources))
	}
}

func TestRetrieverDisabledReturnsNoHits(t *testing.T) {
	retriever := NewRetriever(&mockkp.Provider{
		Hits: []knowledgeprovider.KnowledgeHit{
			{DocumentID: "doc-1", Title: "Ignored", Content: "Ignored"},
		},
	})

	hits, err := retriever.Retrieve(context.Background(), AIRequest{
		Query: "ignored",
		RetrievalPolicy: RetrievalPolicy{
			Enabled: false,
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(hits))
	}
}

func TestQueryOrchestratorGuardrailsRejectBlockedInput(t *testing.T) {
	llmProvider := &mockllm.Provider{
		ChatResponse: llm.ChatResponse{Content: "should not be returned"},
	}
	orchestrator := NewQueryOrchestrator(llmProvider, nil)

	_, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "please run DROP TABLE users",
	})
	if err == nil {
		t.Fatal("expected guardrail error")
	}

	metrics := orchestrator.Metrics()
	if metrics.QueryCount != 1 || metrics.FallbackCount != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestQueryOrchestratorSanitizesLongOutputAndTracksMetrics(t *testing.T) {
	longContent := ""
	for i := 0; i < 2500; i++ {
		longContent += "a"
	}
	llmProvider := &mockllm.Provider{
		ChatResponse: llm.ChatResponse{
			Content: longContent,
			Model:   "mock-model",
			TokenUsage: &llm.TokenUsage{
				TotalTokens: 123,
			},
		},
	}
	orchestrator := NewQueryOrchestrator(llmProvider, nil)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !resp.Truncated {
		t.Fatal("expected output to be truncated")
	}
	if len(resp.Content) != 2000 {
		t.Fatalf("expected truncated length 2000, got %d", len(resp.Content))
	}

	metrics := orchestrator.Metrics()
	if metrics.QueryCount != 1 || metrics.SuccessCount != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if metrics.LastTokenUsage != 123 {
		t.Fatalf("unexpected token usage: %+v", metrics)
	}
}

func TestQueryOrchestratorReturnsErrorWhenRetrieverFails(t *testing.T) {
	llmProvider := &mockllm.Provider{
		ChatResponse: llm.ChatResponse{Content: "unused"},
	}
	orchestrator := NewQueryOrchestrator(llmProvider, &mockkp.Provider{
		SearchError: context.DeadlineExceeded,
	})

	_, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "billing",
		RetrievalPolicy: RetrievalPolicy{
			Enabled: true,
		},
	})
	if err == nil {
		t.Fatal("expected retrieval error")
	}

	metrics := orchestrator.Metrics()
	if metrics.FallbackCount != 1 {
		t.Fatalf("expected fallback count to increment, got %+v", metrics)
	}
}

func TestQueryOrchestratorPropagatesMessagesWithoutAppendingQuery(t *testing.T) {
	llmProvider := &mockllm.Provider{
		ChatResponse: llm.ChatResponse{Content: "answer", Provider: "anthropic"},
	}
	orchestrator := NewQueryOrchestrator(llmProvider, nil)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "latest question",
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "history question"},
			{Role: "assistant", Content: "history answer"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Fatalf("expected anthropic provider, got %s", resp.Provider)
	}
}

func TestQueryOrchestratorPolicyHookRejectsRequest(t *testing.T) {
	orchestrator := NewQueryOrchestrator(&mockllm.Provider{}, nil)
	orchestrator.SetPolicyHooks(stubPolicyHook{
		decision: PolicyDecision{Allowed: false, Reason: "policy_denied"},
	})

	_, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "blocked",
	})
	if err == nil {
		t.Fatal("expected policy rejection")
	}

	metrics := orchestrator.Metrics()
	if metrics.PolicyRejectCount != 1 || metrics.FallbackCount != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestQueryOrchestratorPolicyHookErrorCountsAsFailure(t *testing.T) {
	orchestrator := NewQueryOrchestrator(&mockllm.Provider{}, nil)
	orchestrator.SetPolicyHooks(stubPolicyHook{
		err: errors.New("policy backend down"),
	})

	_, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "hello",
	})
	if err == nil {
		t.Fatal("expected policy hook error")
	}

	metrics := orchestrator.Metrics()
	if metrics.ErrorCount != 1 || metrics.LastErrorCategory != "policy_hook_error" {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestQueryOrchestratorWritesPromptAuditRecord(t *testing.T) {
	recorder := &stubAuditRecorder{}
	orchestrator := NewQueryOrchestrator(&mockllm.Provider{
		ChatResponse: llm.ChatResponse{Content: "answer", Provider: "openai"},
	}, nil)
	orchestrator.SetAuditRecorder(recorder)

	_, err := orchestrator.Handle(context.Background(), AIRequest{
		TaskType:     TaskTypeQA,
		Query:        "hello",
		SystemPrompt: "system",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(recorder.records))
	}
	if recorder.records[0].PromptVersion != "v1" {
		t.Fatalf("unexpected audit record: %+v", recorder.records[0])
	}
}

// stubTool is a minimal Tool implementation for agent-loop testing.
type stubTool struct {
	name   string
	desc   string
	schema map[string]interface{}
	result map[string]interface{}
	err    error
}

func (t *stubTool) Name() string                   { return t.name }
func (t *stubTool) Description() string            { return t.desc }
func (t *stubTool) Schema() map[string]interface{} { return t.schema }
func (t *stubTool) Execute(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	return t.result, t.err
}

func TestQueryOrchestratorAgentLoopExecutesToolThenReturnsText(t *testing.T) {
	provider := &stepProvider{
		responses: []llm.ChatResponse{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:   "call-1",
					Name: "test_tool",
					Arguments: map[string]interface{}{
						"input": "hello",
					},
				}},
			},
			{
				Content:      "tool execution complete",
				FinishReason: "stop",
				TokenUsage:   &llm.TokenUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&stubTool{
		name:   "test_tool",
		desc:   "A test tool",
		schema: map[string]interface{}{"type": "object"},
		result: map[string]interface{}{"status": "done"},
	})
	executor := NewToolExecutor(registry, nil)
	orchestrator := NewQueryOrchestrator(provider, nil)
	orchestrator.SetToolExecutor(executor)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "run test tool",
		ToolPolicy: ToolPolicy{
			Enabled:  true,
			MaxSteps: 5,
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "tool execution complete" {
		t.Fatalf("expected final content, got %q", resp.Content)
	}
	if provider.callCount != 2 {
		t.Fatalf("expected 2 LLM calls (tool + follow-up), got %d", provider.callCount)
	}
	if resp.TokenUsage == nil || resp.TokenUsage.TotalTokens == 0 {
		t.Fatalf("expected non-zero token usage, got %+v", resp.TokenUsage)
	}

	metrics := orchestrator.Metrics()
	if metrics.ToolCallCount != 1 {
		t.Fatalf("expected 1 tool call metric, got %d", metrics.ToolCallCount)
	}
}

func TestQueryOrchestratorAgentLoopMaxStepsReached(t *testing.T) {
	provider := &stepProvider{
		responses: []llm.ChatResponse{
			{Content: "", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "test_tool", Arguments: map[string]interface{}{}}}},
			{Content: "", ToolCalls: []llm.ToolCall{{ID: "call-2", Name: "test_tool", Arguments: map[string]interface{}{}}}},
			{Content: "", ToolCalls: []llm.ToolCall{{ID: "call-3", Name: "test_tool", Arguments: map[string]interface{}{}}}},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&stubTool{name: "test_tool", desc: "tool", result: map[string]interface{}{"ok": true}})
	executor := NewToolExecutor(registry, nil)
	orchestrator := NewQueryOrchestrator(provider, nil)
	orchestrator.SetToolExecutor(executor)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "loop",
		ToolPolicy: ToolPolicy{
			Enabled:  true,
			MaxSteps: 2,
		},
	})
	if err != nil {
		t.Fatalf("expected no error from max-steps termination, got %v", err)
	}
	// Should stop after step 2 (maxSteps=2), never making the 3rd call in responses.
	if provider.callCount != 2 {
		t.Fatalf("expected 2 LLM calls (maxSteps), got %d", provider.callCount)
	}
	// Last response's content (empty) should be returned.
	_ = resp
}

func TestQueryOrchestratorAgentLoopDisabledWhenNoToolPolicy(t *testing.T) {
	provider := &stepProvider{
		responses: []llm.ChatResponse{
			{Content: "direct answer", FinishReason: "stop"},
		},
	}
	orchestrator := NewQueryOrchestrator(provider, nil)
	// No SetToolExecutor — tool path is skipped.

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "direct answer" {
		t.Fatalf("expected direct answer, got %q", resp.Content)
	}
	if provider.callCount != 1 {
		t.Fatalf("expected 1 LLM call, got %d", provider.callCount)
	}
}

func TestQueryOrchestratorToolErrorDoesNotCrashLoop(t *testing.T) {
	provider := &stepProvider{
		responses: []llm.ChatResponse{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:        "call-err",
					Name:      "failing_tool",
					Arguments: map[string]interface{}{"x": 1},
				}},
			},
			{
				Content:      "recovered",
				FinishReason: "stop",
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&stubTool{
		name: "failing_tool",
		desc: "always fails",
		err:  errors.New("internal error"),
	})
	executor := NewToolExecutor(registry, nil)
	orchestrator := NewQueryOrchestrator(provider, nil)
	orchestrator.SetToolExecutor(executor)

	resp, err := orchestrator.Handle(context.Background(), AIRequest{
		Query: "fail",
		ToolPolicy: ToolPolicy{
			Enabled:  true,
			MaxSteps: 5,
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "recovered" {
		t.Fatalf("expected recovery content, got %q", resp.Content)
	}
}
