package delivery

import (
	"context"
	"testing"

	mockllm "servify/apps/server/internal/platform/llm/mock"
)

// TestWithRuntimeParamsStampsAIRequest 注入的出站模型参数必须落进每次
// AIRequest——ai.provider 对应配置族的 model/temperature/max_tokens/timeout
// 唯一生效入口在这里，缺一章配置就静默退回 provider 默认（历史死配置
// 事故的回归防线）。nil 安全与链式语义一并覆盖。
func TestWithRuntimeParamsStampsAIRequest(t *testing.T) {
	provider := &mockllm.Provider{}
	svc := NewOrchestratedEnhancedAIService(NewAIService("", ""), provider, nil, "", nil)
	if svc.WithRuntimeParams(AIRuntimeParams{}) != svc {
		t.Fatal("WithRuntimeParams must return the same service for chaining")
	}
	// nil 接收者不 panic（与 AttachBusinessMetrics 同款 nil 安全）。
	var nilSvc *OrchestratedEnhancedAIService
	if nilSvc.WithRuntimeParams(AIRuntimeParams{Model: "x"}) != nilSvc {
		t.Fatal("nil receiver must be preserved")
	}

	svc.WithRuntimeParams(AIRuntimeParams{
		Model:       "params-model",
		Temperature: 0.4,
		MaxTokens:   256,
		TimeoutMs:   900,
	})
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "如何重置密码", "sess-1"); err != nil {
		t.Fatalf("ProcessQueryEnhanced() error = %v", err)
	}
	reqs := provider.RecordedRequests()
	if len(reqs) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Model != "params-model" || got.Temperature != 0.4 ||
		got.MaxTokens != 256 || got.Options.TimeoutMs != 900 {
		t.Fatalf("stamped params = (model=%q temp=%v max=%d timeout=%d), want (params-model, 0.4, 256, 900)",
			got.Model, got.Temperature, got.MaxTokens, got.Options.TimeoutMs)
	}
}

// TestWithoutRuntimeParamsKeepsLegacyDefaults 不注入时出站请求保持零值，
// provider 侧默认兜底——"忘记链不改变现状"的兼容承诺。
func TestWithoutRuntimeParamsKeepsLegacyDefaults(t *testing.T) {
	provider := &mockllm.Provider{}
	svc := NewOrchestratedEnhancedAIService(NewAIService("", ""), provider, nil, "", nil)
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "如何重置密码", "sess-1"); err != nil {
		t.Fatalf("ProcessQueryEnhanced() error = %v", err)
	}
	reqs := provider.RecordedRequests()
	if len(reqs) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Model != "" || got.Temperature != 0 || got.MaxTokens != 0 || got.Options.TimeoutMs != 0 {
		t.Fatalf("legacy params must stay zero, got %+v", got)
	}
}
