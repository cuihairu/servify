package server

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/sirupsen/logrus"
)

// TestScopedAIHandlerAIConfigNilCfg aiConfig 的 nil-cfg 兜底分支：测试与
// 降级路径会以残缺 handler 调用请求级重建，不允许 panic。
func TestScopedAIHandlerAIConfigNilCfg(t *testing.T) {
	s := &scopedAIHandlerService{}
	if got := s.aiConfig(); got.Provider != "" || got.Anthropic.APIKey != "" {
		t.Fatalf("nil cfg must yield zero AIConfig, got %+v", got)
	}
	s.cfg = &config.Config{}
	s.cfg.AI.Provider = "anthropic"
	if got := s.aiConfig(); got.Provider != "anthropic" {
		t.Fatalf("cfg.AI not returned, got %+v", got)
	}
}

// TestRuntimeServiceFromResolvedConfigUnknownProviderFallsBack 请求级重建
// 遇到非法选型：启动 gate 正常情况下已在进程入口拦截，此处防御性兜底——
// 退回 openai provider 让请求继续，而不是让会话内 AI 整体失败。
func TestRuntimeServiceFromResolvedConfigUnknownProviderFallsBack(t *testing.T) {
	buf := &strings.Builder{}
	logger := logrus.New()
	logger.SetOutput(buf)

	service := runtimeServiceFromResolvedConfig(
		config.OpenAIConfig{APIKey: "k"},
		config.DifyConfig{}, config.RagFlowConfig{}, config.WeKnoraConfig{},
		config.AIConfig{Provider: "bogus"}, logger, nil,
	)
	if service == nil {
		t.Fatal("expected fallback runtime service")
	}
	status := service.GetStatus(context.Background())
	if status == nil {
		t.Fatal("expected service status after fallback")
	}
	if !strings.Contains(buf.String(), "falling back to openai provider") {
		t.Fatalf("expected openai fallback warning, got %q", buf.String())
	}
}

// TestBuildQualityServiceFactoryErrorFallsBackRulesOnly 装配层 gate 在
// BuildAIAssembly 先行拦截全局非法选型；本测试直接构造 Runtime 绕过启动
// 链，证明 buildQualityService 自身对 factory 错误也有 rules-only 兜底。
func TestBuildQualityServiceFactoryErrorFallsBackRulesOnly(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Quality.Enabled = true
	cfg.Quality.LLM.Enabled = true
	cfg.AI.Provider = "bogus-provider"
	cfg.AI.OpenAI.APIKey = "quality-test-key"

	buf := &strings.Builder{}
	logger := logrus.New()
	logger.SetOutput(buf)

	rt := &Runtime{Config: cfg, DB: newRuntimeTestDB(t), Logger: logger}
	svc := rt.buildQualityService()
	if svc == nil {
		t.Fatal("quality must fall back to rules-only instead of disappearing")
	}
	if !strings.Contains(buf.String(), "build llm provider") {
		t.Fatalf("expected factory error warning, got %q", buf.String())
	}
}
