package config

import (
	"strings"
	"testing"
	"time"
)

// TestInsecureDefaults_AIProviderEnum ai.provider 非法值在配置层零容忍
// （production/staging 断启动），合法值（含大小写/空白变体）不产生告警；
// 装配层 factory.New 是第二道 gate。
func TestInsecureDefaults_AIProviderEnum(t *testing.T) {
	invalid := GetDefaultConfig()
	invalid.AI.Provider = "vertex"
	warnings := InsecureDefaults(invalid)
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, ";"), "ai.provider must be") {
		t.Fatalf("expected ai.provider warning, got %v", warnings)
	}

	for _, valid := range []string{"", "openai", "anthropic", " OpenAI ", "Anthropic"} {
		cfg := GetDefaultConfig()
		cfg.AI.Provider = valid
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "ai.provider") {
				t.Fatalf("provider %q should be accepted, got warning %q", valid, w)
			}
		}
	}
}

// TestDefaultConfigAnthropicFamily anthropic 配置族默认值与 provider 侧
// 兜底对齐（模型为当前主力系而非停更的 haiku），保证 provider=anthropic
// 时开箱即用且行为可预期。
func TestDefaultConfigAnthropicFamily(t *testing.T) {
	cfg := GetDefaultConfig()
	a := cfg.AI.Anthropic
	if a.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("base_url = %q", a.BaseURL)
	}
	if a.Model != DefaultAnthropicModel || a.Model == "claude-3-haiku-20240307" {
		t.Fatalf("model = %q, want modern default %q", a.Model, DefaultAnthropicModel)
	}
	if a.Temperature != 0.7 || a.MaxTokens != 1000 || a.Timeout != 30*time.Second {
		t.Fatalf("anthropic defaults = (temp=%v max=%d timeout=%v)", a.Temperature, a.MaxTokens, a.Timeout)
	}
}
