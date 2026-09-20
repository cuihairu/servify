package delivery

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

func TestAIService_NewRequestError(t *testing.T) {
	svc := NewAIService("key", "http://127.0.0.1:1\x7f")
	if _, err := svc.callOpenAI(context.Background(), "p"); err == nil {
		t.Fatal("expected request creation error")
	}
}

func TestOrchestratedAI_FallbackErrorBranch(t *testing.T) {
	server := httptest.NewServer(nil)
	url := server.URL
	server.Close()

	base := NewAIService("key", url) // fallback path also fails
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: errors.New("llm down")},
		&mockkp.Provider{},
		"",
		nil,
	)
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected fallback error propagation")
	}
}
