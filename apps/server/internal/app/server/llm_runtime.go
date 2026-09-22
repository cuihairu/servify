package server

import (
	"context"
	"fmt"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/llm"
	llmfactory "servify/apps/server/internal/platform/llm/factory"
)

// resolveLLMRuntime 按全局 ai.provider 构造 LLMProvider 并导出编排层模型
// 参数——装配层唯一的 provider 构造入口（此前 openai.NewProvider 在本包
// 散落多处、anthropic 从未接线）。openai 参数走 configscope 作用域解析
// （租户/工作区可覆盖 key/base_url/model 等）；anthropic 仅全局配置段。
// 未知 provider 返回错误：config 层 InsecureDefaults 已前置同类告警，
// 这里是装配层兜底 gate（production/staging 在加载期就会先被拦下）。
func resolveLLMRuntime(cfg *config.Config, resolver *configscope.Resolver) (llm.LLMProvider, aidelivery.AIRuntimeParams, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	res := resolver
	if res == nil {
		res = configscope.NewResolver(cfg)
	}
	openAICfg := res.ResolveOpenAI(context.Background(), nil)
	factoryCfg := llmfactory.Config{
		Provider:  cfg.AI.Provider,
		OpenAI:    openAICfg,
		Anthropic: cfg.AI.Anthropic,
	}
	provider, err := llmfactory.New(factoryCfg)
	if err != nil {
		return nil, aidelivery.AIRuntimeParams{}, fmt.Errorf("build llm provider: %w", err)
	}
	model, temperature, maxTokens, timeoutMs := llmfactory.RuntimeParams(factoryCfg)
	return provider, aidelivery.AIRuntimeParams{
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		TimeoutMs:   timeoutMs,
	}, nil
}
