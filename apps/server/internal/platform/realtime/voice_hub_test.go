package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	translationapp "servify/apps/server/internal/modules/translation/application"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// fakeVoiceStream 可编程的 VoiceAudioStream 替身：记录喂入分片，可注入
// Feed 失败。
type fakeVoiceStream struct {
	mu      sync.Mutex
	chunks  [][]byte
	feedErr error
	closed  bool
}

func (s *fakeVoiceStream) Feed(chunk []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.feedErr != nil {
		return s.feedErr
	}
	s.chunks = append(s.chunks, append([]byte(nil), chunk...))
	return nil
}

func (s *fakeVoiceStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeVoiceStream) recordedChunks() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chunks
}

func (s *fakeVoiceStream) wasClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// fakeVoiceRuntime 可编程的 VoiceStreamStarter 替身：录制启动参数，把
// sink 回传给测试用于注入管线产出；可预设启动失败。
type fakeVoiceRuntime struct {
	mu       sync.Mutex
	startErr error
	sessions []string
	speakers []string
	sinks    []translationdelivery.VoiceStreamSink
	stream   *fakeVoiceStream
}

func newFakeVoiceRuntime() *fakeVoiceRuntime {
	return &fakeVoiceRuntime{stream: &fakeVoiceStream{}}
}

func (r *fakeVoiceRuntime) StartAudioStream(ctx context.Context, sessionID, speaker string, sink translationdelivery.VoiceStreamSink) (translationdelivery.VoiceAudioStream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.sessions = append(r.sessions, sessionID)
	r.speakers = append(r.speakers, speaker)
	r.sinks = append(r.sinks, sink)
	return r.stream, nil
}

func (r *fakeVoiceRuntime) lastSink() translationdelivery.VoiceStreamSink {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sinks) == 0 {
		return nil
	}
	return r.sinks[len(r.sinks)-1]
}

func (r *fakeVoiceRuntime) startedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// sinkForSession 按会话取启动时捕获的 sink：readPump 调度顺序不保证与建
// 连顺序一致，多连接测试不能按槽位下标对号。
func (r *fakeVoiceRuntime) sinkForSession(sessionID string) translationdelivery.VoiceStreamSink {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.sessions {
		if s == sessionID {
			return r.sinks[i]
		}
	}
	return nil
}

// newVoiceTestHub 起 hub 事件循环 + httptest 语音通道端点，返回 hub 与
// 通道 URL。
func newVoiceTestHub(t *testing.T, runtime translationdelivery.VoiceStreamStarter) (*VoiceHub, string) {
	t.Helper()
	hub := NewVoiceHub()
	if runtime != nil {
		hub.SetVoiceTranslationRuntime(runtime)
	}
	go hub.Run()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/ws/voice", hub.HandleVoiceWebSocket)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return hub, server.URL
}

func dialVoice(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(url, "http") + "/api/v1/ws/voice?session_id=s1"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial voice channel: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readVoiceFrame 带超时读一帧下行 JSON。
func readVoiceFrame(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("frame not JSON: %v (%s)", err, payload)
	}
	return frame
}

