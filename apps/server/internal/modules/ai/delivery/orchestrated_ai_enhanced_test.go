package delivery

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"

	"github.com/sirupsen/logrus"
)

func TestOrchestratedEnhancedAIServiceProcessQueryEnhanced(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{
			ChatResponse: llm.ChatResponse{Content: "answer", TokenUsage: &llm.TokenUsage{TotalTokens: 42}},
		},
		&mockkp.Provider{
			Hits: []knowledgeprovider.KnowledgeHit{
				{DocumentID: "doc-1", Title: "Billing", Content: "Billing details", Score: 0.91},
			},
		},
		"",
		nil,
		"kb-1",
		nil,
	)

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "billing", "session-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Strategy != "weknora" {
		t.Fatalf("expected weknora strategy, got %s", resp.Strategy)
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("expected sources, got %+v", resp.Sources)
	}
	if resp.TokensUsed != 42 {
		t.Fatalf("expected tokens used to be tracked, got %d", resp.TokensUsed)
	}

	metrics := svc.GetMetrics()
	if metrics.QueryCount != 1 || metrics.SuccessCount != 1 || metrics.WeKnoraUsageCount != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestOrchestratedEnhancedAIServiceProcessQueryEnhancedWithDifyProvider(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{
			ChatResponse: llm.ChatResponse{Content: "dify-answer", TokenUsage: &llm.TokenUsage{TotalTokens: 21}},
		},
		&mockkp.Provider{
			Hits: []knowledgeprovider.KnowledgeHit{
				{DocumentID: "doc-dify-1", Title: "Refund", Content: "Dify refund policy", Score: 0.93, Source: "dify"},
			},
		},
		"dify",
		nil,
		"dataset-1",
		nil,
	)

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "refund", "session-dify-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Strategy != "dify" {
		t.Fatalf("expected dify strategy, got %s", resp.Strategy)
	}
	metrics := svc.GetMetrics()
	if metrics.DifyUsageCount != 1 || metrics.WeKnoraUsageCount != 0 {
		t.Fatalf("unexpected provider metrics: %+v", metrics)
	}
	status := svc.GetStatus(context.Background())
	if provider, _ := status["knowledge_provider"].(string); provider != "dify" {
		t.Fatalf("expected dify knowledge provider, got %+v", status)
	}
	if enabled, _ := status["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected knowledge provider enabled, got %+v", status)
	}
	if mode, _ := status["knowledge_mode"].(string); mode != "orchestrated" {
		t.Fatalf("expected orchestrated knowledge mode, got %+v", status)
	}
}

func TestOrchestratedEnhancedAIServiceUploadAndSyncWithDifyProvider(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	base.knowledgeBase.AddDocument(models.KnowledgeDoc{Title: "Billing FAQ", Content: "Billing answer", Category: "billing", Tags: "faq"})
	base.knowledgeBase.AddDocument(models.KnowledgeDoc{Title: "Refund FAQ", Content: "Refund answer", Category: "refund", Tags: "faq"})

	provider := &mockkp.Provider{}
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{},
		provider,
		"dify",
		nil,
		"dataset-1",
		nil,
	)

	if err := svc.UploadKnowledgeDocument(context.Background(), "Manual Doc", "Manual content", []string{"manual"}); err != nil {
		t.Fatalf("UploadKnowledgeDocument() error = %v", err)
	}
	if len(provider.Documents) != 1 {
		t.Fatalf("expected 1 uploaded document, got %+v", provider.Documents)
	}

	if err := svc.SyncKnowledgeBase(context.Background()); err != nil {
		t.Fatalf("SyncKnowledgeBase() error = %v", err)
	}
	if len(provider.Documents) < 3 {
		t.Fatalf("expected synced documents to be indexed via dify provider, got %+v", provider.Documents)
	}
	if _, ok := provider.Documents["Billing FAQ"]; !ok {
		t.Fatalf("expected Billing FAQ to be synced, got %+v", provider.Documents)
	}
	if _, ok := provider.Documents["Manual Doc"]; !ok {
		t.Fatalf("expected manual document upload to stay indexed, got %+v", provider.Documents)
	}
}

func TestOrchestratedEnhancedAIServiceUploadKnowledgeDocumentDisabled(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{},
		nil,
		"",
		nil,
		"",
		nil,
	)

	err := svc.UploadKnowledgeDocument(context.Background(), "Manual Doc", "Manual content", []string{"manual"})
	if err == nil {
		t.Fatal("expected error when knowledge provider is disabled, got nil")
	}
	if err.Error() != "knowledge provider is not enabled" {
		t.Fatalf("expected provider disabled error, got %v", err)
	}
}

