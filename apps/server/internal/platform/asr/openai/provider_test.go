package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/asr"

	"github.com/gorilla/websocket"
)

// 编译期契约断言：实现必须满足 asr.Recognizer。
var _ asr.Recognizer = (*Provider)(nil)

const testTimeout = 3 * time.Second

var pcmFormat = asr.AudioFormat{SampleRate: 24000, Channels: 1, Encoding: "pcm16"}

// sessionUpdateReceived 服务端侧捕获的会话状态（session.update 参数面 +
// 已收音频 append）。
type sessionUpdateReceived struct {
	Model       string
	Language    string
	Format      string
	VadType     string
	SilenceMs   int
	Appends     [][]byte
	UpdateCount int
}

// streamServer 可编程的 OpenAI 兼容流式转写端点：收到 session.update 后
// 按序回放 script（原始 WS JSON），随后按 closeCode 发关闭帧（0 = 不发，
// 由 defer conn.Close() 造成 1006 异常断开）；afterScript 在收尾动作之后
// 调用（可挂周期发送器等自定义行为，返回即连接收尾）。
type streamServer struct {
	srv         *httptest.Server
	updates     chan sessionUpdateReceived
	dials       chan struct{}
	script      []string
	closeCode   int
	readUpdates func(update sessionUpdateReceived)
	afterScript func(conn *websocket.Conn)
}

