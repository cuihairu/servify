// Package asr 语音识别 provider 契约（Phase 2 语音实时翻译刀一，
// docs/realtime-translation-design.md §2/§3.1）：流式音频进、识别事件出。
// 识别在服务端串联（客户端不做 ASR）；partial 只驱动"正在说"反馈，
// final 才进翻译队列（§2.2 分句策略）。
package asr

import "context"

// EventKind 识别事件分类。VAD（语音端点检测）事件是分句主触发的来源：
// 静音 ≥ 500ms 的尾点由 provider 以 EventSpeechEnd 透出（§2.2 双触发策略
// 之一），边界触发（标点/长度切句）由上层翻译管线在 EventFinal 文本上做。
type EventKind string

const (
	// EventSpeechStart VAD 起点：说话开始。
	EventSpeechStart EventKind = "speech_start"
	// EventPartial 中间结果：即到即弃，只允许驱动"正在说"行，不进翻译。
	EventPartial EventKind = "partial"
	// EventFinal 句终结果：进翻译队列的唯一事件种类。同句二次 final
	//（provider 稳定化重发）按 Seq 去重覆盖。
	EventFinal EventKind = "final"
	// EventSpeechEnd VAD 尾点：说话结束（静音切句主触发）。
	EventSpeechEnd EventKind = "speech_end"
)

// Event 单条识别事件。Seq 是 provider 侧单调句序号，partial 与 final 共用
// 同一句的序号（同一句的 partial 串与 final 同 Seq）。
type Event struct {
	Kind       EventKind
	Seq        int64
	Text       string
	Confidence float64
}

// AudioFormat 上行音频描述。编码取值 provider 自定（如 "opus"/"pcm_s16le"），
// 契约不设枚举——不支持的组合由 provider 在 NewSession 报错。
type AudioFormat struct {
	SampleRate int
	Channels   int
	Encoding   string
}

// SessionOptions 会话参数：源语言（BCP-47 风格或 "auto"）与音频格式。
type SessionOptions struct {
	SourceLang string
	Format     AudioFormat
}

// RecognizeSession 单条语音流会话：FeedAudio 持续喂音频分片，识别事件从
// NewSession 返回的事件通道异步流出。Close 冲刷收尾——provider 应先排干
// 余量事件再关闭通道；关闭后 FeedAudio 返回 ErrSessionClosed。
type RecognizeSession interface {
	FeedAudio(chunk []byte) error
	Close() error
}

// Recognizer 语音识别 provider。事件通道由 provider 创建并在会话结束后
// 关闭；消费方 range 到通道关闭即为会话终止。
type Recognizer interface {
	NewSession(ctx context.Context, opts SessionOptions) (RecognizeSession, <-chan Event, error)
	HealthCheck(ctx context.Context) error
}
