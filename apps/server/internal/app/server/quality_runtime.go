package server

import (
	"context"

	qualityapp "servify/apps/server/internal/modules/quality/application"
	qualityinfra "servify/apps/server/internal/modules/quality/infra"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/llm/openai"
)

// buildQualityService 装配质检扫描服务；quality 未启用（或无 DB）返回 nil，
// worker 保持不注册。LLM 打分仅在 quality.llm.enabled 且 OpenAI APIKey
// 就绪时挂载，否则自动降级为 rules-only。
func (rt *Runtime) buildQualityService() *qualityapp.QualityService {
	cfg := rt.Config
	if !cfg.Quality.Enabled || rt.DB == nil {
		return nil
	}

	var scorer qualityapp.ScoreProvider
	if cfg.Quality.LLM.Enabled {
		openAIConfig := configscope.NewResolver(cfg).ResolveOpenAI(context.Background(), nil)
		if openAIConfig.APIKey != "" {
			scorer = qualityinfra.NewLLMScoreProvider(
				openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL),
				qualityinfra.LLMRuntimeConfig{
					Model:          cfg.Quality.LLM.Model, // 空则走 provider 默认模型
					Temperature:    cfg.Quality.LLM.Temperature,
					MaxInputChars:  cfg.Quality.LLM.MaxInputChars,
					MaxTurnChars:   cfg.Quality.LLM.MaxTurnChars,
					TimeoutSeconds: cfg.Quality.LLM.TimeoutSeconds,
				},
			)
		} else if rt.Logger != nil {
			rt.Logger.Warn("quality: llm.enabled but ai.openai.api_key is empty; falling back to rules-only scoring")
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
