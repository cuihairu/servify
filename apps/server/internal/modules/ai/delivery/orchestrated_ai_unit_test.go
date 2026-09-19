package delivery

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

type failingUpsertProvider struct {
	*mockkp.Provider
	upsertErr error
}

func (p *failingUpsertProvider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	return "", p.upsertErr
}

func newOrchestratedForUnit(t *testing.T) (*OrchestratedEnhancedAIService, *mockllm.Provider, *mockkp.Provider) {
	t.Helper()
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	llmProvider := &mockllm.Provider{}
	kp := &mockkp.Provider{}
	svc := NewOrchestratedEnhancedAIService(base, llmProvider, kp, "weknora", nil, "kb", nil)
	return svc, llmProvider, kp
}

func TestOrchestratedAI_ProcessQueryWrapper(t *testing.T) {
	svc, llmProvider, _ := newOrchestratedForUnit(t)
	llmProvider.ChatResponse = llm.ChatResponse{Content: "wrapped answer"}

	resp, err := svc.ProcessQuery(context.Background(), " Servify 是什么 ", "sess")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if resp.Content != "wrapped answer" {
		t.Fatalf("unexpected content: %+v", resp)
	}

	// transfer shortcut inside wrapper
	transferResp, err := svc.ProcessQuery(context.Background(), "转人工", "sess")
	if err != nil {
		t.Fatalf("ProcessQuery transfer: %v", err)
	}
	if transferResp.Source != "system" {
		t.Fatalf("expected system source: %+v", transferResp)
	}
}

func TestOrchestratedAI_ProcessQueryEnhanced_ErrorPaths(t *testing.T) {
	svc, llmProvider, _ := newOrchestratedForUnit(t)
	llmProvider.ChatError = errors.New("llm down")

	// fallback enabled (default) -> legacy fallback used
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess")
	if err != nil {
		t.Fatalf("expected fallback, got %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %q", resp.Strategy)
	}

	// fallback disabled -> error surfaces
	svc.SetFallbackEnabled(false)
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected error when fallback disabled")
	}
}

func TestOrchestratedAI_ProcessQueryEnhanced_ProviderDisabled(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: errors.New("boom")},
		nil, // no knowledge provider
		"",
		nil,
		"",
		nil,
	)
	// knowledgeProviderEnabled=false -> no circuit breaker interaction, fallback still used
	resp, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess")
	if err != nil {
		t.Fatalf("expected fallback, got %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback, got %q", resp.Strategy)
	}
}

func TestOrchestratedAI_Delegates(t *testing.T) {
	svc, _, _ := newOrchestratedForUnit(t)

	if !svc.ShouldTransferToHuman("我要投诉", nil) {
		t.Fatal("expected transfer decision from base service")
	}
	summary, err := svc.GetSessionSummary([]models.Message{{Sender: "user", Content: "hi"}})
	if err != nil || summary == "" {
		t.Fatalf("GetSessionSummary: %q %v", summary, err)
	}
	svc.InitializeKnowledgeBase()
	if len(svc.base.knowledgeBase.documents) == 0 {
		t.Fatal("expected initialized knowledge base")
	}
}

func TestOrchestratedAI_ActiveOrchestrator(t *testing.T) {
	svc, _, _ := newOrchestratedForUnit(t)

	// happy path: provider allowed -> cached orchestrator
	if svc.activeOrchestrator() == nil {
		t.Fatal("expected orchestrator")
	}
	cached := svc.orchestrator
	if svc.activeOrchestrator() != cached {
		t.Fatal("expected cached orchestrator")
	}

	// nil orchestrator is rebuilt lazily
	svc.orchestrator = nil
	rebuilt := svc.activeOrchestrator()
	if rebuilt == nil || rebuilt == cached {
		t.Fatal("expected freshly built orchestrator")
	}

	// open circuit -> provider inactive -> new throwaway orchestrator each call
	svc.orchestrator = cached
	for i := 0; i < 5; i++ {
		svc.circuitBreaker.OnFailure()
	}
	if svc.circuitBreaker.Allow() {
		t.Fatal("expected circuit to be open")
	}
	throwaway := svc.activeOrchestrator()
	if throwaway == cached {
		t.Fatal("expected separate orchestrator while circuit open")
	}
	if svc.activeKnowledgeProvider() != nil {
		t.Fatal("expected nil provider while circuit open")
	}
}

func TestOrchestratedAI_GetStatus_HealthError(t *testing.T) {
	svc, _, kp := newOrchestratedForUnit(t)
	kp.HealthError = errors.New("unreachable")

	status := svc.GetStatus(context.Background())
	if status["knowledge_provider_healthy"] != false {
		t.Fatalf("expected unhealthy: %+v", status)
	}
	if status["knowledge_provider_error"] != "unreachable" {
		t.Fatalf("expected error detail: %+v", status)
	}

	kp.HealthError = nil
	if svc.GetStatus(context.Background())["knowledge_provider_healthy"] != true {
		t.Fatal("expected healthy provider")
	}
}

func TestOrchestratedAI_SyncKnowledgeBase_UpsertError(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{},
		&failingUpsertProvider{Provider: &mockkp.Provider{}, upsertErr: errors.New("write failed")},
		"dify",
		nil,
		"",
		nil,
	)
	if err := svc.SyncKnowledgeBase(context.Background()); err == nil {
		t.Fatal("expected sync error")
	}
	if err := svc.UploadKnowledgeDocument(context.Background(), "t", "c", nil); err == nil {
		t.Fatal("expected upload error")
	}
}

func TestOrchestratedAI_HelperBranches(t *testing.T) {
	if got := confidenceFromSources(nil); got != 0.6 {
		t.Fatalf("empty hits: %v", got)
	}
	if got := confidenceFromSources([]knowledgeprovider.KnowledgeHit{{Score: 0}}); got != 0.85 {
		t.Fatalf("zero score: %v", got)
	}
	if got := confidenceFromSources([]knowledgeprovider.KnowledgeHit{{Score: 0.99}}); got != 0.95 {
		t.Fatalf("capped score: %v", got)
	}
	if got := confidenceFromSources([]knowledgeprovider.KnowledgeHit{{Score: 0.7}}); got != 0.7 {
		t.Fatalf("score passthrough: %v", got)
	}

	base := NewAIService("", "")
	svc := NewOrchestratedEnhancedAIService(base, &mockllm.Provider{}, nil, "", nil, "", nil)
	if got := svc.activeKnowledgeProviderID(); got != "" {
		t.Fatalf("disabled provider id: %q", got)
	}
	svc.SetKnowledgeProviderEnabled(true)
	if got := svc.activeKnowledgeProviderID(); got != "weknora" {
		t.Fatalf("default provider id: %q", got)
	}

	// Sources conversion
	sources := toWeKnoraSources([]knowledgeprovider.KnowledgeHit{{DocumentID: "d1", Title: "T", Score: 0.5, Source: "s", Metadata: map[string]interface{}{"k": "v"}}})
	if len(sources) != 1 || sources[0].DocumentID != "d1" {
		t.Fatalf("unexpected sources: %+v", sources)
	}
	if got := toWeKnoraSources(nil); len(got) != 0 {
		t.Fatalf("expected empty sources: %+v", got)
	}
}

func TestOrchestratedAI_SetFallbackEnabled(t *testing.T) {
	svc, _, _ := newOrchestratedForUnit(t)
	svc.SetFallbackEnabled(false)
	if svc.fallbackEnabled {
		t.Fatal("expected fallback disabled")
	}
}
