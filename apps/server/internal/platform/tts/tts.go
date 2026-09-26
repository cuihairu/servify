// Package tts 语音合成 provider 契约（Phase 2 语音实时翻译刀一，
// docs/realtime-translation-design.md §2/§3.1）：逐句文本进、音频字节出。
// 非流式整句合成是 Phase 2 口径（预算 P50 ≤ 800ms/句）；流式 chunked TTS
// 并入 Phase 3，届时以新方法扩展本契约而非改写。
package tts

import "context"

// SynthesizeRequest 单句合成请求。Voice 取 provider 自定命名（空 = provider
// 默认音色）；Format 取 "mp3"/"wav" 等（空 = provider 默认）。
type SynthesizeRequest struct {
	Text   string
	Lang   string
	Voice  string
	Format string
}

// SynthesizeResponse 合成结果：音频字节与实际格式。SampleRate 为 0 表示
// provider 未报告（消费方按 Format 自行解码兜底）。
type SynthesizeResponse struct {
	Audio      []byte
	Format     string
	SampleRate int
}

// Synthesizer 语音合成 provider。
type Synthesizer interface {
	Synthesize(ctx context.Context, req SynthesizeRequest) (SynthesizeResponse, error)
	HealthCheck(ctx context.Context) error
}
