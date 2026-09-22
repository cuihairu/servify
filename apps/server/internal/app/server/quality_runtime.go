package server

import (
	"context"
	"strings"

	qualityapp "servify/apps/server/internal/modules/quality/application"
	qualityinfra "servify/apps/server/internal/modules/quality/infra"
	"servify/apps/server/internal/platform/configscope"
	llmfactory "servify/apps/server/internal/platform/llm/factory"
)

// buildQualityService 装配质检扫描服务；quality 未启用（或无 DB）返回 nil，
// worker 保持不注册。LLM 打分仅在 quality.llm.enabled 且所选 provider
// （ai.provider）的 APIKey 就绪时挂载，否则自动降级为 rules-only。
func (rt *Runtime) buildQualityService() *qualityapp.QualityService {
	cfg := rt.Config
	if !cfg.Quality.Enabled || rt.DB == nil {
		return nil
	}

	var scorer qualityapp.ScoreProvider
	if cfg.Quality.LLM.Enabled {
		openAIConfig := configscope.NewResolver(cfg).ResolveOpenAI(context.Background(), nil)
		factoryCfg := llmfactory.Config{
			Provider:  cfg.AI.Provider,
			OpenAI:    openAIConfig,
			Anthropic: cfg.AI.Anthropic,
		}
		// 就绪判定跟随 provider 选型：openai 看作用域解析后的 api_key，
		// anthropic 看全局段 api_key。
		ready := openAIConfig.APIKey != ""
		if strings.EqualFold(strings.TrimSpace(cfg.AI.Provider), "anthropic") {
			ready = cfg.AI.Anthropic.APIKey != ""
		}
		if ready {
			provider, err := llmfactory.New(factoryCfg)
			if err != nil {
				if rt.Logger != nil {
					rt.Logger.Warnf("quality: build llm provider: %v; falling back to rules-only scoring", err)
				}
				provider = nil
			}
			if provider != nil {
				// 打分模型参数沿用 quality.llm.*（与客服首答模型解耦；
				// provider 切换时模型名需是所选 provider 的合法模型）。
				scorer = qualityinfra.NewLLMScoreProvider(
					provider,
					qualityinfra.LLMRuntimeConfig{
						Model:          cfg.Quality.LLM.Model, // 空则走 provider 默认模型
						Temperature:    cfg.Quality.LLM.Temperature,
						MaxInputChars:  cfg.Quality.LLM.MaxInputChars,
						MaxTurnChars:   cfg.Quality.LLM.MaxTurnChars,
						TimeoutSeconds: cfg.Quality.LLM.TimeoutSeconds,
					},
				)
			}
		} else if rt.Logger != nil {
			rt.Logger.Warn("quality: llm.enabled but provider api_key is empty; falling back to rules-only scoring")
		}
	}

	return qualityapp.NewQualityService(
		qualityinfra.NewGormRepository(rt.DB),
		scorer,
		qualityapp.ServiceConfig{
			SampleRate:          cfg.Quality.SampleRate,
			BatchSize:           cfg.Quality.BatchSize,
			LookbackDays:        cfg.Quality.LookbackDays,
			MinMessages:         cfg.Quality.MinMessages,
			MaxAttempts:         cfg.Quality.MaxAttempts,
			RetryBackoffSeconds: cfg.Quality.RetryBackoffSeconds,
			LLMEnabled:          cfg.Quality.LLM.Enabled,
			Rules: qualityapp.RuleConfig{
				BannedWords:                 cfg.Quality.Rules.BannedWords,
				ResponseTimeoutSeconds:      cfg.Quality.Rules.ResponseTimeoutSeconds,
				FirstResponseTimeoutSeconds: cfg.Quality.Rules.FirstResponseTimeoutSeconds,
			},
		},
		rt.Logger,
	)
}
