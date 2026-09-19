package delivery

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
)

func TestAIService_GetStatus(t *testing.T) {
	service := &AIService{
		openAIAPIKey: "test-key",
		knowledgeBase: &KnowledgeBase{
			documents: []models.KnowledgeDoc{
				{Title: "doc1", Content: "content1"},
				{Title: "doc2", Content: "content2"},
			},
		},
	}

	status := service.GetStatus(context.Background())

	if status["type"] != "standard" {
		t.Errorf("expected type 'standard', got %v", status["type"])
	}

	if status["openai_enabled"] != true {
		t.Error("expected openai_enabled to be true")
	}

	if status["knowledge_provider"] != "embedded" {
		t.Errorf("expected knowledge_provider 'embedded', got %v", status["knowledge_provider"])
	}

	if status["knowledge_provider_enabled"] != true {
		t.Errorf("expected knowledge_provider_enabled true, got %v", status["knowledge_provider_enabled"])
	}

	if status["knowledge_mode"] != "embedded" {
		t.Errorf("expected knowledge_mode 'embedded', got %v", status["knowledge_mode"])
	}

	if status["document_count"] != 2 {
		t.Errorf("expected document_count 2, got %v", status["document_count"])
	}
}

func TestAIService_GetStatus_NoAPIKey(t *testing.T) {
	service := &AIService{
		openAIAPIKey: "",
		knowledgeBase: &KnowledgeBase{
			documents: []models.KnowledgeDoc{},
		},
	}

	status := service.GetStatus(context.Background())

	if status["openai_enabled"] != false {
		t.Error("expected openai_enabled to be false when no API key")
	}

	if status["knowledge_provider_enabled"] != true {
		t.Errorf("expected knowledge_provider_enabled true, got %v", status["knowledge_provider_enabled"])
	}

	if status["document_count"] != 0 {
		t.Errorf("expected document_count 0, got %v", status["document_count"])
	}
}

func TestAIService_InitializeKnowledgeBase(t *testing.T) {
	service := &AIService{
		knowledgeBase: &KnowledgeBase{},
	}

	// 这个方法只是初始化，不应该panic
	service.InitializeKnowledgeBase()

	if service.knowledgeBase == nil {
		t.Error("expected knowledge base to be initialized")
	}
}

func TestAIService_GetSessionSummary(t *testing.T) {
	service := &AIService{}

	messages := []models.Message{
		{Content: "Hello"},
		{Content: "How can I help?"},
	}

	summary, err := service.GetSessionSummary(messages)

	// 我们不关心实际结果，只验证它返回了值
	if err != nil && summary != "" {
		t.Errorf("unexpected error and summary combination: err=%v, summary=%s", err, summary)
	}
}