func newStreamServer(t *testing.T, script []string, closeCode int) *streamServer {
	t.Helper()
	s := &streamServer{
		updates:   make(chan sessionUpdateReceived, 8),
		dials:     make(chan struct{}, 1),
		script:    script,
		closeCode: closeCode,
	}
	upgrader := websocket.Upgrader{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.dials <- struct{}{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		state := sessionUpdateReceived{}
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type    string `json:"type"`
				Audio   string `json:"audio"`
				Session struct {
					InputAudioFormat        string `json:"input_audio_format"`
					InputAudioTranscription struct {
						Model    string `json:"model"`
						Language string `json:"language"`
					} `json:"input_audio_transcription"`
					TurnDetection *struct {
						Type              string `json:"type"`
						SilenceDurationMs int    `json:"silence_duration_ms"`
					} `json:"turn_detection"`
				} `json:"session"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "transcription_session.update":
				state.UpdateCount++
				state.Model = msg.Session.InputAudioTranscription.Model
				state.Language = msg.Session.InputAudioTranscription.Language
				state.Format = msg.Session.InputAudioFormat
				if msg.Session.TurnDetection != nil {
					state.VadType = msg.Session.TurnDetection.Type
					state.SilenceMs = msg.Session.TurnDetection.SilenceDurationMs
				}
				s.updates <- state
				if s.readUpdates != nil {
					s.readUpdates(state)
				}
				// 会话参数到位后回放脚本：保证客户端看到的时序是
				// update 处理完才轮到服务端事件。
				for _, frame := range s.script {
					_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
				}
				if s.closeCode != 0 {
					_ = conn.WriteControl(websocket.CloseMessage,
						websocket.FormatCloseMessage(s.closeCode, "bye"), time.Now().Add(time.Second))
				}
				if s.afterScript != nil {
					s.afterScript(conn)
				}
			case "input_audio_buffer.append":
				audio, _ := base64.StdEncoding.DecodeString(msg.Audio)
				state.Appends = append(state.Appends, audio)
				s.updates <- state
			}
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func newTestProvider(t *testing.T, httpURL string) *Provider {
	t.Helper()
	return NewProvider(Config{APIKey: "key-1", BaseURL: httpURL, Model: "gpt-4o-transcribe", Language: "zh"})
}

func collectEvents(t *testing.T, ch <-chan asr.Event, want int) []asr.Event {
	t.Helper()
	var out []asr.Event
	deadline := time.After(testTimeout)
	for len(out) < want {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed early, got %d/%d: %+v", len(out), want, out)
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("timed out collecting %d events, got %d: %+v", want, len(out), out)
		}
	}
	return out
}

func drainClosed(t *testing.T, ch <-chan asr.Event) []asr.Event {
	t.Helper()
	var out []asr.Event
	deadline := time.After(testTimeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatal("timed out waiting for events channel to close")
		}
	}
}

func TestValidateFormat(t *testing.T) {
	cases := []struct {
		name   string
		format asr.AudioFormat
		wantOK bool
	}{
		{name: "pcm16 ok", format: pcmFormat, wantOK: true},
		{name: "pcm_s16le alias ok", format: asr.AudioFormat{SampleRate: 24000, Channels: 1, Encoding: "PCM_S16LE"}, wantOK: true},
		{name: "flac rejected", format: asr.AudioFormat{SampleRate: 24000, Channels: 1, Encoding: "flac"}, wantOK: false},
		{name: "wrong rate rejected", format: asr.AudioFormat{SampleRate: 16000, Channels: 1, Encoding: "pcm16"}, wantOK: false},
		{name: "stereo rejected", format: asr.AudioFormat{SampleRate: 24000, Channels: 2, Encoding: "pcm16"}, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFormat(tc.format)
			if tc.wantOK && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantOK && !errors.Is(err, asr.ErrUnsupportedFormat) {
				t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
			}
		})
	}
}

func TestNewSessionRejectsBadFormatBeforeDialing(t *testing.T) {
	s := newStreamServer(t, nil, 0)
	p := newTestProvider(t, s.srv.URL)

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{
		Format: asr.AudioFormat{SampleRate: 16000, Channels: 1, Encoding: "opus"},
	})
	if !errors.Is(err, asr.ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
	if sess != nil || events != nil {
		t.Fatal("failed NewSession must return nil session/channel")
	}
	select {
	case <-s.dials:
		t.Fatal("format validation must happen before any dial")
	default:
	}
}

func TestSessionEventMapping(t *testing.T) {
	script := []string{
		`{"type":"transcription_session.created"}`, // 会务事件：不映射
		`{"type":"input_audio_buffer.speech_started"}`,
		`{"type":"conversation.item.input_audio_transcription.delta","delta":"你好"}`,
		`{"type":"conversation.item.input_audio_transcription.delta","delta":"，世界"}`,
		`{"type":"input_audio_buffer.speech_stopped"}`,
		`{"type":"conversation.item.input_audio_transcription.completed","transcript":"你好，世界。"}`,
	}
	s := newStreamServer(t, script, 0)
	s.readUpdates = func(update sessionUpdateReceived) {
		if update.Model != "gpt-4o-transcribe" || update.Language != "zh" || update.Format != "pcm16" {
			t.Errorf("session update degraded: %+v", update)
		}
		if update.VadType != "server_vad" || update.SilenceMs != 500 {
			t.Errorf("server VAD not configured per design §2.2: %+v", update)
		}
	}
	p := newTestProvider(t, s.srv.URL)

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{SourceLang: "zh", Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	chunk := []byte{0x01, 0x02, 0x03, 0x04}
	if err := sess.FeedAudio(chunk); err != nil {
		t.Fatalf("FeedAudio: %v", err)
	}

	got := collectEvents(t, events, 5)
	wantKinds := []asr.EventKind{asr.EventSpeechStart, asr.EventPartial, asr.EventPartial, asr.EventSpeechEnd, asr.EventFinal}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Fatalf("event[%d].Kind = %q, want %q (all: %+v)", i, got[i].Kind, want, got)
		}
	}
	if got[0].Seq != 1 {
		t.Fatalf("speech_start seq = %d, want 1", got[0].Seq)
	}
	if got[1].Text != "你好" || got[2].Text != "，世界" {
		t.Fatalf("partial texts degraded: %+v", got)
	}
	if got[4].Text != "你好，世界。" {
		t.Fatalf("final transcript degraded: %+v", got[4])
	}
	for _, ev := range got {
		if (ev.Kind == asr.EventPartial || ev.Kind == asr.EventFinal || ev.Kind == asr.EventSpeechEnd) && ev.Seq != 1 {
			t.Fatalf("partial/final/end must carry the active turn seq 1: %+v", ev)
		}
	}

	// 服务端收到与喂入逐字节一致的音频（base64 往返无损）。
	deadline := time.After(testTimeout)
	for {
		select {
		case state := <-s.updates:
			if len(state.Appends) == 0 {
				continue
			}
			if len(state.Appends) != 1 || string(state.Appends[0]) != string(chunk) {
				t.Fatalf("audio append not transported verbatim: %+v", state.Appends)
			}
			if err := sess.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			for _, ev := range drainClosed(t, events) {
				if ev.Kind == asr.EventError {
					t.Fatal("clean close must not surface EventError")
				}
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for server-side append capture")
		}
	}
}

func TestEmptyChunkSwallowed(t *testing.T) {
	s := newStreamServer(t, nil, 0)
	p := newTestProvider(t, s.srv.URL)
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.FeedAudio(nil); err != nil {
		t.Fatalf("FeedAudio(nil): %v", err)
	}
	if err := sess.FeedAudio([]byte{}); err != nil {
		t.Fatalf("FeedAudio(empty): %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := drainClosed(t, events); len(got) != 0 {
		t.Fatalf("no events expected, got %+v", got)
	}
}

func TestServerErrorBreaksSession(t *testing.T) {
	script := []string{
		`{"type":"error","error":{"type":"invalid_request_error","message":"model not found"}}`,
	}
	s := newStreamServer(t, script, 0)
	p := newTestProvider(t, s.srv.URL)

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	got := drainClosed(t, events)
	if len(got) != 1 || got[0].Kind != asr.EventError {
		t.Fatalf("events = %+v, want single EventError", got)
	}
	if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "model not found") {
		t.Fatalf("EventError must carry server message: %+v", got[0])
	}
	// 破损会话：FeedAudio 必返 ErrSessionClosed（契约语义）。
	if err := sess.FeedAudio([]byte("x")); !errors.Is(err, asr.ErrSessionClosed) {
		t.Fatalf("FeedAudio after EventError = %v, want ErrSessionClosed", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close after break: %v", err)
	}
}

func TestRemoteAbnormalCloseSurfacesEventError(t *testing.T) {
	// 服务端以 1011（internal）异常关闭：读侧断裂 → EventError；随后
	// FeedAudio 返 ErrSessionClosed（先置破损再发事件的顺序保证）。
	s := newStreamServer(t, nil, websocket.CloseInternalServerErr)
	p := newTestProvider(t, s.srv.URL)

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	got := drainClosed(t, events)
	if len(got) != 1 || got[0].Kind != asr.EventError {
		t.Fatalf("events = %+v, want single EventError", got)
	}
	if !strings.Contains(got[0].Err.Error(), "stream read") {
		t.Fatalf("EventError must carry read failure: %+v", got[0])
	}
	if err := sess.FeedAudio([]byte("x")); !errors.Is(err, asr.ErrSessionClosed) {
		t.Fatalf("FeedAudio after EventError = %v, want ErrSessionClosed", err)
	}
	_ = sess.Close()
}

func TestNewSessionDialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	p := newTestProvider(t, url)

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err == nil || !strings.Contains(err.Error(), "dial") {
		t.Fatalf("err = %v, want dial failure", err)
	}
	if sess != nil || events != nil {
		t.Fatal("failed dial must return nil session/channel")
	}
}

func TestHealthCheckVariants(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer key-1" {
				t.Errorf("health probe degraded: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		p := newTestProvider(t, srv.URL)
		if err := p.HealthCheck(context.Background()); err != nil {
			t.Fatalf("HealthCheck: %v", err)
		}
	})
	t.Run("unhealthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		p := newTestProvider(t, srv.URL)
		if err := p.HealthCheck(context.Background()); err == nil {
			t.Fatal("4xx health must fail")
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()
		p := newTestProvider(t, url)
		if err := p.HealthCheck(context.Background()); err == nil {
			t.Fatal("health against closed server must fail")
		}
	})
}

func TestProviderURLDerivation(t *testing.T) {
	p := NewProvider(Config{})
	if p.wsURL != "wss://api.openai.com/v1" || p.restBase != "https://api.openai.com/v1" {
		t.Fatalf("default URL derivation degraded: ws=%q rest=%q", p.wsURL, p.restBase)
	}
	if p.model != defaultModel || p.timeout != defaultTimeout {
		t.Fatalf("constructor defaults degraded: %+v", p)
	}
	p2 := NewProvider(Config{BaseURL: "https://gw.example/v1/", Timeout: 5 * time.Second})
	if p2.wsURL != "wss://gw.example/v1" || p2.timeout != 5*time.Second {
		t.Fatalf("custom URL/timeout derivation degraded: ws=%q timeout=%v", p2.wsURL, p2.timeout)
	}
	if p2.language != "" {
		t.Fatalf("empty language must stay empty, got %q", p2.language)
	}
}

func TestIsGracefulClose(t *testing.T) {
	if !isGracefulClose(&websocket.CloseError{Code: websocket.CloseNormalClosure}) {
		t.Fatal("1000 must be graceful")
	}
	if !isGracefulClose(&websocket.CloseError{Code: websocket.CloseGoingAway}) {
		t.Fatal("1001 must be graceful")
	}
	if isGracefulClose(&websocket.CloseError{Code: websocket.CloseInternalServerErr}) {
		t.Fatal("1011 must NOT be graceful")
	}
	if isGracefulClose(errors.New("plain")) {
		t.Fatal("plain error must NOT be graceful")
	}
}

func TestRemoteGracefulCloseEndsSessionQuietly(t *testing.T) {
	// 服务端主动 1000 收尾（本方未 Close，done 未闭）：readLoop 视为正常
	// 收尾静默退出，不发 EventError。
	s := newStreamServer(t, nil, websocket.CloseNormalClosure)
	p := newTestProvider(t, s.srv.URL)

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := drainClosed(t, events); len(got) != 0 {
		t.Fatalf("graceful remote close must not emit events, got %+v", got)
	}
	// 会话已终止：FeedAudio 按契约返 ErrSessionClosed。
	if err := sess.FeedAudio([]byte{0x01}); !errors.Is(err, asr.ErrSessionClosed) {
		t.Fatalf("FeedAudio after remote graceful close = %v, want ErrSessionClosed", err)
	}
	_ = sess.Close()
}

func TestNewSessionUpdateWriteFailure(t *testing.T) {
	// 拨号注入点 + 过期写截止：握手成功但 session.update 写出必然失败，
	// 确定性命中"send session update"分支。
	s := newStreamServer(t, nil, 0)
	p := newTestProvider(t, s.srv.URL)
	p.timeout = time.Nanosecond
	p.dial = func(ctx context.Context, urlStr string, hdr http.Header) (*websocket.Conn, error) {
		d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
		conn, _, err := d.DialContext(ctx, urlStr, hdr)
		return conn, err
	}

	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err == nil || !strings.Contains(err.Error(), "send session update") {
		t.Fatalf("err = %v, want session update write failure", err)
	}
	if sess != nil || events != nil {
		t.Fatal("failed handshake write must return nil session/channel")
	}
}

func TestFeedAudioWriteFailure(t *testing.T) {
	s := newStreamServer(t, nil, 0)
	p := newTestProvider(t, s.srv.URL)
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// 白盒置过期写截止：FeedAudio 写出必失败 → 会话破损。
	sess.(*session).timeout = time.Nanosecond
	if err := sess.FeedAudio([]byte{0x01}); err == nil || !strings.Contains(err.Error(), "append audio") {
		t.Fatalf("err = %v, want append audio write failure", err)
	}
	if err := sess.FeedAudio([]byte{0x01}); !errors.Is(err, asr.ErrSessionClosed) {
		t.Fatalf("FeedAudio after write failure = %v, want ErrSessionClosed", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, ev := range drainClosed(t, events) {
		if ev.Kind == asr.EventError {
			t.Fatalf("local write-failure teardown must not double-report EventError: %+v", ev)
		}
	}
}

func TestEmitAbandonsWhenDoneClosedAndBufferFull(t *testing.T) {
	// 事件缓冲占满 + done 关闭：emit 只能走 done 分支，绝不阻塞。
	s := newStreamServer(t, nil, 0)
	p := newTestProvider(t, s.srv.URL)
	sess, _, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sk := sess.(*session)
	for i := 0; i < eventsChannelBuffer; i++ {
		sk.emit(asr.Event{Kind: asr.EventPartial, Text: "x"})
	}
	sk.doneOnce()

	completed := make(chan struct{})
	go func() {
		sk.emit(asr.Event{Kind: asr.EventPartial, Text: "blocked?"})
		close(completed)
	}()
	select {
	case <-completed:
	case <-time.After(testTimeout):
		t.Fatal("emit must not block once done is closed and buffer is full")
	}
	_ = sess.Close()
}

func TestEmitAfterCloseIsSilentlyDropped(t *testing.T) {
	// 收线（readLoop 退出 close 事件通道）后到达的 emit：走 eventsClosed 丢弃
	// 分支——不 panic、不投递（emit/close 同锁不变量的关闭侧行为面）。
	s := newStreamServer(t, nil, 0)
	p := newTestProvider(t, s.srv.URL)
	sess, _, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sk := sess.(*session)
	sk.doneOnce()
	if evs := drainClosed(t, sk.events); len(evs) != 0 {
		t.Fatalf("events after quiet teardown = %+v, want none", evs)
	}
	sk.emit(asr.Event{Kind: asr.EventPartial, Text: "late"}) // 绝不 panic
	_ = sess.Close()
}

func TestReadLoopExitsOnDoneAfterLiveRead(t *testing.T) {
	// done 关闭但连接仍活：readLoop 成功再读一帧（未映射会务事件）后于
	// 循环顶退出——验证顶部 done 检查分支。
	s := newStreamServer(t, nil, 0)
	stop := make(chan struct{})
	s.afterScript = func(conn *websocket.Conn) {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				if conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"transcription_session.created"}`)) != nil {
					return
				}
			}
		}
	}
	p := newTestProvider(t, s.srv.URL)
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{Format: pcmFormat})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.(*session).doneOnce()

	if got := drainClosed(t, events); len(got) != 0 {
		t.Fatalf("no events expected on done-driven exit, got %+v", got)
	}
	close(stop)
	_ = sess.Close()
}

func TestHealthCheckBuildRequestFailure(t *testing.T) {
	p := newTestProvider(t, "http://exa mple.com/v1")
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("malformed base URL must fail health check")
	}
}
