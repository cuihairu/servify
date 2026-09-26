// Package openai asr.Recognizer 的 OpenAI 兼容流式转写实现（Phase 2 刀二，
// docs/realtime-translation-design.md §2/§3.1）：OpenAI 实时转写 WS 协议
// （/v1/realtime?intent=transcription），base_url 可指向实现了同协议的兼容
// 网关。事件映射：speech_started/speech_stopped → VAD 事件，transcription
// delta → partial，completed → final，error → EventError（会话就此破损）。
// 音频只收 pcm16 24kHz 单声道（协议要求，重采样是调用方责任）。
package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"servify/apps/server/internal/platform/asr"

	"github.com/gorilla/websocket"
)

const (
	defaultBaseURL      = "https://api.openai.com/v1"
	defaultModel        = "gpt-4o-mini-transcribe"
	defaultTimeout      = 30 * time.Second
	requiredSampleRate  = 24000
	requiredChannels    = 1
	eventsChannelBuffer = 256
)

// Config provider 接入参数（factory 从 config.ASRConfig 平移）。
type Config struct {
	APIKey   string
	BaseURL  string
	Model    string
	Language string
	Timeout  time.Duration
}

// —— 出站帧（客户端 → 服务端）——

type sessionUpdateMessage struct {
	Type    string              `json:"type"`
	Session sessionUpdateParams `json:"session"`
}

type sessionUpdateParams struct {
	InputAudioFormat         string                     `json:"input_audio_format"`
	InputAudioTranscription  inputAudioTranscriptionCfg `json:"input_audio_transcription"`
	TurnDetection            *turnDetectionCfg          `json:"turn_detection"`
	InputAudioNoiseReduction *noiseReductionCfg         `json:"input_audio_noise_reduction,omitempty"`
}

type inputAudioTranscriptionCfg struct {
	Model    string `json:"model"`
	Language string `json:"language,omitempty"`
}

type turnDetectionCfg struct {
	Type              string  `json:"type"`
	SilenceDurationMs int     `json:"silence_duration_ms,omitempty"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms,omitempty"`
	Threshold         float64 `json:"threshold,omitempty"`
}

type noiseReductionCfg struct {
	Type string `json:"type"`
}

type appendAudioMessage struct {
	Type  string `json:"type"`
	Audio string `json:"audio"`
}

// —— 入站帧（服务端 → 客户端，仅声明消费的字段）——

type serverEvent struct {
	Type string `json:"type"`

	// conversation.item.input_audio_transcription.delta / .completed
	Delta      string `json:"delta"`
	Transcript string `json:"transcript"`

	// error
	Error *serverErrorBody `json:"error,omitempty"`
}

type serverErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Provider OpenAI 兼容流式转写实现。
type Provider struct {
	apiKey   string
	wsURL    string
	model    string
	language string
	timeout  time.Duration
	restBase string

	// dial 出站建联（包内测试注入点：替换拨号来源以确定性触发握手写失败
	// 等分支）。生产路径恒为 dialReal。
	dial func(ctx context.Context, urlStr string, hdr http.Header) (*websocket.Conn, error)
}

// NewProvider 构造 provider：ws URL 由 base_url 推导（https→wss、http→ws，
// 兼容 self-host 网关；base_url 兼容网关需实现 /v1/realtime 转写协议与
// /v1/models 探活）。
func NewProvider(cfg Config) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	wsURL := strings.Replace(baseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	p := &Provider{
		apiKey:   cfg.APIKey,
		wsURL:    wsURL,
		model:    model,
		language: strings.TrimSpace(cfg.Language),
		timeout:  timeout,
		restBase: baseURL,
	}
	p.dial = p.dialReal
	return p
}

// dialReal 真实出站建联。
func (p *Provider) dialReal(ctx context.Context, urlStr string, hdr http.Header) (*websocket.Conn, error) {
	dialer := websocket.Dialer{HandshakeTimeout: p.timeout}
	conn, _, err := dialer.DialContext(ctx, urlStr, hdr)
	return conn, err
}