// waitVoiceClose 带超时等待连接关闭（中间帧丢弃）。
func waitVoiceClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// waitForStarts 轮询等 N 条语音流全部启动（握手后异步起管线）。
func waitForStarts(t *testing.T, runtime *fakeVoiceRuntime, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.startedCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d streams started", runtime.startedCount(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForVoiceClient 轮询等连接进入会话表。register 是无缓冲信道：投递
// 交接完成到 Run 执行 map 插入之间有调度窗口，管线已启动不代表已可查。
func waitForVoiceClient(t *testing.T, hub *VoiceHub) *VoiceClient {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		hub.mutex.RLock()
		var found *VoiceClient
		for _, c := range hub.clients {
			found = c
		}
		hub.mutex.RUnlock()
		if found != nil {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatal("client not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVoiceHandshakeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("missing session id", func(t *testing.T) {
		hub, url := newVoiceTestHub(t, newFakeVoiceRuntime())
		resp, err := http.Get(url + "/api/v1/ws/voice")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (hub=%p)", resp.StatusCode, hub)
		}
	})
	t.Run("invalid speaker", func(t *testing.T) {
		_, url := newVoiceTestHub(t, newFakeVoiceRuntime())
		resp, err := http.Get(url + "/api/v1/ws/voice?session_id=s1&speaker=robot")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
	t.Run("runtime not configured", func(t *testing.T) {
		_, url := newVoiceTestHub(t, nil)
		resp, err := http.Get(url + "/api/v1/ws/voice?session_id=s1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
	})
	t.Run("token validator rejects", func(t *testing.T) {
		hub, url := newVoiceTestHub(t, newFakeVoiceRuntime())
		hub.SetTokenValidator(func(sessionID, accessToken string) error {
			return errors.New("bad token")
		})
		resp, err := http.Get(url + "/api/v1/ws/voice?session_id=s1&access_token=x")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})
}

func TestVoiceChannelHappyPath(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)

	conn := dialVoice(t, url)
	// 管线在握手后异步启动：轮询等 StartAudioStream 落定再取 sink。
	waitForStarts(t, runtime, 1)
	sink := runtime.lastSink()
	if sink == nil {
		t.Fatal("pipeline must start on connect")
	}

	// 上行二进制音频直通 stream.Feed；空帧跳过不入 Feed。
	if err := conn.WriteMessage(websocket.BinaryMessage, nil); err != nil {
		t.Fatalf("write empty audio: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"whatever"}`)); err != nil {
		t.Fatalf("write text: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(runtime.stream.recordedChunks()) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("audio chunk never reached stream.Feed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 文本帧不计入音频。
	if got := runtime.stream.recordedChunks(); len(got) != 1 || string(got[0]) != "\x01\x02" {
		t.Fatalf("chunks = %v, want single binary passthrough", got)
	}

	// 管线产出 → 下行帧：delta / final / audio。OnSpeechStart 与 OnError
	// 不产帧（partial 已承载"正在说"；失败零帧）。
	sink.OnSpeechStart(1)
	sink.OnError(errors.New("translate exploded"))
	sink.OnPartial(1, "你说")
	sink.OnCaption(translationapp.VoiceCaption{
		Seq: 1, Source: "你说。", Translated: "You said.", SourceLang: "", TargetLang: "en",
	})
	sink.OnAudio(1, []byte("mp3"), "mp3")

	delta := readVoiceFrame(t, conn)
	if delta["type"] != "translation-delta" {
		t.Fatalf("frame = %+v, want translation-delta", delta)
	}
	data := delta["data"].(map[string]interface{})
	if data["speaker"] != "visitor" || data["turn_seq"] != float64(1) || data["text"] != "你说" {
		t.Fatalf("delta payload degraded: %+v", data)
	}
	if delta["session_id"] != "s1" {
		t.Fatalf("frame must be session-scoped: %+v", delta)
	}

	final := readVoiceFrame(t, conn)
	if final["type"] != "translation-final" {
		t.Fatalf("frame = %+v, want translation-final", final)
	}
	fdata := final["data"].(map[string]interface{})
	if fdata["seq"] != float64(1) || fdata["original"] != "你说。" || fdata["content"] != "You said." ||
		fdata["target_lang"] != "en" || fdata["degraded"] != false {
		t.Fatalf("final payload degraded: %+v", fdata)
	}

	audio := readVoiceFrame(t, conn)
	if audio["type"] != "translation-audio" {
		t.Fatalf("frame = %+v, want translation-audio", audio)
	}
	adata := audio["data"].(map[string]interface{})
	if adata["format"] != "mp3" ||
		adata["audio"] != base64.StdEncoding.EncodeToString([]byte("mp3")) {
		t.Fatalf("audio payload degraded: %+v", adata)
	}

	// 客户端收线：stream.Close 必被调用（资源收尾）。
	_ = conn.Close()
	deadline = time.Now().Add(3 * time.Second)
	for !runtime.stream.wasClosed() {
		if time.Now().After(deadline) {
			t.Fatal("stream.Close never called after disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVoiceChannelSpeakerParamPassed(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)

	wsURL := "ws" + strings.TrimPrefix(url, "http") + "/api/v1/ws/voice?session_id=s1&speaker=AGENT"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	waitForStarts(t, runtime, 1)
	if runtime.speakers[0] != "agent" {
		t.Fatalf("speaker = %q, want agent (normalized lowercase)", runtime.speakers[0])
	}
}

func TestVoiceChannelStartFailureSurfacesVoiceError(t *testing.T) {
	t.Run("inactive session", func(t *testing.T) {
		runtime := newFakeVoiceRuntime()
		runtime.startErr = translationdelivery.ErrVoiceChannelInactive
		_, url := newVoiceTestHub(t, runtime)

		conn := dialVoice(t, url)
		frame := readVoiceFrame(t, conn)
		if frame["type"] != "voice-error" {
			t.Fatalf("frame = %+v, want voice-error", frame)
		}
		data := frame["data"].(map[string]interface{})
		if data["code"] != "disabled" || !strings.Contains(data["message"].(string), "preference") {
			t.Fatalf("error payload degraded: %+v", data)
		}
		waitVoiceClose(t, conn)
	})
	t.Run("asr unavailable", func(t *testing.T) {
		runtime := newFakeVoiceRuntime()
		runtime.startErr = errors.New("dial transcription stream: ws://internal-asr.internal:443 refused")
		_, url := newVoiceTestHub(t, runtime)

		conn := dialVoice(t, url)
		frame := readVoiceFrame(t, conn)
		data := frame["data"].(map[string]interface{})
		if data["code"] != "asr_unavailable" {
			t.Fatalf("error payload degraded: %+v", data)
		}
		// provider 原始错误可能含内部端点：只回固定文案。
		if message := data["message"].(string); strings.Contains(message, "internal-asr") || strings.Contains(message, "ws://") {
			t.Fatalf("provider detail leaked to client: %q", message)
		}
		waitVoiceClose(t, conn)
	})
	t.Run("translation unconfigured maps to disabled", func(t *testing.T) {
		runtime := newFakeVoiceRuntime()
		runtime.startErr = translationdelivery.ErrTranslationUnavailable
		_, url := newVoiceTestHub(t, runtime)

		conn := dialVoice(t, url)
		frame := readVoiceFrame(t, conn)
		data := frame["data"].(map[string]interface{})
		if data["code"] != "disabled" {
			t.Fatalf("error payload degraded: %+v", data)
		}
		waitVoiceClose(t, conn)
	})
}

func TestVoiceChannelFeedFailureBreaksStream(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	runtime.stream.feedErr = errors.New("session broken")
	_, url := newVoiceTestHub(t, runtime)

	conn := dialVoice(t, url)
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01}); err != nil {
		t.Fatalf("write: %v", err)
	}
	frame := readVoiceFrame(t, conn)
	if frame["type"] != "voice-error" {
		t.Fatalf("frame = %+v, want voice-error", frame)
	}
	data := frame["data"].(map[string]interface{})
	if data["code"] != "stream_broken" {
		t.Fatalf("error payload degraded: %+v", data)
	}
	waitVoiceClose(t, conn)
	if !runtime.stream.wasClosed() {
		t.Fatal("broken stream must be closed on teardown")
	}
}

func TestVoiceChannelSessionIsolation(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)

	connA := dialVoice(t, url) // session s1
	// 会话 s2 的第二连接。
	wsURL := "ws" + strings.TrimPrefix(url, "http") + "/api/v1/ws/voice?session_id=s2"
	connB, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial s2: %v", err)
	}
	t.Cleanup(func() { _ = connB.Close() })

	waitForStarts(t, runtime, 2)

	// s1 的产出不得漏到 s2（sink 按会话对号：readPump 调度顺序不定）。
	sinkA := runtime.sinkForSession("s1")
	if sinkA == nil {
		t.Fatal("s1 sink not captured")
	}
	sinkA.OnCaption(translationapp.VoiceCaption{Seq: 1, Source: "a", Translated: "a"})
	frameA := readVoiceFrame(t, connA)
	if frameA["type"] != "translation-final" {
		t.Fatalf("s1 frame = %+v", frameA)
	}
	_ = connB.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := connB.ReadMessage(); err == nil {
		t.Fatal("s2 must not receive s1 frames")
	}
}

// TestVoiceChannelUpgradeFailure 非 WebSocket 请求走到升级：握手校验全过
// 但无升级头，Upgrade 失败分支只记日志不 panic。
func TestVoiceChannelUpgradeFailure(t *testing.T) {
	_, url := newVoiceTestHub(t, newFakeVoiceRuntime())
	resp, err := http.Get(url + "/api/v1/ws/voice?session_id=s1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing upgrade headers)", resp.StatusCode)
	}
}

// TestVoiceChannelSlowClientKicked 下行缓冲写满的连接在广播时被踢线
// （与会话 WS 同口径）：Send 塞满后再产出一帧，广播循环 close 其 Send
// 并从会话表摘除，writePump 排干余帧发 close 帧退出。
func TestVoiceChannelSlowClientKicked(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)

	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)
	client := waitForVoiceClient(t, hub)

	// 泛洪下行：客户端不读 socket，TCP 缓冲积满后 writePump 阻在写出，
	// Send 随之写满，广播循环走踢线分支（close + 摘除，与会话 WS 同口径
	// 的 4000×256B 泛洪法）。直接压 broadcast 通道（阻塞发送，emit 的
	// 非阻塞丢弃不参与本口径）。
	for i := 0; i < 4000; i++ {
		hub.broadcast <- WebSocketMessage{Type: "translation-delta", SessionID: "s1", Data: strings.Repeat("x", 256)}
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		hub.mutex.RLock()
		_, ok := hub.clients[client.ID]
		hub.mutex.RUnlock()
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow client never kicked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 连接最终被 writePump 的 defer 关闭（客户端读到 close 帧或 EOF）。
	waitVoiceClose(t, conn)
}

// TestVoiceChannelTeardownAfterKickDoesNotPanic 慢客户端被踢线（Run 已关闭
// 其 Send、摘除会话表）后再收线：teardown 命令按"连接不在会话表"跳过，
// 不向已关闭通道投递——客户端泵直投 Send 的写法在此会 send on closed
// channel 打崩进程。测试通过本身即无 panic。
func TestVoiceChannelTeardownAfterKickDoesNotPanic(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)

	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)
	client := waitForVoiceClient(t, hub)

	// 泛洪下行把客户端踢线（Send 已 close、会话表已摘除）。
	for i := 0; i < 4000; i++ {
		hub.broadcast <- WebSocketMessage{Type: "translation-delta", SessionID: "s1", Data: strings.Repeat("x", 256)}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		hub.mutex.RLock()
		_, ok := hub.clients[client.ID]
		hub.mutex.RUnlock()
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow client never kicked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 已踢线的连接再上报收线：必须被静默跳过（无 panic）。
	hub.teardown <- &voiceTeardown{
		client:  client,
		message: client.voiceErrorFrame(voiceErrStreamBroken, "audio stream broken"),
	}
	hub.mutex.RLock()
	_, ok := hub.clients[client.ID]
	hub.mutex.RUnlock()
	if ok {
		t.Fatal("closed connection must stay out of the session table")
	}
	waitVoiceClose(t, conn)
}

// TestVoiceChannelWritePumpExitBranches 直构 writePump 的两个退出分支
// （与会话 WS 同款直构法）：写消息失败退出、ping 失败退出。
func TestVoiceChannelWritePumpExitBranches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Run("write json failure", func(t *testing.T) {
		client := &VoiceClient{
			ID: "c-wperr", SessionID: "s", Conn: conn, Hub: NewVoiceHub(),
			Send: make(chan WebSocketMessage),
		}
		done := make(chan struct{})
		go func() {
			client.writePump()
			close(done)
		}()
		// 关闭传输层后交接一条消息：非缓冲 channel 的交接保证 writePump
		// 收到并走到 WriteJSON 错误分支。
		if err := conn.Close(); err != nil {
			t.Fatalf("close conn: %v", err)
		}
		client.Send <- WebSocketMessage{Type: "translation-delta"}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("writePump did not exit after write failure")
		}
	})

	t.Run("ping failure", func(t *testing.T) {
		conn2, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws", nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn2.Close()
		client := &VoiceClient{
			ID: "c-pingerr", SessionID: "s", Conn: conn2, Hub: NewVoiceHub(),
			Send: make(chan WebSocketMessage), pingInterval: 10 * time.Millisecond,
		}
		done := make(chan struct{})
		go func() {
			client.writePump()
			close(done)
		}()
		// 已关闭连接上的 ping 必然失败：writePump 只能经由 ticker 的 ping
		// 分支退出。
		if err := conn2.Close(); err != nil {
			t.Fatalf("close conn: %v", err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("writePump did not exit via ping branch")
		}
	})
}

