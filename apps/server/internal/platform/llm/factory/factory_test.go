package factory

import (
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/llm"
	"servify/apps/server/internal/platform/llm/anthropic"
	"servify/apps/server/internal/platform/llm/openai"
)

// TestNewProviderSelection 全选型矩阵：空值/大小写归一到 openai，
// anthropic 返回对应实现，未知选型报错（装配层据此启动失败）。
func TestNewProviderSelection(t *testing.T) {
	cases := []struct {
		provider string
		want     llm.LLMProvider
		wantErr  string
	}{
		{provider: "", want: &openai.Provider{}},
		{provider: "openai", want: &openai.Provider{}},
		{provider: " OpenAI ", want: &openai.Provider{}},
		{provider: "anthropic", want: &anthropic.Provider{}},
		{provider: "Anthropic", want: &anthropic.Provider{}},
		{provider: "vertex", wantErr: `unknown provider "vertex"`},
	}
	for _, tc := range cases {
		got, err := New(Config{Provider: tc.provider})
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("New(%q) error = %v, want contains %q", tc.provider, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("New(%q) error = %v", tc.provider, err)
		}
		// 类型断言比较：接口值直接 != 会因包装差异误判，按具体类型核对。
		switch tc.want.(type) {
		case *openai.Provider:
			if _, ok := got.(*openai.Provider); !ok {
				t.Fatalf("New(%q) = %T, want *openai.Provider", tc.provider, got)
			}
		case *anthropic.Provider:
			if _, ok := got.(*anthropic.Provider); !ok {
				t.Fatalf("New(%q) = %T, want *anthropic.Provider", tc.provider, got)
			}
		}
	}
}

// TestRuntimeParamsBranches 参数导出按选型走对应配置族：openai 族支持
// 作用域覆盖（调用方传入解析结果），anthropic 仅全局段；timeout 换算
// 为毫秒，零值原样透传（provider 侧默认兜底）。
func TestRuntimeParamsBranches(t *testing.T) {
	cases := []struct {
		name            string
		cfg             Config
		wantModel       string
		wantTemperature float64
		wantMaxTokens   int
		wantTimeoutMs   int
	}{
		{
			name: "openai family",
			cfg: Config{OpenAI: config.OpenAIConfig{
				Model: "m-openai", Temperature: 0.2, MaxTokens: 500, Timeout: 5 * time.Second,
			}},
			wantModel: "m-openai", wantTemperature: 0.2, wantMaxTokens: 500, wantTimeoutMs: 5000,
		},
		{
			name: "anthropic family",
			cfg: Config{
				Provider: "anthropic",
				Anthropic: config.AnthropicConfig{
					Model: "m-anthropic", Temperature: 0.9, MaxTokens: 800, Timeout: 12 * time.Second,
				},
			},
			wantModel: "m-anthropic", wantTemperature: 0.9, wantMaxTokens: 800, wantTimeoutMs: 12000,
		},
		{
			name: "zero values pass through",
			cfg:  Config{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, temperature, maxTokens, timeoutMs := RuntimeParams(tc.cfg)
			if model != tc.wantModel || temperature != tc.wantTemperature ||
				maxTokens != tc.wantMaxTokens || timeoutMs != tc.wantTimeoutMs {
				t.Fatalf("RuntimeParams = (%q, %v, %d, %d), want (%q, %v, %d, %d)",
					model, temperature, maxTokens, timeoutMs,
					tc.wantModel, tc.wantTemperature, tc.wantMaxTokens, tc.wantTimeoutMs)
			}
		})
	}
}
