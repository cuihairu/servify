package server

import (
	"context"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	llm "servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"

	"github.com/sirupsen/logrus"
)

// stubHistoryLoader 会话历史读取桩：只记录调用，供接线断言。
type stubHistoryLoader struct {
	calls int
}

func (s *stubHistoryLoader) ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]models.Message, error) {
	s.calls++
	return nil, nil
}

// TestResolveLLMRuntimeHandoffParams 置信门参数随 AIRuntimeParams 全局下发：
// 默认开箱即"答不上来才建议转人工"（enabled + 0.65），显式关闭原样透传。
func TestResolveLLMRuntimeHandoffParams(t *testing.T) {
	cfg := config.GetDefaultConfig()
	_, params, err := resolveLLMRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("resolveLLMRuntime() error = %v", err)
	}
	if !params.HandoffEnabled || params.HandoffConfidenceThreshold != 0.65 {
		t.Fatalf("default handoff params = %+v, want enabled with 0.65", params)
	}

	cfg.AI.Handoff = config.HandoffConfig{Enabled: false}
	_, params, err = resolveLLMRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("resolveLLMRuntime() error = %v", err)
	}
	if params.HandoffEnabled || params.HandoffConfidenceThreshold != 0 {
		t.Fatalf("disabled handoff must pass through zero values, got %+v", params)
	}
}

// TestAttachSessionHistoryWiresAllAISurfaces 会话历史读取口回填三个 AI 面：
// scoped runtime 包装器（WS 请求级重建）、scoped handler 包装器（REST）与
// 启动装配的编排实例（pgvector 等全局路径）。nil loader 整体跳过（单轮）。
func TestAttachSessionHistoryWiresAllAISurfaces(t *testing.T) {
	cfg := config.GetDefaultConfig()
	logger := logrus.New()
	orch := aidelivery.NewOrchestratedEnhancedAIService(
		aidelivery.NewAIService("", ""),
		&llmmock.Provider{},
		nil, "", logger,
	)
	aiAssembly := &AIAssembly{
		Service:        aidelivery.NewHandlerServiceAdapter(orch),
		RuntimeService: orch,
	}
	rt := &Runtime{Config: cfg, Logger: logger}
	rt.AIService = NewScopedAIRuntimeService(cfg, logger, nil, orch, nil)
	rt.AIHandlerService = NewScopedAIHandlerService(cfg, logger, nil, aiAssembly.Service, orch, nil)

	loader := &stubHistoryLoader{}
	attachSessionHistory(rt, aiAssembly, loader)

	if v := rt.AIService.(*scopedAIRuntimeService); v.historyLoader == nil {
		t.Fatal("scoped runtime wrapper must receive the history loader")
	}
	if v := rt.AIHandlerService.(*scopedAIHandlerService); v.historyLoader == nil {
		t.Fatal("scoped handler wrapper must receive the history loader")
	}
	// 启动编排实例跨包不可窥字段：用行为探针——注入后首答会拉一次历史。
	if loader.calls != 0 {
		t.Fatalf("loader must be inert before any query, got %d calls", loader.calls)
	}
	if _, err := aiAssembly.RuntimeService.ProcessQuery(context.Background(), "问题", "sess-wire-1"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if loader.calls == 0 {
		t.Fatal("startup orchestrated instance must receive the history loader (no history fetch on query)")
	}

	// nil loader：整体跳过，不得清空已注入的实例。
	attachSessionHistory(rt, aiAssembly, nil)
	if v := rt.AIService.(*scopedAIRuntimeService); v.historyLoader == nil {
		t.Fatal("nil loader must be a no-op")
	}

	// nil receiver：包装器 setter 的 nil 安全，不得 panic。
	var nilHandler *scopedAIHandlerService
	nilHandler.WithSessionHistory(loader)
	var nilRuntime *scopedAIRuntimeService
	nilRuntime.WithSessionHistory(loader)
}

// TestSessionHistoryFlowsThroughScopedRebuild 请求级重建路径也带多轮上下文：
// WithSessionHistory 注入后 scoped 包装器持有 loader，重建时透传编排服务。
func TestSessionHistoryFlowsThroughScopedRebuild(t *testing.T) {
	cfg := config.GetDefaultConfig()
	logger := logrus.New()
	orch := aidelivery.NewOrchestratedEnhancedAIService(
		aidelivery.NewAIService("", ""),
		&llmmock.Provider{},
		nil, "", logger,
	)
	scoped := NewScopedAIRuntimeService(cfg, logger, nil, orch, nil).(*scopedAIRuntimeService)
	if scoped.historyLoader != nil {
		t.Fatal("must start without a history loader")
	}
	scoped.WithSessionHistory(&stubHistoryLoader{})
	if scoped.historyLoader == nil {
		t.Fatal("WithSessionHistory must stick on the scoped runtime wrapper")
	}
}

// copilotHistoryLoader 坐席 Copilot 接线探针桩：返回一条可用的历史消息。
type copilotHistoryLoader struct {
	calls int
}

func (c *copilotHistoryLoader) ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]models.Message, error) {
	c.calls++
	return []models.Message{{Sender: "customer", Content: "我的订单还没发货"}}, nil
}

// TestAttachSessionHistoryWiresCopilot 坐席 Copilot 与首答同用一份会话
// 历史口径：回填后 suggest_reply 会拉一次历史；rt.AICopilot 为 nil 时
// 回填不 panic（BuildAIAssembly 之外的装配形态）。
func TestAttachSessionHistoryWiresCopilot(t *testing.T) {
	cfg := config.GetDefaultConfig()
	logger := logrus.New()
	loader := &copilotHistoryLoader{}

	rt := &Runtime{Config: cfg, Logger: logger}
	rt.AICopilot = aidelivery.NewAgentCopilotService(&llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "建议"}}, aidelivery.AIRuntimeParams{})
	attachSessionHistory(rt, &AIAssembly{}, loader)

	resp, err := rt.AICopilot.Copilot(context.Background(), aidelivery.CopilotRequest{
		Action:    aidelivery.CopilotSuggestReply,
		SessionID: "sess-copilot-1",
	})
	if err != nil {
		t.Fatalf("Copilot() error = %v", err)
	}
	if resp.Text != "建议" {
		t.Fatalf("Text = %q", resp.Text)
	}
	if loader.calls == 0 {
		t.Fatal("copilot must receive the history loader (no history fetch on suggest_reply)")
	}

	// nil Copilot：回填整体 no-op，不 panic。
	attachSessionHistory(&Runtime{Config: cfg, Logger: logger}, &AIAssembly{}, loader)
}