// NewSession 建立流式转写会话：校验音频面 → 拨 WS → 下发 session.update。
// 建联与握手失败在此同步返回；此后读侧失败经 EventError 透出（会话破损）。
func (p *Provider) NewSession(ctx context.Context, opts asr.SessionOptions) (asr.RecognizeSession, <-chan asr.Event, error) {
	if err := validateFormat(opts.Format); err != nil {
		return nil, nil, err
	}
	hdr := http.Header{}
	if p.apiKey != "" {
		hdr.Set("Authorization", "Bearer "+p.apiKey)
		hdr.Set("OpenAI-Beta", "realtime=v1")
	}
	conn, err := p.dial(ctx, p.wsURL+"?intent=transcription", hdr)
	if err != nil {
		return nil, nil, fmt.Errorf("asr openai: dial transcription stream: %w", err)
	}
	update := sessionUpdateMessage{
		Type: "transcription_session.update",
		Session: sessionUpdateParams{
			InputAudioFormat: "pcm16",
			InputAudioTranscription: inputAudioTranscriptionCfg{
				Model:    p.model,
				Language: p.language,
			},
			// 服务端 VAD（§2.2 静音切句主触发）：500ms 尾点静音即 speech_stopped。
			TurnDetection: &turnDetectionCfg{
				Type:              "server_vad",
				SilenceDurationMs: 500,
				PrefixPaddingMs:   300,
			},
		},
	}
	conn.SetWriteDeadline(time.Now().Add(p.timeout))
	if err := conn.WriteJSON(update); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("asr openai: send session update: %w", err)
	}

	s := &session{
		conn:    conn,
		timeout: p.timeout,
		events:  make(chan asr.Event, eventsChannelBuffer),
		done:    make(chan struct{}),
	}
	go s.readLoop()
	return s, s.events, nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.restBase+"/models", nil)
	if err != nil {
		return err
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := (&http.Client{Timeout: p.timeout}).Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("asr openai: health status %d", resp.StatusCode)
	}
	return nil
}

// validateFormat openai 流式转写只收 pcm16 24kHz 单声道。
func validateFormat(f asr.AudioFormat) error {
	encoding := strings.ToLower(strings.TrimSpace(f.Encoding))
	if encoding != "pcm16" && encoding != "pcm_s16le" {
		return fmt.Errorf("%w: encoding %q (openai stream accepts pcm16/pcm_s16le)", asr.ErrUnsupportedFormat, f.Encoding)
	}
	if f.SampleRate != requiredSampleRate {
		return fmt.Errorf("%w: sample rate %d (openai stream requires %dHz)", asr.ErrUnsupportedFormat, f.SampleRate, requiredSampleRate)
	}
	if f.Channels != requiredChannels {
		return fmt.Errorf("%w: channels %d (openai stream requires mono)", asr.ErrUnsupportedFormat, f.Channels)
	}
	return nil
}

// session 单会话：写侧仅 FeedAudio 使用（gorilla 单写者约束，writeMu 串行），
// 读侧由 readLoop 独占；事件通道由 readLoop 退出时关闭。
type session struct {
	conn    *websocket.Conn
	timeout time.Duration
	events  chan asr.Event
	done    chan struct{}

	writeMu    sync.Mutex
	turnSeq    int64 // 仅 readLoop 触碰，无锁
	brokenMu   sync.Mutex
	broken     bool
	doneMu     sync.Mutex
	doneClosed bool

	// emitMu/eventsClosed：事件通道的发送与关闭同锁串行——emit 与 readLoop
	// 退出时的 defer close 竞态会 send on closed channel（进程级 panic，CI
	// TestEmitAbandonsWhenDoneClosedAndBufferFull 实锤）。done 分支保证消费方
	// 放弃后 emit 不阻塞；持锁阻塞在满缓冲上的 emit 由 done 关闭解围，不会
	// 死锁收线路径。
	emitMu       sync.Mutex
	eventsClosed bool
}

func (s *session) FeedAudio(chunk []byte) error {
	s.brokenMu.Lock()
	broken := s.broken
	s.brokenMu.Unlock()
	if broken {
		return asr.ErrSessionClosed
	}
	if len(chunk) == 0 {
		return nil // 空分片无信息量，静默吞掉（采集端静默期常见）
	}
	msg := appendAudioMessage{
		Type:  "input_audio_buffer.append",
		Audio: base64.StdEncoding.EncodeToString(chunk),
	}
	s.writeMu.Lock()
	s.conn.SetWriteDeadline(time.Now().Add(s.timeout))
	err := s.conn.WriteJSON(msg)
	s.writeMu.Unlock()
	if err != nil {
		// 写失败即会话破损：标记 + 通知读侧收尾（EventError 由 readLoop 发出）。
		s.markBroken()
		return fmt.Errorf("asr openai: append audio: %w", err)
	}
	return nil
}

