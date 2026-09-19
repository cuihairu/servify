package delivery

// AI 编排落库错误分支（原 auth/satisfaction 段已分别随 auth、satisfaction
// 模块迁移至各自 internal/modules/*/application）。

import (
	"context"
	"errors"
	"testing"

	mockllm "servify/apps/server/internal/platform/llm/mock"
)

func TestOrchestratedAI_ProcessQueryError(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: errors.New("llm unavailable")},
		nil,
		"",
		nil,
		"",
		nil,
	)
	// no provider + no fallback configured -> orchestrator error surfaces when LLM fails
	svc.SetFallbackEnabled(false)
	if _, err := svc.ProcessQuery(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected ProcessQuery error")
	}
}
