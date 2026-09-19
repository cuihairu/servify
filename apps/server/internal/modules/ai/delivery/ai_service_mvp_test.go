package delivery

import (
	"context"
	aimodule "servify/apps/server/internal/modules/ai/application"
	"testing"

	"servify/apps/server/internal/models"
)

func TestAIService_InitializeKnowledgeBase_AddsDocs(t *testing.T) {
	svc := NewAIService("", "https://api.openai.com/v1")
	svc.InitializeKnowledgeBase()
	if svc.knowledgeBase == nil {
		t.Fatalf("knowledgeBase is nil")
	}
	if len(svc.knowledgeBase.documents) == 0 {
		t.Fatalf("expected default documents > 0")
	}
}

func TestAIService_ProcessQuery_Fallback_NoAPIKey(t *testing.T) {
	svc := NewAIService("", "https://api.openai.com/v1")
	svc.InitializeKnowledgeBase()
	ctx := context.Background()
	resp, err := svc.ProcessQuery(ctx, "你好，帮我介绍一下Servify", "test-session")
	if err != nil {
		t.Fatalf("ProcessQuery error: %v", err)
	}
	if resp == nil || resp.Content == "" {
		t.Fatalf("expected non-empty content in response")
	}
	if resp.Confidence < 0.0 || resp.Confidence > 1.0 {
		t.Fatalf("unexpected confidence: %v", resp.Confidence)
	}
}

func TestAIService_ShouldTransferToHuman(t *testing.T) {
	if !aimodule.ShouldTransferToHuman("请帮我转人工客服", nil) {
		t.Fatalf("expected true for human transfer keywords")
	}
	if aimodule.ShouldTransferToHuman("简单问题咨询", []models.Message{}) {
		t.Fatalf("expected false for normal query")
	}
}

func TestAIService_GetSessionSummary_NoAPIKey(t *testing.T) {
	svc := NewAIService("", "")
	msgs := []models.Message{
		{Sender: "user", Content: "你好"},
		{Sender: "ai", Content: "您好！有什么可以帮您？"},
	}
	s, err := svc.GetSessionSummary(msgs)
	if err != nil {
		t.Fatalf("GetSessionSummary error: %v", err)
	}
	if s == "" {
		t.Fatalf("expected non-empty summary")
	}
}
