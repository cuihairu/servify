//go:build integration
// +build integration

package delivery

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

func TestOrchestratedEnhancedAIServiceFallbackWhenKnowledgeProviderFailsIntegration(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	service := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{
			ChatResponse: llm.ChatResponse{Content: "llm answer"},
		},
		&mockkp.Provider{
			SearchError: errors.New("knowledge provider unavailable"),
		},
		"",
		nil,
		"kb-1",
		nil,
	)

	resp, err := service.ProcessQueryEnhanced(context.Background(), "退款政策", "session-knowledge-fallback")
	if err != nil {
		t.Fatalf("expected fallback response, got error: %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %s", resp.Strategy)
	}

	status := service.GetStatus(context.Background())
	cb := status["circuit_breaker"].(map[string]interface{})
	if cb["failure_count"].(int) == 0 {
		t.Fatalf("expected circuit breaker to record knowledge provider failure, got %+v", cb)
	}
}

func TestOrchestratedEnhancedAIServiceFallbackWhenLLMProviderFailsIntegration(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	service := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{
			ChatError: context.DeadlineExceeded,
		},
		&mockkp.Provider{
			Hits: []knowledgeprovider.KnowledgeHit{
				{DocumentID: "doc-1", Title: "退款", Content: "退款说明", Score: 0.92},
			},
		},
		"",
		nil,
		"kb-1",
		nil,
	)

	resp, err := service.ProcessQueryEnhanced(context.Background(), "退款", "session-llm-fallback")
	if err != nil {
		t.Fatalf("expected fallback response, got error: %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %s", resp.Strategy)
	}

	metrics := service.GetMetrics()
	if metrics.FallbackUsageCount == 0 {
		t.Fatalf("expected fallback metrics to be recorded, got %+v", metrics)
	}
}
