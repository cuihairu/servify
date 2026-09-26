// Package factory 按配置选择 tts.Synthesizer 实现，与 platform/llm/factory
// 同款收口：provider 选型唯一构造入口，新增 provider 只改本包 switch。
// Phase 2 刀一只有契约与 mock（mock 不进 switch，测试直构——llm 同例）；
// OpenAI 兼容 TTS 口（tts-1 档，§3.1 选型）随语音管线刀接入。
package factory

import (
	"fmt"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/tts"
)

// New 返回选定 provider：空选型返回 ErrNotConfigured（语音链路未启用的
// 合法形态，装配层据此跳过接线），未知选型返回错误。
func New(cfg config.TTSConfig) (tts.Synthesizer, error) {
	switch kind := strings.ToLower(strings.TrimSpace(cfg.Provider)); kind {
	case "":
		return nil, tts.ErrNotConfigured
	default:
		return nil, fmt.Errorf("tts factory: unknown provider %q", kind)
	}
}
