package delivery

import (
	"context"
	"strings"

	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/asr"
	"servify/apps/server/internal/platform/llm"
	"servify/apps/server/internal/platform/tts"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// voiceChannelAdapter 语音翻译通道适配器：每条上行流组装一次"ASR 会话 +
// 语音管线"。翻译门面与偏好读取与 message-translated 路径同源（同一
// HandlerService 与 PreferenceService），仅读向按说话方对偶解析——visitor
// 说话消费 agent 读向（坐席读什么语言），agent 说话消费 visitor 读向。
type voiceChannelAdapter struct {
	translator TranslateInvoker
	prefs      SessionPreferenceReader
	recognizer asr.Recognizer
	synth      tts.Synthesizer // nil = 仅字幕形态（TTS 未配置的合法降级）
	obs        translationapp.VoiceObserver
}

// NewVoiceChannelService 创建语音翻译通道服务。recognizer 为 nil 视作
// 未配置（ErrTranslationUnavailable）；synth 可为 nil（仅字幕）；
// metrics 可为 nil（不计量）。
func NewVoiceChannelService(translator TranslateInvoker, prefs SessionPreferenceReader, recognizer asr.Recognizer, synth tts.Synthesizer, metrics *svcmetrics.BusinessMetrics) VoiceStreamStarter {
	var obs translationapp.VoiceObserver
	if metrics != nil {
		obs = &voiceMetricsObserver{metrics: metrics}
	}
	return &voiceChannelAdapter{translator: translator, prefs: prefs, recognizer: recognizer, synth: synth, obs: obs}
}

// voiceMetricsObserver 管线逐句产出 → 业务计量桥（设计文档 §3.2 会话级
// 计量落 rt.BusinessMetrics 既有口；token 计量复用 ai_llm_tokens 的
// input/output 口径）。
type voiceMetricsObserver struct {
	metrics *svcmetrics.BusinessMetrics
}

func (o *voiceMetricsObserver) OnSentenceOutcome(outcome string) {
	o.metrics.RecordVoiceTranslationSentence(outcome)
}

func (o *voiceMetricsObserver) OnTranslateTokens(provider string, usage llm.TokenUsage) {
	o.metrics.RecordAILLMTokens(provider, "input", usage.InputTokens)
	o.metrics.RecordAILLMTokens(provider, "output", usage.OutputTokens)
}

// StartAudioStream 见 VoiceStreamStarter 契约。管线随流启动，Close 收尾
// （cancel 管线 ctx + 关 ASR 会话）；事件通道随 readLoop 退出关闭，管线
// 自然返回。
func (a *voiceChannelAdapter) StartAudioStream(ctx context.Context, sessionID, speaker string, sink VoiceStreamSink) (VoiceAudioStream, error) {
	if a == nil || a.recognizer == nil || a.prefs == nil {
		return nil, ErrTranslationUnavailable
	}
	viewer := ViewerRoleAgent
	if speaker == VoiceSpeakerAgent {
		viewer = ViewerRoleVisitor
	}
	targetLang, err := a.prefs.GetSessionLanguage(ctx, sessionID, viewer)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetLang) == "" {
		return nil, ErrVoiceChannelInactive
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	session, events, err := a.recognizer.NewSession(streamCtx, asr.SessionOptions{
		SourceLang: "", // 说话方语言未知，模型自动检测
		Format:     asr.AudioFormat{SampleRate: 24000, Channels: 1, Encoding: "pcm16"},
	})
	if err != nil {
		cancel()
		return nil, err
	}
	pipeline := translationapp.NewVoicePipeline(a.translator, a.synth, sink, "", targetLang, a.obs)
	go pipeline.Run(streamCtx, events)
	return &voiceAudioStream{session: session, cancel: cancel}, nil
}

// voiceAudioStream VoiceAudioStream 的 ASR 会话包装。
type voiceAudioStream struct {
	session asr.RecognizeSession
	cancel  context.CancelFunc
}

func (s *voiceAudioStream) Feed(chunk []byte) error {
	return s.session.FeedAudio(chunk)
}

func (s *voiceAudioStream) Close() error {
	s.cancel() // 先停管线消费循环，再收 ASR 会话（读侧退出关事件通道）
	return s.session.Close()
}
