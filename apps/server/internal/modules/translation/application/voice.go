package application

import (
	"context"
	"strings"
	"time"

	"servify/apps/server/internal/modules/translation/domain"
	"servify/apps/server/internal/platform/asr"
	"servify/apps/server/internal/platform/llm"
	"servify/apps/server/internal/platform/tts"
)

// 逐句预算熔断上限（设计文档 §2.1/§3.2）：P50 预算 LLM ≤600ms、TTS ≤800ms，
// 熔断给足裕量但远小于 provider 传输层 30s 兜底——挂死的上游把该句降级为
// 仅字幕/仅原文，而不是把串行管线拖住 30s（后续句全部滞后）。
const (
	sentenceTranslateBudget = 5 * time.Second
	sentenceSynthBudget     = 5 * time.Second
)

// 产出词表（VoiceObserver.OnSentenceOutcome 的 outcome 值，同时是
// voice_translation_sentences_total 的 outcome 标签值）。
const (
	VoiceOutcomeTranslated  = "translated"   // 字幕 + 音频
	VoiceOutcomeCaptionOnly = "caption_only" // 译文仅字幕（synth 未配置/失败/超时）
	VoiceOutcomeDegraded    = "degraded"     // 降级原文字幕（翻译失败/超时）
)

// VoiceObserver 逐句产出观测面（§3.2 成本计量落 rt.BusinessMetrics 既有
// 口；装配层桥接，nil 安全）。管线串行调用，实现须非阻塞。
type VoiceObserver interface {
	// OnSentenceOutcome 每句产出一次（三种 outcome 见词表）。
	OnSentenceOutcome(outcome string)
	// OnTranslateTokens 逐句翻译的 token 消费；provider 未回传 usage 则不调。
	OnTranslateTokens(provider string, usage llm.TokenUsage)
}

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

// SentenceTranslator 逐句翻译出站面（*Service 结构化满足；delivery 适配器
// 传 HandlerService 门面，测试可注入替身）。
type SentenceTranslator interface {
	Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error)
}

// VoicePipeline 每说话方一条的语音翻译管线（设计文档 §1.2：识别与翻译在
// 服务端串联）：消费 ASR 事件流 → 分句（§2.2 双触发）→ 逐句翻译（携带
// 上一句原文+译文尾窗防割裂）→ TTS 逐句合成。双向翻译 = 每说话方各一条，
// 语言状态互不污染。
type VoicePipeline struct {
	translate       SentenceTranslator // nil = 翻译不可用：全部句子降级原文
	synth           tts.Synthesizer    // nil = 仅字幕形态
	sink            VoiceSink
	obs             VoiceObserver // nil = 不计量
	asm             *domain.SentenceAssembler
	sourceLang      string // 说话方语言；空 = auto
	targetLang      string // 读向语言（字幕与 TTS 的目标语言）
	translateBudget time.Duration
	synthBudget     time.Duration

	nextSeq     int64
	contextTail string // 上一句原文+译文尾窗（≤ MaxContextRunes，§2.2）
}

// NewVoicePipeline 构造管线。translate/synth 均可为 nil（对应部署形态的
// 降级面：仅原文字幕 / 仅字幕），sink 必填；obs 可为 nil（不计量）。
func NewVoicePipeline(translate SentenceTranslator, synth tts.Synthesizer, sink VoiceSink, sourceLang, targetLang string, obs VoiceObserver) *VoicePipeline {
	return &VoicePipeline{
		translate:       translate,
		synth:           synth,
		sink:            sink,
		obs:             obs,
		asm:             domain.NewSentenceAssembler(),
		sourceLang:      sourceLang,
		targetLang:      targetLang,
		translateBudget: sentenceTranslateBudget,
		synthBudget:     sentenceSynthBudget,
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

// processSentence 单句产出：翻译（失败/超预算降级原文）→ 字幕 → TTS（仅对
// 翻译成功句合成；降级句不为对方播报听不懂的原声）。会话拆除中的在途句
// （外层 ctx 已取消）不产出字幕也不计降级——那是收线，不是熔断。
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
		if ctx.Err() != nil {
			return
		}
		caption.Degraded = true
		caption.Translated = sentence.Text
		p.sink.OnCaption(caption)
		p.sink.OnError(err)
		p.observe(VoiceOutcomeDegraded)
		return
	}
	caption.Translated = translated
	p.sink.OnCaption(caption)
	p.appendContextTail(sentence.Text, translated)

	if p.synth == nil {
		p.observe(VoiceOutcomeCaptionOnly)
		return // 仅字幕形态
	}
	sctx, cancel := context.WithTimeout(ctx, p.synthBudget)
	defer cancel()
	resp, err := p.synth.Synthesize(sctx, tts.SynthesizeRequest{Text: caption.Translated, Lang: caption.TargetLang})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		// §3.2 预算熔断降级：该句仅字幕，管线继续。
		p.sink.OnError(err)
		p.observe(VoiceOutcomeCaptionOnly)
		return
	}
	p.sink.OnAudio(caption.Seq, resp.Audio, resp.Format)
	p.observe(VoiceOutcomeTranslated)
}

func (p *VoicePipeline) translateSentence(ctx context.Context, text string) (string, error) {
	if p.translate == nil {
		return "", ErrTranslationUnavailable
	}
	tctx, cancel := context.WithTimeout(ctx, p.translateBudget)
	defer cancel()
	res, err := p.translate.Translate(tctx, TranslateCommand{
		Text:       text,
		SourceLang: p.sourceLang,
		TargetLang: p.targetLang,
		Context:    p.contextTail,
	})
	if err != nil {
		return "", err
	}
	if p.obs != nil && res.TokenUsage != nil && res.Provider != "" {
		p.obs.OnTranslateTokens(res.Provider, *res.TokenUsage)
	}
	return res.Text, nil
}

// observe 单点计量出口（obs nil 安全）。
func (p *VoicePipeline) observe(outcome string) {
	if p.obs != nil {
		p.obs.OnSentenceOutcome(outcome)
	}
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
