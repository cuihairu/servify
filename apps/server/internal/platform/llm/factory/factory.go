// Package factory 按 config 选择 LLMProvider 实现，是全仓库唯一的
// provider 构造入口：provider 选型（ai.provider）全局一份，openai 与
// anthropic 的连接参数各自成族。此前 openai.NewProvider 在装配层散落
// 五处、anthropic 从未接线，切供应商要改遍调用点——收口到这里后，
// 新增 provider 只改本包 switch。
package factory

import (
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/llm"
	"servify/apps/server/internal/platform/llm/anthropic"
	localllm "servify/apps/server/internal/platform/llm/local"
	"servify/apps/server/internal/platform/llm/openai"
)

// Config 工厂入参。Provider 为空按 openai 兜底（与历史行为一致）；
// OpenAI 字段来自 configscope 作用域解析结果（租户/工作区可覆盖），
// Anthropic 仅全局配置。
type Config struct {
	Provider  string
	OpenAI    config.OpenAIConfig
	Anthropic config.AnthropicConfig
}

// New 返回选定 provider；未知选型返回错误（装配层据此启动失败，
// config 层 InsecureDefaults 已前置同类告警，两层合围见双层 gate 惯例）。
func New(cfg Config) (llm.LLMProvider, error) {
	switch kind := strings.ToLower(strings.TrimSpace(cfg.Provider)); kind {
	case "", "openai":
		return openai.NewProvider(cfg.OpenAI.APIKey, cfg.OpenAI.BaseURL), nil
	case "anthropic":
		return anthropic.NewProvider(cfg.Anthropic.APIKey, cfg.Anthropic.BaseURL), nil
	case "local":
		// 零依赖抽取式问答器：无网络请求、无 key 要求，与 embedding/local、
		// knowledge.provider=local 组成完全本地的离线链路。
		return localllm.NewProvider(), nil
	default:
		return nil, fmt.Errorf("llm factory: unknown provider %q (want openai, anthropic or local)", kind)
	}
}

// RuntimeParams 从同一份配置导出编排层模型参数：openai 取作用域解析
// 结果，anthropic 取全局段，local 无外部参数（模型名固定为抽取基线标识，
// 温度/上限/超时零值——provider 侧不消费）。零值字段由 provider 侧默认
// 兜底（空模型 → provider 默认模型，0 温度/0 max_tokens/0 超时 → 不下发
// 对应字段）。
func RuntimeParams(cfg Config) (Model string, Temperature float64, MaxTokens int, TimeoutMs int) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "anthropic":
		return cfg.Anthropic.Model, cfg.Anthropic.Temperature, cfg.Anthropic.MaxTokens,
			int(cfg.Anthropic.Timeout / time.Millisecond)
	case "local":
		return localllm.DefaultModel, 0, 0, 0
	default:
		return cfg.OpenAI.Model, cfg.OpenAI.Temperature, cfg.OpenAI.MaxTokens,
			int(cfg.OpenAI.Timeout / time.Millisecond)
	}
}
