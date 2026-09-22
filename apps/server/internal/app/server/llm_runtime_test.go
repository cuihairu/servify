package server

import (
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/llm"
	"servify/apps/server/internal/platform/llm/anthropic"
	"servify/apps/server/internal/platform/llm/openai"
)

// TestResolveLLMRuntimeProviderSelection 装配层 provider 选型三层：默认
// openai（历史行为）、anthropic 走全局段参数、未知选型报错（双层 gate
// 的装配层兜底，config 层告警前置）。
func TestResolveLLMRuntimeProviderSelection(t *testing.T) {
	t.Run("default openai with default params", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		provider, params, err := resolveLLMRuntime(cfg, nil)
		if err != nil {
			t.Fatalf("resolveLLMRuntime() error = %v", err)
		}
		if _, ok := provider.(*openai.Provider); !ok {
			t.Fatalf("provider = %T, want *openai.Provider", provider)
		}
		if params.Model != config.DefaultOpenAIModel || params.Temperature != 0.7 ||
			params.MaxTokens != 1000 || params.TimeoutMs != 30000 {
			t.Fatalf("params = %+v, want openai defaults", params)
		}
	})

	t.Run("anthropic uses global family", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.AI.Provider = "anthropic"
		cfg.AI.Anthropic.APIKey = "ak-test"
		provider, params, err := resolveLLMRuntime(cfg, nil)
		if err != nil {
			t.Fatalf("resolveLLMRuntime() error = %v", err)
		}
		if _, ok := provider.(*anthropic.Provider); !ok {
			t.Fatalf("provider = %T, want *anthropic.Provider", provider)
		}
		if params.Model != config.DefaultAnthropicModel || params.Temperature != 0.7 ||
			params.MaxTokens != 1000 || params.TimeoutMs != 30000 {
			t.Fatalf("params = %+v, want anthropic defaults", params)
		}
	})

	t.Run("unknown provider rejected", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.AI.Provider = "bogus"
		_, _, err := resolveLLMRuntime(cfg, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown provider") {
			t.Fatalf("error = %v, want unknown provider rejection", err)
		}
	})
}

// TestResolveLLMRuntimeNilSafety nil cfg / nil resolver 走零值兜底而不 panic：
// 测试与降级路径会以残缺入参调用装配助手。
func TestResolveLLMRuntimeNilSafety(t *testing.T) {
	provider, params, err := resolveLLMRuntime(nil, nil)
	if err != nil {
		t.Fatalf("resolveLLMRuntime(nil, nil) error = %v", err)
	}
	if _, ok := provider.(llm.LLMProvider); !ok || provider == nil {
		t.Fatalf("provider = %v, want non-nil", provider)
	}
	if params.Model != "" {
		t.Fatalf("nil config must export zero params, got %+v", params)
	}
}