// TestVoiceChannelPongRefreshesReadDeadline pong 处理器刷新读超时。读超时
// 注入为 1s、存活期 2.5s：pong 处理器一旦失效，readPump 必在 1s 读超时上
// 死掉，故存活本身即证明"被刷新"。保活 pong 由客户端主动补发（每 50ms）
// 而非依赖服务端 ping 的往返：pong 帧在 TCP 缓冲里排队，服务端读循环每次
// 醒来处理时都以"处理时刻"重锚截止时间，因此单次调度延迟不会误杀读循环
// （CI 高负载下 ping→读→pong 往返链任一环延迟都可能超过 1s 读超时）。
// 服务端自身 100ms ping 仍在跑，顺带覆盖 writePump 的 ping 分支。
// VoiceClient 直构在升级后的服务端连接上（与
// TestVoiceChannelWritePumpExitBranches 同款直构法，但握有的是服务端
// conn——上行分片因此真的走到 readPump）。
func TestVoiceChannelPongRefreshesReadDeadline(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	built := make(chan *VoiceClient, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hub := NewVoiceHub()
		hub.SetVoiceTranslationRuntime(runtime)
		go hub.Run()
		client := &VoiceClient{
			ID: "c-pong", SessionID: "s1", Speaker: translationdelivery.VoiceSpeakerVisitor,
			Conn: conn, Hub: hub, Send: make(chan WebSocketMessage, 16),
			pingInterval: 100 * time.Millisecond, readTimeout: time.Second,
		}
		built <- client
		go client.writePump()
		go client.readPump()
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case <-built:
	case <-time.After(3 * time.Second):
		t.Fatal("server-side voice client never built")
	}
	waitForStarts(t, runtime, 1)
	keepaliveStop := make(chan struct{})

	// 客户端持续读：消费服务端 ping 与任何下行帧（读侧不堵，客户端写方向
	// 才能持续推进）。
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// 保活 pong 补发：WriteControl 是直写，不经读循环，因此不依赖客户端
	// 读 goroutine 被调度。
	keepaliveDone := make(chan struct{})
	go func() {
		defer close(keepaliveDone)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second)); err != nil {
					return
				}
			case <-keepaliveStop:
				return
			}
		}
	}()

	// 远超单个读超时（1s）的存活期：读循环只能靠 pong 刷新续命。
	time.Sleep(2500 * time.Millisecond)
	close(keepaliveStop)
	<-keepaliveDone
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("keepalive")); err != nil {
		t.Fatalf("write after pings: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(runtime.stream.recordedChunks()) < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("upstream chunk lost after ping/pong cycles (stream closed=%v)", runtime.stream.wasClosed())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = conn.Close()
	<-readerDone
}

// TestVoiceChannelUnexpectedCloseLogged 客户端以协议 close 帧收线（非
// going away/abnormal 码）记错误日志；流收尾照常。
func TestVoiceChannelUnexpectedCloseLogged(t *testing.T) {
	logs := &lockedBuffer{}
	origOut := logrus.StandardLogger().Out
	logrus.StandardLogger().SetOutput(logs)
	t.Cleanup(func() { logrus.StandardLogger().SetOutput(origOut) })

	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)

	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)

	if err := conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")); err != nil {
		t.Fatalf("write close: %v", err)
	}
	waitVoiceClose(t, conn)
	// 服务端 readPump 记日志与客户端观测到收线之间无同步关系（收线帧由
	// writePump 写，两者同在 readPump 退出链上但不同 goroutine）：轮询等
	// 日志落定，避免拿调度顺序打赌。
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(logs.String(), "Voice WebSocket error") {
		if time.Now().After(deadline) {
			t.Fatalf("expected unexpected-close log, got %q", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !runtime.stream.wasClosed() {
		t.Fatal("stream must be closed after client disconnect")
	}
}
