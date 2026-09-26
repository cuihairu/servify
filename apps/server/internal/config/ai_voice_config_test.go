package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

// TestLoad_VoiceProviderFaces 语音链路（Phase 2 实时翻译刀一）配置面：
// ai.asr / ai.tts 键经 viper 反序列化进结构体（provider 空 = 未启用是合法
// 形态，装配层据此跳过接线），env 占位符与全仓键风格一致。
func TestLoad_VoiceProviderFaces(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("ai.asr.provider", "deepgram")
	viper.Set("ai.asr.api_key", "${ASR_API_KEY}")
	viper.Set("ai.asr.language", "auto")
	viper.Set("ai.asr.timeout", "5s")
	viper.Set("ai.tts.provider", "openai")
	viper.Set("ai.tts.voice", "alloy")
	viper.Set("ai.tts.format", "mp3")
	viper.Set("ai.tts.timeout", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AI.ASR.Provider != "deepgram" || cfg.AI.ASR.Language != "auto" {
		t.Fatalf("asr face degraded: %+v", cfg.AI.ASR)
	}
	if cfg.AI.ASR.Timeout != 5*time.Second {
		t.Fatalf("asr timeout = %v, want 5s", cfg.AI.ASR.Timeout)
	}
	if cfg.AI.TTS.Provider != "openai" || cfg.AI.TTS.Voice != "alloy" || cfg.AI.TTS.Format != "mp3" {
		t.Fatalf("tts face degraded: %+v", cfg.AI.TTS)
	}
	if cfg.AI.TTS.Timeout != 3*time.Second {
		t.Fatalf("tts timeout = %v, want 3s", cfg.AI.TTS.Timeout)
	}
}

// TestDefaultConfig_VoiceProvidersOff 缺省配置语音链路关闭：不设 provider
// 时 factory 返回 ErrNotConfigured、装配层跳过接线——语音面不改变既有部署
// 的启动行为。
func TestDefaultConfig_VoiceProvidersOff(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.AI.ASR.Provider != "" {
		t.Fatalf("default asr provider = %q, want empty (off)", cfg.AI.ASR.Provider)
	}
	if cfg.AI.TTS.Provider != "" {
		t.Fatalf("default tts provider = %q, want empty (off)", cfg.AI.TTS.Provider)
	}
}
