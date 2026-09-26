package application

import (
	"context"
	"strings"

	"servify/apps/server/internal/modules/translation/domain"
	"servify/apps/server/internal/platform/asr"
	"servify/apps/server/internal/platform/tts"
)

// VoiceCaption 一句语音的字幕产出（设计文档 §1.2 分发半边）。Seq 为管线
// 侧单调递增句序（从 1 起）；Degraded 表示翻译失败按 §3.2 预算熔断降级，
// Translated 即原文（客户端可按需标注"未翻译"）。
type VoiceCaption struct {
	Seq        int64
	Source     string
	Translated string
	SourceLang string
	TargetLang string
	Degraded   bool
}

// VoiceSink 管线产出接口：delivery 层适配为 WS 下行帧（partial →
// "正在说"字幕行；caption/audio → translation 帧族）或测试录制器。
// 全部方法要求非阻塞（管线串行调用，重活由实现方自行异步）。
type VoiceSink interface {
	// OnSpeechStart ASR 检测到说话开始（turnSeq 为 ASR 轮次序号）。
	OnSpeechStart(turnSeq int64)
	// OnPartial 中间假设（只驱动"正在说"，不进翻译——§2.2 稳定化规则）。
	OnPartial(turnSeq int64, text string)
	// OnCaption 一句完整产出（翻译成功或降级原文）。
	OnCaption(caption VoiceCaption)
	// OnAudio 译文合成的音频（§1.2 客户端本地播放形态；合成失败则该句仅字幕）。
	OnAudio(capSeq int64, audio []byte, format string)
	// OnError 非致命故障透出（逐句翻译/合成失败、ASR 会话破损）；管线不中断。
	OnError(err error)
}

// VoicePipeline 每说话方一条的语音翻译管线（设计文档 §1.2：识别与翻译在
// 服务端串联）：消费 ASR 事件流 → 分句（§2.2 双触发）→ 逐句翻译（携带
// 上一句原文+译文尾窗防割裂）→ TTS 逐句合成。双向翻译 = 每说话方各一条，
// 语言状态互不污染。
type VoicePipeline struct {
	translate  *Service        // nil = 翻译不可用：全部句子降级原文
	synth      tts.Synthesizer // nil = 仅字幕形态
	sink       VoiceSink
	asm        *domain.SentenceAssembler
	sourceLang string // 说话方语言；空 = auto
	targetLang string // 读向语言（字幕与 TTS 的目标语言）

	nextSeq     int64
	contextTail string // 上一句原文+译文尾窗（≤ MaxContextRunes，§2.2）
}

// NewVoicePipeline 构造管线。translate/synth 均可为 nil（对应部署形态的
// 降级面：仅原文字幕 / 仅字幕），sink 必填。
func NewVoicePipeline(translate *Service, synth tts.Synthesizer, sink VoiceSink, sourceLang, targetLang string) *VoicePipeline {
	return &VoicePipeline{
		translate:  translate,
		synth:      synth,
		sink:       sink,
		asm:        domain.NewSentenceAssembler(),
		sourceLang: sourceLang,
		targetLang: targetLang,
	}
}

// Run 消费 ASR 事件流直到通道关闭（正常收尾：残余 buffer Flush 后返回）
// 或 ctx 结束（会话拆除：立即返回）。事件处理串行——逐句预算熔断以
// 处理时延自然呈现，无需额外并发控制。
func (p *VoicePipeline) Run(ctx context.Context, events <-chan asr.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				p.flush(ctx)
				return
			}
			p.handle(ctx, ev)
		}
	}
}

func (p *VoicePipeline) handle(ctx context.Context, ev asr.Event) {
	switch ev.Kind {
	case asr.EventSpeechStart:
		p.sink.OnSpeechStart(ev.Seq)
	case asr.EventPartial:
		p.sink.OnPartial(ev.Seq, ev.Text)
	case asr.EventFinal:
		for _, sentence := range p.asm.Feed(ev.Seq, ev.Text) {
			p.processSentence(ctx, sentence)
		}
	case asr.EventSpeechEnd:
		for _, sentence := range p.asm.Flush() {
			p.processSentence(ctx, sentence)
		}
	case asr.EventError:
		p.sink.OnError(ev.Err)
	}
}

// flush ASR 会话结束的残余收尾（未触发 VAD 的尾部缓冲）。
func (p *VoicePipeline) flush(ctx context.Context) {
	for _, sentence := range p.asm.Flush() {
		p.processSentence(ctx, sentence)
	}
}

// processSentence 单句产出：翻译（失败降级原文）→ 字幕 → TTS（仅对翻译
// 成功句合成；降级句不为对方播报听不懂的原声）。
func (p *VoicePipeline) processSentence(ctx context.Context, sentence domain.Sentence) {
	p.nextSeq++
	caption := VoiceCaption{
		Seq:        p.nextSeq,
		Source:     sentence.Text,
		SourceLang: p.sourceLang,
		TargetLang: p.targetLang,
	}
	translated, err := p.translateSentence(ctx, sentence.Text)
	if err != nil {
		caption.Degraded = true
		caption.Translated = sentence.Text
		p.sink.OnCaption(caption)
		p.sink.OnError(err)
		return
	}
	caption.Translated = translated
	p.sink.OnCaption(caption)
	p.appendContextTail(sentence.Text, translated)

	if p.synth == nil {
		return // 仅字幕形态
	}
	resp, err := p.synth.Synthesize(ctx, tts.SynthesizeRequest{Text: caption.Translated, Lang: caption.TargetLang})
	if err != nil {
		// §3.2 预算熔断降级：该句仅字幕，管线继续。
		p.sink.OnError(err)
		return
	}
	p.sink.OnAudio(caption.Seq, resp.Audio, resp.Format)
}

func (p *VoicePipeline) translateSentence(ctx context.Context, text string) (string, error) {
	if p.translate == nil {
		return "", ErrTranslationUnavailable
	}
	res, err := p.translate.Translate(ctx, TranslateCommand{
		Text:       text,
		SourceLang: p.sourceLang,
		TargetLang: p.targetLang,
		Context:    p.contextTail,
	})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// appendContextTail 维护"上一句原文+译文"尾窗（§2.2）：服务端截到
// MaxContextRunes，控制时延与费用，也避免长会话内存无界增长。
func (p *VoicePipeline) appendContextTail(source, translated string) {
	tail := strings.TrimSpace(source) + "\n" + strings.TrimSpace(translated)
	if p.contextTail != "" {
		p.contextTail += "\n" + tail
	} else {
		p.contextTail = tail
	}
	runes := []rune(p.contextTail)
	if len(runes) > MaxContextRunes {
		p.contextTail = string(runes[len(runes)-MaxContextRunes:])
	}
}
