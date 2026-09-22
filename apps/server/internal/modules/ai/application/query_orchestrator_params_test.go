package application

import (
	"context"
	"testing"

	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

// TestOrchestratorThreadsModelParams 两条出站路径（工具循环 / 纯 Chat）都
// 必须把 AIRequest 的模型参数原样带进 ChatRequest——这是 ai.openai.model 等
// 配置从"死配置"变回"生效配置"的管线，任一路径丢参都会静默退回 provider
// 默认模型。
func TestOrchestratorThreadsModelParams(t *testing.T) {
	cases := []struct {
		name       string
		toolPolicy ToolPolicy
	}{
		{name: "plain chat path", toolPolicy: ToolPolicy{Enabled: false}},
		{name: "tool loop path", toolPolicy: ToolPolicy{Enabled: true, MaxSteps: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "ok"}}
			orchestrator := NewQueryOrchestrator(provider, nil)
			_, err := orchestrator.Handle(context.Background(), AIRequest{
				TaskType:    TaskTypeQA,
				Query:       "测试问题",
				Model:       "test-model",
				Temperature: 0.25,
				MaxTokens:   321,
				TimeoutMs:   1500,
				ToolPolicy:  tc.toolPolicy,
			})
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			reqs := provider.RecordedRequests()
			if len(reqs) != 1 {
				t.Fatalf("recorded %d requests, want 1", len(reqs))
			}
			got := reqs[0]
			if got.Model != "test-model" {
				t.Fatalf("model = %q, want test-model", got.Model)
			}
			if got.Temperature != 0.25 {
				t.Fatalf("temperature = %v, want 0.25", got.Temperature)
			}
			if got.MaxTokens != 321 {
				t.Fatalf("max_tokens = %d, want 321", got.MaxTokens)
			}
			if got.Options.TimeoutMs != 1500 {
				t.Fatalf("options.timeout_ms = %d, want 1500", got.Options.TimeoutMs)
			}
		})
	}
}