func TestOrchestratedEnhancedAIServiceSyncKnowledgeBaseDisabled(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	base.knowledgeBase.AddDocument(models.KnowledgeDoc{
		Title:   "Billing FAQ",
		Content: "Billing answer",
	})

	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{},
		nil,
		"",
		nil,
		"",
		nil,
	)

	err := svc.SyncKnowledgeBase(context.Background())
	if err == nil {
		t.Fatal("expected error when knowledge provider is disabled, got nil")
	}
	if err.Error() != "knowledge provider is not enabled" {
		t.Fatalf("expected provider disabled error, got %v", err)
	}
}

func TestOrchestratedEnhancedAIServiceUploadAndSyncRespectEnableToggle(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	base.knowledgeBase.AddDocument(models.KnowledgeDoc{
		Title:   "Billing FAQ",
		Content: "Billing answer",
		Tags:    "faq",
	})

	provider := &mockkp.Provider{}
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{},
		provider,
		"dify",
		nil,
		"dataset-1",
		nil,
	)

	svc.SetKnowledgeProviderEnabled(false)

	if err := svc.UploadKnowledgeDocument(context.Background(), "Manual Doc", "Manual content", []string{"manual"}); err == nil {
		t.Fatal("expected upload to fail when knowledge provider disabled")
	}
	if err := svc.SyncKnowledgeBase(context.Background()); err == nil {
		t.Fatal("expected sync to fail when knowledge provider disabled")
	}

	svc.SetKnowledgeProviderEnabled(true)

	if err := svc.UploadKnowledgeDocument(context.Background(), "Manual Doc", "Manual content", []string{"manual"}); err != nil {
		t.Fatalf("expected upload to recover after enable, got %v", err)
	}
	if err := svc.SyncKnowledgeBase(context.Background()); err != nil {
		t.Fatalf("expected sync to recover after enable, got %v", err)
	}
	if _, ok := provider.Documents["Manual Doc"]; !ok {
		t.Fatalf("expected manual doc to be uploaded after re-enable, got %+v", provider.Documents)
	}
	if _, ok := provider.Documents["Billing FAQ"]; !ok {
		t.Fatalf("expected billing faq to be synced after re-enable, got %+v", provider.Documents)
	}
}

func TestOrchestratedEnhancedAIServiceFallbackAndReset(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: context.DeadlineExceeded},
		&mockkp.Provider{},
		"",
		nil,
		"kb-1",
		nil,
	)

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "Servify 是什么", "session-1")
	if err != nil {
		t.Fatalf("expected fallback response, got %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %s", resp.Strategy)
	}

	status := svc.GetStatus(context.Background())
	cb := status["circuit_breaker"].(map[string]interface{})
	if cb["failure_count"].(int) == 0 {
		t.Fatalf("expected circuit breaker failures, got %+v", cb)
	}

	metrics := svc.GetMetrics()
	if metrics.FallbackUsageCount == 0 {
		t.Fatalf("expected fallback metrics, got %+v", metrics)
	}

	svc.ResetCircuitBreaker()
	status = svc.GetStatus(context.Background())
	cb = status["circuit_breaker"].(map[string]interface{})
	if cb["failure_count"].(int) != 0 {
		t.Fatalf("expected reset circuit breaker, got %+v", cb)
	}
}

func TestOrchestratedEnhancedAIServiceFallbackLogsStrategy(t *testing.T) {
	var buf bytes.Buffer
	logger := logrus.New()
	logger.Out = &buf
	logger.Formatter = &logrus.TextFormatter{DisableColors: true}

	base := NewAIService("", "")
	base.InitializeKnowledgeBase()

	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: context.DeadlineExceeded},
		&mockkp.Provider{},
		"",
		nil,
		"kb-1",
		logger,
	)

	resp, err := svc.ProcessQueryEnhanced(context.Background(), "Servify 是什么", "session-log-1")
	if err != nil {
		t.Fatalf("expected fallback response, got %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %s", resp.Strategy)
	}

	logs := buf.String()
	for _, want := range []string{"strategy=fallback", "session_id=session-log-1", "level=warning"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected %q in fallback log, got %s", want, logs)
		}
	}
}
