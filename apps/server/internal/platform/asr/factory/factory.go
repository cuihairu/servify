// Package factory 按配置选择 asr.Recognizer 实现，与 platform/llm/factory
// 同款收口：provider 选型唯一构造入口，新增 provider 只改本包 switch。
// Phase 2 刀二起 openai（OpenAI 兼容流式转写 WS，§3.1 兼容口优先）进
// switch；mock 不进 switch，测试直构；Deepgram/火山/阿里按可用区后续刀。
package factory

import (
	"fmt"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/asr"
	asropenai "servify/apps/server/internal/platform/asr/openai"
)

// New 返回选定 provider：空选型返回 ErrNotConfigured（语音链路未启用的
// 合法形态，装配层据此跳过接线），未知选型返回错误。
func New(cfg config.ASRConfig) (asr.Recognizer, error) {
	switch kind := strings.ToLower(strings.TrimSpace(cfg.Provider)); kind {
	case "":
		return nil, asr.ErrNotConfigured
	case "openai":
		return asropenai.NewProvider(asropenai.Config{
			APIKey:   cfg.APIKey,
			BaseURL:  cfg.BaseURL,
			Model:    cfg.Model,
			Language: cfg.Language,
			Timeout:  cfg.Timeout,
		}), nil
	default:
		return nil, fmt.Errorf("asr factory: unknown provider %q", kind)
	}
}