func (s *session) Close() error {
	s.markBroken()
	// 先闭 done 再断连：readLoop 随后的读错误经 done 判定为本方收尾，
	// 不误报 EventError。不等待服务端 final（未触发 VAD 的尾部缓冲按
	// 协议被服务端丢弃——管线只把已收到的 final 句送翻译）。
	s.doneOnce()
	_ = s.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(s.timeout))
	_ = s.conn.Close()
	return nil
}

// doneOnce 幂等关闭 done（Close 与 FeedAudio 写失败路径都可能收尾）。
func (s *session) doneOnce() {
	s.doneMu.Lock()
	if !s.doneClosed {
		s.doneClosed = true
		close(s.done)
	}
	s.doneMu.Unlock()
}

func (s *session) markBroken() {
	s.brokenMu.Lock()
	s.broken = true
	s.brokenMu.Unlock()
}

// readLoop 读侧独占循环：协议事件 → 契约事件映射；通道发送与 done 竞争，
// 消费方放弃会话（Close）后立即退出。退出时关闭事件通道（消费方 range 到
// 关闭即会话终止）。
func (s *session) readLoop() {
	// 收线：close 与 emit 同锁串行（emitMu 不变量）。先置 eventsClosed 再
	// close——此后到达的 emit 走丢弃分支，绝不触碰已关闭通道；close 前最后
	// 一批缓冲事件仍可被消费方 range 排干（契约不变）。
	defer func() {
		s.emitMu.Lock()
		s.eventsClosed = true
		close(s.events)
		s.emitMu.Unlock()
	}()
	for {
		select {
		case <-s.done:
			return
		default:
		}
		var ev serverEvent
		if err := s.conn.ReadJSON(&ev); err != nil {
			if isGracefulClose(err) {
				// 远端正常收尾：会话已终止，后续 FeedAudio 按契约返
				// ErrSessionClosed；不发 EventError（非断裂）。
				s.markBroken()
				return
			}
			// 读侧断裂：若非 Close 主动收尾（done 未闭），作为破损事件透出。
			select {
			case <-s.done:
			default:
				s.markBroken() // 先置破损再发事件：EventError 一旦可见，FeedAudio 必返 ErrSessionClosed
				s.emit(asr.Event{Kind: asr.EventError, Err: fmt.Errorf("asr openai: stream read: %w", err)})
			}
			return
		}
		switch ev.Type {
		case "input_audio_buffer.speech_started":
			s.turnSeq++
			s.emit(asr.Event{Kind: asr.EventSpeechStart, Seq: s.turnSeq})
		case "conversation.item.input_audio_transcription.delta":
			s.emit(asr.Event{Kind: asr.EventPartial, Seq: s.turnSeq, Text: ev.Delta})
		case "input_audio_buffer.speech_stopped":
			s.emit(asr.Event{Kind: asr.EventSpeechEnd, Seq: s.turnSeq})
		case "conversation.item.input_audio_transcription.completed":
			s.emit(asr.Event{Kind: asr.EventFinal, Seq: s.turnSeq, Text: ev.Transcript})
		case "error":
			err := errors.New("asr openai: server error")
			if ev.Error != nil && ev.Error.Message != "" {
				err = fmt.Errorf("asr openai: server error (%s): %s", ev.Error.Type, ev.Error.Message)
			}
			s.emit(asr.Event{Kind: asr.EventError, Err: err})
			// 服务端 error 事件 = 会话破损：按契约收尾，消费方 Close 重建。
			s.markBroken()
			return
		default:
			// session.created/updated、buffer.committed 等会务事件不映射。
		}
	}
}

// emit 事件投递：与收线 close 同锁串行（见 emitMu 注释），通道关闭后到达
// 的 emit 静默丢弃而非 panic。
func (s *session) emit(ev asr.Event) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	if s.eventsClosed {
		return
	}
	select {
	case s.events <- ev:
	case <-s.done:
	}
}

// isGracefulClose 远端按协议正常关闭（1000/1001 或 TLS 抖动的 abnormal）算
// 收尾；其余读错误视为断裂。
func isGracefulClose(err error) bool {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway
	}
	return errors.Is(err, websocket.ErrCloseSent)
}
