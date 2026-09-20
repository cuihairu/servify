package delivery

import (
	"context"
	"testing"

	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

// promMetricRow 是从 Registry Gather 结果里取出的单个样本。
type promMetricRow struct {
	labels map[string]string
	value  float64
}

// gatherPromRows 抓取指定指标名的全部样本（含标签），供打点断言。
func gatherPromRows(t *testing.T, reg *svcmetrics.Registry, name string) []promMetricRow {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	var rows []promMetricRow
	for _, mf := range mfs {
		if mf.GetName() != name || mf.GetMetric() == nil {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := make(map[string]string)
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if m.GetCounter() != nil {
				rows = append(rows, promMetricRow{labels: labels, value: m.GetCounter().GetValue()})
			} else if m.GetHistogram() != nil {
				rows = append(rows, promMetricRow{labels: labels, value: float64(m.GetHistogram().GetSampleCount())})
			}
		}
	}
	return rows
}

func promRowValue(t *testing.T, reg *svcmetrics.Registry, name string, wantLabels map[string]string) float64 {
	t.Helper()
	for _, row := range gatherPromRows(t, reg, name) {
		if len(row.labels) != len(wantLabels) {
			continue
		}
		matched := true
		for k, v := range wantLabels {
			if row.labels[k] != v {
				matched = false
				break
			}
		}
		if matched {
			return row.value
		}
	}
	t.Fatalf("metric %s with labels %v not found", name, wantLabels)
	return 0
}

func newMeteredOrchestrator(t *testing.T, base *AIService) (*OrchestratedEnhancedAIService, *mockllm.Provider, *mockkp.Provider, *svcmetrics.Registry) {
	t.Helper()
	reg := svcmetrics.NewRegistry()
	bm := svcmetrics.NewBusinessMetrics(reg)
	llmProvider := &mockllm.Provider{}
	kp := &mockkp.Provider{}
	if base == nil {
		base = NewAIService("", "")
		base.InitializeKnowledgeBase()
	}
	svc := NewOrchestratedEnhancedAIService(base, llmProvider, kp, "weknora", nil).AttachBusinessMetrics(bm)
	return svc, llmProvider, kp, reg
}

func TestOrchestratedAI_PromMetricsTransfer(t *testing.T) {
	svc, _, _, reg := newMeteredOrchestrator(t, nil)

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "转人工", "sess"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if got := promRowValue(t, reg, "ai_requests_total", map[string]string{
		"provider": "internal", "model": "", "outcome": "success", "strategy": "transfer",
	}); got != 1 {
		t.Fatalf("transfer counter = %v, want 1", got)
	}
}

func TestOrchestratedAI_PromMetricsPrimaryHitAndTokens(t *testing.T) {
	svc, llmProvider, kp, reg := newMeteredOrchestrator(t, nil)
	kp.Hits = []knowledgeprovider.KnowledgeHit{{Title: "Servify 是什么", Content: "智能客服平台"}}
	llmProvider.ChatResponse = llm.ChatResponse{
		Content:    "answer",
		TokenUsage: &llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "Servify 是什么", "sess"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if got := promRowValue(t, reg, "ai_requests_total", map[string]string{
		"provider": "weknora", "model": "", "outcome": "success", "strategy": "primary",
	}); got != 1 {
		t.Fatalf("primary counter = %v, want 1", got)
	}
	if got := promRowValue(t, reg, "ai_llm_tokens_total", map[string]string{
		"provider": "weknora", "token_type": "input",
	}); got != 10 {
		t.Fatalf("input tokens = %v, want 10", got)
	}
	if got := promRowValue(t, reg, "ai_llm_tokens_total", map[string]string{
		"provider": "weknora", "token_type": "output",
	}); got != 5 {
		t.Fatalf("output tokens = %v, want 5", got)
	}
	if got := promRowValue(t, reg, "ai_request_duration_seconds", map[string]string{
		"provider": "weknora", "model": "",
	}); got != 1 {
		t.Fatalf("duration observations = %v, want 1", got)
	}
}

func TestOrchestratedAI_PromMetricsFallbackOnOrchestratorError(t *testing.T) {
	svc, llmProvider, _, reg := newMeteredOrchestrator(t, nil)
	llmProvider.ChatError = context.DeadlineExceeded

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if got := promRowValue(t, reg, "ai_requests_total", map[string]string{
		"provider": "weknora", "model": "", "outcome": "success", "strategy": "fallback",
	}); got != 1 {
		t.Fatalf("fallback counter = %v, want 1", got)
	}
}

func TestOrchestratedAI_PromMetricsFallbackOnMissingSources(t *testing.T) {
	svc, llmProvider, _, reg := newMeteredOrchestrator(t, nil)
	// llm 正常但知识库无命中：Handle 成功、sources 为空 → 记 strategy=fallback。
	llmProvider.ChatResponse = llm.ChatResponse{Content: "answer"}

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "冷门问题", "sess"); err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if got := promRowValue(t, reg, "ai_requests_total", map[string]string{
		"provider": "weknora", "model": "", "outcome": "success", "strategy": "fallback",
	}); got != 1 {
		t.Fatalf("no-sources fallback counter = %v, want 1", got)
	}
}

func TestOrchestratedAI_PromMetricsPrimaryFailureWithoutFallback(t *testing.T) {
	svc, llmProvider, _, reg := newMeteredOrchestrator(t, nil)
	llmProvider.ChatError = context.DeadlineExceeded
	svc.SetFallbackEnabled(false)

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected error when fallback disabled")
	}
	if got := promRowValue(t, reg, "ai_requests_total", map[string]string{
		"provider": "weknora", "model": "", "outcome": "failure", "strategy": "primary",
	}); got != 1 {
		t.Fatalf("primary failure counter = %v, want 1", got)
	}
}

func TestOrchestratedAI_PromMetricsFallbackFailureUsesNoneProvider(t *testing.T) {
	// 无外部 provider（provider 标签 none）且 base 也失败：outcome=failure + strategy=fallback。
	base := NewAIService("sk-unreachable", "http://127.0.0.1:1")
	base.InitializeKnowledgeBase()
	reg := svcmetrics.NewRegistry()
	bm := svcmetrics.NewBusinessMetrics(reg)
	svc := NewOrchestratedEnhancedAIService(base, &mockllm.Provider{ChatError: context.DeadlineExceeded}, nil, "", nil).AttachBusinessMetrics(bm)

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected fallback failure")
	}
	if got := promRowValue(t, reg, "ai_requests_total", map[string]string{
		"provider": "none", "model": "", "outcome": "failure", "strategy": "fallback",
	}); got != 1 {
		t.Fatalf("fallback failure counter = %v, want 1", got)
	}
}

func TestOrchestratedAI_PromMetricsNilReceiverAttach(t *testing.T) {
	var svc *OrchestratedEnhancedAIService
	if got := svc.AttachBusinessMetrics(nil); got != nil {
		t.Fatalf("nil receiver Attach 应返回 nil，got %v", got)
	}
}

func TestOrchestratedAI_PromMetricsNilMeterNoop(t *testing.T) {
	// 未 Attach 指标（nil）时全部打点路径静默降级，不 panic。
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(base, &mockllm.Provider{ChatError: context.DeadlineExceeded}, nil, "", nil)

	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err != nil {
		t.Fatalf("nil-meter fallback: %v", err)
	}
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "转人工", "sess"); err != nil {
		t.Fatalf("nil-meter transfer: %v", err)
	}
}
