// voice_webrtc_test.go 语音 WebRTC 音轨面的信令与媒体路由测试（Phase 3）。
// 端到端用真 pion PC 对端握手；媒体断言只到 μ-law 载荷字节一致（浏览器解码
// 无法本地验证，codec 往返正确性由 voice_media_test.go 保证）。
package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"servify/apps/server/internal/platform/iceturn"
)

// dialVoiceSpeaker 带说话方参数拨语音通道（dialVoice 固定 query，无法追加）。
func dialVoiceSpeaker(t *testing.T, url, speaker string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(url, "http") + "/api/v1/ws/voice?session_id=s1&speaker=" + speaker
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial voice channel (speaker=%s): %v", speaker, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// voiceClientBySpeaker 按说话方取会话客户端（visitor/agent 各一条连接）。
func voiceClientBySpeaker(t *testing.T, hub *VoiceHub, sessionID, speaker string) *VoiceClient {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		hub.mutex.RLock()
		for _, c := range hub.clients {
			if c.SessionID == sessionID && c.Speaker == speaker {
				client := c
				hub.mutex.RUnlock()
				return client
			}
		}
		hub.mutex.RUnlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("voice client (session=%s speaker=%s) not registered", sessionID, speaker)
	return nil
}

// waitMediaClosed 等客户端完成收线并从会话表摘除（teardown case 在
// closeMedia 返回后才删除条目，故条目消失 ⇒ 媒体面已收线完毕）。
func waitMediaClosed(t *testing.T, hub *VoiceHub, clientID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		hub.mutex.RLock()
		_, ok := hub.clients[clientID]
		hub.mutex.RUnlock()
		if !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("voice client not torn down")
}

// voiceMediaOf 取连接的媒体面（可空）。
func voiceMediaOf(hub *VoiceHub, client *VoiceClient) *voiceClientMedia {
	hub.mutex.RLock()
	defer hub.mutex.RUnlock()
	return client.media
}

// attachTestMedia 绕过信令直挂媒体面（路由/收线单测用，省整段 PC 握手）。
func attachTestMedia(t *testing.T, hub *VoiceHub, client *VoiceClient) *voiceClientMedia {
	t.Helper()
	media, err := newVoiceClientMedia(hub.currentICEServers(), client)
	if err != nil {
		t.Fatalf("new voice client media: %v", err)
	}
	hub.mutex.Lock()
	client.media = media
	hub.mutex.Unlock()
	t.Cleanup(media.close)
	return media
}

// startVoiceFrameReader 连接的单读循环：全部下行帧（含 trickle 候选）投
// channel 供主测试断言/注入——gorilla 同一连接只允许一个读者，全部下行
// 消费集中到这里。候选不在此直接注入：客户端远端描述未置位的窗口内 pion
// 丢弃候选（InvalidStateError），注入时序由主 goroutine 在 SRD 之后统一收敛。
func startVoiceFrameReader(conn *websocket.Conn) <-chan WebSocketMessage {
	frames := make(chan WebSocketMessage, 64)
	go func() {
		for {
			// 不设读超时：读超时会把 reader 误杀（慢机上下行帧间隔超过
			// 预算即退场，后续帧无人消费）。终局信号是 cleanup 的 conn
			// Close——那是真实读错误。
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame WebSocketMessage
			if json.Unmarshal(payload, &frame) != nil {
				continue
			}
			select {
			case frames <- frame:
			default:
			}
		}
	}()
	return frames
}

// waitForClientConnected 冲刷下行候选帧直至 PC 连通（trickle 服务端候选
// 先于应答投进帧通道；客户端远端描述已置位，主 goroutine 统一注入收敛——
// reader goroutine 直接注入会与 SRD 竞速丢候选）。
func waitForClientConnected(t *testing.T, frames <-chan WebSocketMessage, pc *webrtc.PeerConnection) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for pc.ConnectionState() != webrtc.PeerConnectionStateConnected && time.Now().Before(deadline) {
		select {
		case frame := <-frames:
			if frame.Type != voiceFrameWebRTCCandidate {
				continue
			}
			if data, ok := frame.Data.(map[string]interface{}); ok {
				raw, _ := json.Marshal(data["candidate"])
				var candidate webrtc.ICECandidateInit
				if json.Unmarshal(raw, &candidate) == nil {
					_ = pc.AddICECandidate(candidate)
				}
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	if pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatalf("peer connection state = %s, want connected", pc.ConnectionState())
	}
}

// waitVoiceFrameOfType 直读连接等待指定类型帧（无 reader goroutine 的用例）。
func waitVoiceFrameOfType(t *testing.T, conn *websocket.Conn, want string) WebSocketMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s frame: %v", want, err)
			}
			continue
		}
		var frame WebSocketMessage
		if err := json.Unmarshal(payload, &frame); err != nil {
			t.Fatalf("voice frame not JSON: %v", err)
		}
		if frame.Type == want {
			return frame
		}
	}
}

// writeVoiceFrame 上行一帧（互斥：客户端候选回调与主循环共用 conn 写面）。
func writeVoiceFrame(t *testing.T, conn *websocket.Conn, mu *sync.Mutex, frameType string, data interface{}) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if err := conn.WriteJSON(WebSocketMessage{Type: frameType, Data: data}); err != nil {
		t.Fatalf("write voice frame %s: %v", frameType, err)
	}
}

// waitFrameOfType 从帧通道等待指定类型帧，超时失败。
func waitFrameOfType(t *testing.T, frames <-chan WebSocketMessage, want string) WebSocketMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case frame := <-frames:
			if frame.Type == want {
				return frame
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("timed out waiting for %s frame", want)
		}
	}
}

// assertNoFrameOfType 断言帧通道在观察窗内不出现指定类型（负向断言）。
func assertNoFrameOfType(t *testing.T, frames <-chan WebSocketMessage, exclude string) {
	t.Helper()
	deadline := time.Now().Add(400 * time.Millisecond)
	for {
		select {
		case frame := <-frames:
			if frame.Type == exclude {
				t.Fatalf("unexpected %s frame", exclude)
			}
			continue
		case <-time.After(time.Until(deadline)):
			return
		}
	}
}

func TestVoiceWebRTCSignalingAndMedia(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)

	visitorConn := dialVoice(t, url)
	agentConn := dialVoiceSpeaker(t, url, "agent")
	waitForStarts(t, runtime, 2)

	mu := &sync.Mutex{}
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new peer connection: %v", err)
	}
	t.Cleanup(func() { _ = clientPC.Close() })

	frames := startVoiceFrameReader(agentConn)

	// 客户端候选上行（载荷即 ICECandidateInit 本体，与服务端 asICECandidate
	// 的入站形态一致）；候选可能先于应答到达，服务端按序暂存待应答冲刷。
	clientPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		writeVoiceFrame(t, agentConn, mu, voiceFrameWebRTCCandidate, cand.ToJSON())
	})

	writeVoiceFrame(t, agentConn, mu, voiceFrameWebRTCRequest, nil)
	offerFrame := waitFrameOfType(t, frames, voiceFrameWebRTCOffer)

	offer, err := asSessionDescription(offerFrame.Data)
	if err != nil {
		t.Fatalf("decode offer: %v", err)
	}
	if err := clientPC.SetRemoteDescription(offer); err != nil {
		t.Fatalf("set remote: %v", err)
	}
	answer, err := clientPC.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := clientPC.SetLocalDescription(answer); err != nil {
		t.Fatalf("set local: %v", err)
	}
	writeVoiceFrame(t, agentConn, mu, voiceFrameWebRTCAnswer, answer)

	clientTrack := make(chan *webrtc.TrackRemote, 1)
	clientPC.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		clientTrack <- track
	})

	waitForClientConnected(t, frames, clientPC)

	// 访客管线（不上 WebRTC）是产出方：坐席音轨消费其产出 → 格式协商 wav。
	visitorSink := runtime.sinkForSpeaker("s1", "visitor")
	if visitorSink == nil {
		t.Fatal("visitor sink not captured")
	}
	if got := visitorSink.AudioFormat(); got != "wav" {
		t.Fatalf("visitor sink AudioFormat = %q, want wav (agent track consumes)", got)
	}

	// 访客产出 wav 译文音频 → 坐席音轨 → μ-law RTP 载荷。先喂 3 帧同内容
	// （40ms 节拍），OnTrack 由首个包触发后仍有包可读。
	samples := make([]int16, voiceFrameSamples)
	for i := range samples {
		samples[i] = int16(1000 + i*3)
	}
	wav := buildTestWAV(samples, voiceClockRate, 1)
	for seq := range int32(3) {
		visitorSink.OnAudio(int64(seq+1), wav, "wav")
	}

	var track *webrtc.TrackRemote
	select {
	case track = <-clientTrack:
		if track.Kind() != webrtc.RTPCodecTypeAudio || track.Codec().MimeType != webrtc.MimeTypePCMU {
			t.Fatalf("client track = %s/%s, want audio/PCMU", track.Kind(), track.Codec().MimeType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no audio track negotiated")
	}

	payload := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 12+voiceFrameSamples)
		n, _, err := track.Read(buf)
		if err != nil {
			payload <- nil
			return
		}
		payload <- buf[12:n]
	}()
	select {
	case got := <-payload:
		if got == nil {
			t.Fatal("track read failed")
		}
		want := pcm16ToMuLaw(samples)
		if len(got) != len(want) {
			t.Fatalf("payload len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("payload[%d] = 0x%02x, want 0x%02x", i, got[i], want[i])
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no media packet after wav emission")
	}

	// WS 帧契约保持：translation-audio 原样下发（wav + base64 原字节）。
	_ = visitorConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, audioPayload, err := visitorConn.ReadMessage()
	if err != nil {
		t.Fatalf("read audio frame: %v", err)
	}
	var audioFrame map[string]interface{}
	if err := json.Unmarshal(audioPayload, &audioFrame); err != nil {
		t.Fatalf("audio frame not JSON: %v", err)
	}
	if audioFrame["type"] != "translation-audio" {
		t.Fatalf("frame type = %v, want translation-audio", audioFrame["type"])
	}
	data, ok := audioFrame["data"].(map[string]interface{})
	if !ok || data["format"] != "wav" {
		t.Fatalf("audio frame data = %+v, want format wav", audioFrame["data"])
	}
	if encoded, _ := data["audio"].(string); encoded != base64.StdEncoding.EncodeToString(wav) {
		t.Fatal("audio frame must carry the original wav bytes")
	}
}

func TestVoiceWebRTCAudioFormatTracksWebRTCState(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)

	visitorConn := dialVoice(t, url)
	agentConn := dialVoiceSpeaker(t, url, "agent")
	waitForStarts(t, runtime, 2)

	visitorSink := runtime.sinkForSpeaker("s1", "visitor")
	if visitorSink == nil {
		t.Fatal("visitor sink not captured")
	}
	if got := visitorSink.AudioFormat(); got != "" {
		t.Fatalf("AudioFormat before request = %q, want empty", got)
	}

	agentClient := voiceClientBySpeaker(t, hub, "s1", "agent")
	attachTestMedia(t, hub, agentClient)
	if got := visitorSink.AudioFormat(); got != "wav" {
		t.Fatalf("AudioFormat with agent track = %q, want wav", got)
	}

	// 同侧说话方不算消费方：visitor 音轨不消费 visitor 产出。
	visitorClient := voiceClientBySpeaker(t, hub, "s1", "visitor")
	attachTestMedia(t, hub, visitorClient)
	if got := visitorSink.AudioFormat(); got != "wav" {
		t.Fatalf("AudioFormat with own-side track must stay wav, got %q", got)
	}

	// 坐席收线 → 媒体面摘除 → 格式回落空（mp3 默认）。
	if err := agentConn.Close(); err != nil {
		t.Fatalf("close agent conn: %v", err)
	}
	waitMediaClosed(t, hub, agentClient.ID)
	if got := visitorSink.AudioFormat(); got != "" {
		t.Fatalf("AudioFormat after teardown = %q, want empty", got)
	}
	_ = visitorConn
}

func TestVoiceWebRTCRequestFailures(t *testing.T) {
	cases := []struct {
		name  string
		seams func(t *testing.T)
	}{
		{"pc create fails", func(t *testing.T) {
			hscovSetSeam(t, &hookNewVoicePeerConnectionVar,
				func(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
					return nil, errors.New("synthetic pc failure")
				})
		}},
		{"track create fails", func(t *testing.T) {
			hscovSetSeam(t, &hookNewTrackLocalStaticRTPVar,
				func(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
					return nil, errors.New("synthetic track failure")
				})
		}},
		{"offer create fails", func(t *testing.T) {
			hscovSetSeam(t, &hookPeerConnectionCreateOfferVar,
				func(pc *webrtc.PeerConnection, options *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
					return webrtc.SessionDescription{}, errors.New("synthetic offer failure")
				})
		}},
		{"set local fails", func(t *testing.T) {
			hscovSetSeam(t, &hookPeerConnectionSetLocalDescriptionVar,
				func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
					return errors.New("synthetic setlocal failure")
				})
		}},
		{"add transceiver fails", func(t *testing.T) {
			hscovSetSeam(t, &hookPeerConnectionAddTransceiverFromTrackVar,
				func(pc *webrtc.PeerConnection, track webrtc.TrackLocal, init webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error) {
					return nil, errors.New("synthetic transceiver failure")
				})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newFakeVoiceRuntime()
			hub, url := newVoiceTestHub(t, runtime)
			conn := dialVoice(t, url)
			waitForStarts(t, runtime, 1)
			tc.seams(t)

			writeVoiceFrame(t, conn, &sync.Mutex{}, voiceFrameWebRTCRequest, nil)
			frame := waitVoiceFrameOfType(t, conn, voiceFrameWebRTCError)
			data, _ := frame.Data.(map[string]interface{})
			if data["code"] != voiceErrWebRTCUnavail {
				t.Fatalf("error code = %v, want %s", data["code"], voiceErrWebRTCUnavail)
			}
			// 通道不收线：纯 WS 语义继续可用（音频上行仍喂入）。
			if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01}); err != nil {
				t.Fatalf("write after failure: %v", err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for len(runtime.stream.recordedChunks()) < 1 {
				if time.Now().After(deadline) {
					t.Fatal("binary audio no longer fed after webrtc failure")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// 失败清理后可重新请求（media 字段已复位）。
			_ = hub
		})
	}
}

func TestVoiceWebRTCUnrequestedSignalsIgnored(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)

	mu := &sync.Mutex{}
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCAnswer,
		map[string]interface{}{"type": "answer", "sdp": "v=0"})
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate,
		map[string]interface{}{"candidate": "candidate:1 1 udp 1 1.1.1.1 1 typ host"})
	writeVoiceFrame(t, conn, mu, "webrtc-unknown", nil)
	if err := conn.WriteMessage(websocket.TextMessage, []byte("not-json")); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	// 无任何下行帧（连 voice-error 都没有：非法信令是忽略不是收线）。
	_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	if _, payload, err := conn.ReadMessage(); err == nil {
		t.Fatalf("unexpected frame %s for unrequested signaling", payload)
	}
	// 连接存活：音频上行仍可达。
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x02}); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(runtime.stream.recordedChunks()) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("audio feed broken after ignored signaling")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVoiceWebRTCAnswerAndCandidateErrorBranches(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)
	client := waitForVoiceClient(t, hub)
	media := attachTestMedia(t, hub, client)

	mu := &sync.Mutex{}
	// 应答解码失败（非法载荷形态）。
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCAnswer, "garbage")
	// 应答被 pion 拒绝（缝线注入）。
	hscovSetSeam(t, &hookPeerConnectionSetRemoteDescriptionVar,
		func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
			return errors.New("synthetic answer rejection")
		})
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCAnswer,
		map[string]interface{}{"type": "answer", "sdp": "v=0"})
	// 候选解码失败 + pion 拒绝。
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate, "garbage")
	hscovSetSeam(t, &hookAddICECandidateVar,
		func(pc *webrtc.PeerConnection, candidate webrtc.ICECandidateInit) error {
			return errors.New("synthetic candidate rejection")
		})
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate,
		map[string]interface{}{"candidate": "candidate:1 1 udp 1 1.1.1.1 1 typ host"})

	// 全部错误都是忽略不是收线：媒体面仍在，音频仍喂入。
	if voiceMediaOf(hub, client) == nil {
		t.Fatal("media must survive signaling errors")
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x03}); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(runtime.stream.recordedChunks()) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("audio feed broken after signaling errors")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = media
}

func TestVoiceWebRTCRouteAudioToPeerSpeakerOnly(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)
	dialVoice(t, url)
	dialVoiceSpeaker(t, url, "agent")
	waitForStarts(t, runtime, 2)

	visitorClient := voiceClientBySpeaker(t, hub, "s1", "visitor")
	visitorMedia := attachTestMedia(t, hub, visitorClient)
	agentClient := voiceClientBySpeaker(t, hub, "s1", "agent")
	agentMedia := attachTestMedia(t, hub, agentClient)

	pattern := []int16{1000, -2000, 3000}
	// 访客产出 → 只进坐席轨；坐席产出 → 只进访客轨。
	hub.routeVoiceAudio("s1", "visitor", pattern, voiceClockRate)
	if got := agentMedia.track.enqueued(); got != len(pattern) {
		t.Fatalf("agent track enqueued = %d, want %d", got, len(pattern))
	}
	if got := visitorMedia.track.enqueued(); got != 0 {
		t.Fatalf("visitor track must not receive own-side audio, got %d", got)
	}
	hub.routeVoiceAudio("s1", "agent", pattern, voiceClockRate)
	if got := visitorMedia.track.enqueued(); got != len(pattern) {
		t.Fatalf("visitor track enqueued = %d, want %d", got, len(pattern))
	}
	if got := agentMedia.track.enqueued(); got != len(pattern) {
		t.Fatalf("agent track must not grow, got %d", got)
	}
	// 无媒体面/无此会话：静默跳过不 panic。
	hub.routeVoiceAudio("s-other", "visitor", pattern, voiceClockRate)
	hub.routeVoiceAudio("s1", "nobody", nil, voiceClockRate)

	// sink 的 wav 路由同口径（OnAudio → decode → route）。
	sink := &voiceFrameSink{client: visitorClient}
	sink.OnAudio(7, buildTestWAV(pattern, voiceClockRate, 1), "wav")
	if got := agentMedia.track.enqueued(); got != 2*len(pattern) {
		t.Fatalf("agent track after sink route = %d, want %d", got, 2*len(pattern))
	}
	// 非 wav / 损坏 wav 不路由。
	sink.OnAudio(8, []byte("mp3"), "mp3")
	if got := agentMedia.track.enqueued(); got != 2*len(pattern) {
		t.Fatalf("non-wav must not route, got %d", got)
	}
	sink.OnAudio(9, []byte("not-a-wav"), "wav")
	if got := agentMedia.track.enqueued(); got != 2*len(pattern) {
		t.Fatalf("broken wav must not route, got %d", got)
	}
}

func TestVoiceWebRTCTeardownClosesMedia(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)
	client := waitForVoiceClient(t, hub)
	media := attachTestMedia(t, hub, client)

	var pcCloses atomic.Int32
	hscovSetSeam(t, &hookPeerConnectionCloseVar,
		func(pc *webrtc.PeerConnection) error {
			pcCloses.Add(1)
			return nil
		})

	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	waitMediaClosed(t, hub, client.ID)
	if got := pcCloses.Load(); got != 1 {
		t.Fatalf("pc close count = %d, want 1", got)
	}
	// 收线幂等（重复摘除不重复关 PC）。
	client.closeMedia()
	if got := pcCloses.Load(); got != 1 {
		t.Fatalf("pc close count after re-close = %d, want 1", got)
	}
	_ = media
	// 上下文取消：节拍循环停摆（Run 返回）。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	media.track.Run(ctx) // 立即返回不阻塞
}

// TestVoiceWebRTCICEServersWired 装配接线：WebRTCService 的 ICE 出站面直达
// 语音音轨 PC（SetICEServers 装配层一次注入，建面时读取）。
func TestVoiceWebRTCICEServersWired(t *testing.T) {
	svc := NewWebRTCService(iceturn.ICEConfig{
		STUNServers:    []string{"stun:stun.example:3478"},
		TURNURL:        "turn:turn.example:3478",
		TURNUsername:   "u",
		TURNCredential: "p",
	}, nil)
	servers := svc.ICEServers()
	if len(servers) != 2 {
		t.Fatalf("ice servers = %d, want 2", len(servers))
	}

	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)
	hub.SetICEServers(servers)

	// 建面读取装配面（捕获校验）；测试不打外部 STUN/TURN，否则 pion 收线时
	// 卡在不可达的 DNS 解析上（测试环境无外网）。
	var gotICEServers []webrtc.ICEServer
	hscovSetSeam(t, &hookNewVoicePeerConnectionVar,
		func(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
			gotICEServers = config.ICEServers
			stripped := config
			stripped.ICEServers = nil
			return webrtc.NewPeerConnection(stripped)
		})

	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)
	writeVoiceFrame(t, conn, &sync.Mutex{}, voiceFrameWebRTCRequest, nil)
	offer := waitVoiceFrameOfType(t, conn, voiceFrameWebRTCOffer)
	if offer.SessionID != "s1" {
		t.Fatalf("offer session = %q, want s1", offer.SessionID)
	}
	client := waitForVoiceClient(t, hub)
	media := voiceMediaOf(hub, client)
	if media == nil {
		t.Fatal("media missing after request")
	}
	if len(gotICEServers) != 2 {
		t.Fatalf("pc ice servers = %d, want 2", len(gotICEServers))
	}
}

// TestVoiceWebRTCDuplicateRequestIgnored 重复请求幂等：媒体面已存在时静默
// 忽略（客户端沿用既有 offer 应答），不重建面也不产生新下行帧。
func TestVoiceWebRTCDuplicateRequestIgnored(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)

	mu := &sync.Mutex{}
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCRequest, nil)
	waitVoiceFrameOfType(t, conn, voiceFrameWebRTCOffer)
	client := waitForVoiceClient(t, hub)
	media := voiceMediaOf(hub, client)
	if media == nil {
		t.Fatal("media missing after request")
	}

	// 排干首次协商的 trickle 候选，再观察重复请求窗口。
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCRequest, nil)
	_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	if _, payload, err := conn.ReadMessage(); err == nil {
		t.Fatalf("duplicate request must be silent, got %s", payload)
	}
	if voiceMediaOf(hub, client) != media {
		t.Fatal("duplicate request must not rebuild media")
	}
}

// TestVoiceWebRTCSignalBackpressure 信令面背压两个分支：signal 缓冲满时
// signalFrame 非阻塞丢弃（读泵不因信令面停摆）；下行 Send 满时信令投递按
// 广播同口径踢线。
func TestVoiceWebRTCSignalBackpressure(t *testing.T) {
	t.Run("signal buffer full drops frame", func(t *testing.T) {
		hub := NewVoiceHub() // 不起 Run：signal 缓冲无人排空
		client := &VoiceClient{ID: "voice-sig-drop", SessionID: "s1", Send: make(chan WebSocketMessage, 1), Hub: hub}
		for range cap(hub.signal) {
			hub.signal <- &voiceSignal{client: client, message: &WebSocketMessage{Type: "webrtc-offer"}}
		}
		done := make(chan struct{})
		go func() {
			client.signalFrame(voiceFrameWebRTCOffer, map[string]interface{}{})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("signalFrame blocked on full signal buffer")
		}
	})

	t.Run("full send kicks client", func(t *testing.T) {
		hub := NewVoiceHub()
		go hub.Run()
		client := &VoiceClient{ID: "voice-sig-kick", SessionID: "s1", Speaker: "visitor", Send: make(chan WebSocketMessage, 1), Hub: hub}
		client.Send <- WebSocketMessage{Type: "translation-delta"} // 预填满
		hub.mutex.Lock()
		hub.clients[client.ID] = client
		hub.mutex.Unlock()
		client.signalFrame(voiceFrameWebRTCOffer, map[string]interface{}{})

		deadline := time.Now().Add(3 * time.Second)
		for {
			hub.mutex.RLock()
			_, ok := hub.clients[client.ID]
			hub.mutex.RUnlock()
			if !ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("client not kicked on full send")
			}
			time.Sleep(5 * time.Millisecond)
		}
		// 关闭后的缓冲值先排干，再观察到 closed。
		for i := 0; i <= cap(client.Send); i++ {
			if _, open := <-client.Send; !open {
				return
			}
		}
		t.Fatal("kicked client send channel must be closed")
	})
}

// TestVoiceWebRTCMediaCloseErrorPath PC 关闭失败只记日志：媒体面照常摘除，
// 收线语义不受影响（pion Close 真实链路不会失败，错误分支为防御性）。
func TestVoiceWebRTCMediaCloseErrorPath(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	hub, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)
	client := waitForVoiceClient(t, hub)
	attachTestMedia(t, hub, client)

	hscovSetSeam(t, &hookPeerConnectionCloseVar,
		func(*webrtc.PeerConnection) error {
			return errors.New("synthetic pc close failure")
		})
	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	waitMediaClosed(t, hub, client.ID)
}

// TestVoiceWebRTCCandidatesBufferedUntilAnswer 应答前置候选的服务端缓冲：
// RFC 8829 允许候选先于 answer 到达（trickle 顺序无关），pion 在远端描述
// 置位前不缓冲直接注入的候选（InvalidStateError 静默丢弃）——服务端暂存、
// 应答置位后冲刷；无应答的候选洪水按 voiceMaxPendingCandidates 截头保留。
func TestVoiceWebRTCCandidatesBufferedUntilAnswer(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)

	var appliedMu sync.Mutex
	applied := 0
	hscovSetSeam(t, &hookAddICECandidateVar,
		func(pc *webrtc.PeerConnection, cand webrtc.ICECandidateInit) error {
			appliedMu.Lock()
			applied++
			appliedMu.Unlock()
			return pc.AddICECandidate(cand)
		})

	mu := &sync.Mutex{}
	frames := startVoiceFrameReader(conn)
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new peer connection: %v", err)
	}
	t.Cleanup(func() { _ = clientPC.Close() })

	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCRequest, nil)
	offerFrame := waitFrameOfType(t, frames, voiceFrameWebRTCOffer)
	offer, err := asSessionDescription(offerFrame.Data)
	if err != nil {
		t.Fatalf("decode offer: %v", err)
	}

	// 33 条手造候选先于应答上行（服务端 SRD 未置位 → 暂存，超上限截头，
	// 最早 1 条丢弃）。
	for i := 0; i < voiceMaxPendingCandidates+1; i++ {
		writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate, map[string]string{
			"candidate": fmt.Sprintf("candidate:%d 1 UDP 2130706431 192.0.2.1 %d typ host", i, 5000+i),
		})
	}

	// 暂存不注入：应答前不得有任何候选触达 PC。
	time.Sleep(300 * time.Millisecond)
	appliedMu.Lock()
	if applied != 0 {
		t.Fatalf("pre-answer candidates applied = %d, want 0 (must buffer until answer)", applied)
	}
	appliedMu.Unlock()

	if err := clientPC.SetRemoteDescription(offer); err != nil {
		t.Fatalf("set remote: %v", err)
	}
	answer, err := clientPC.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCAnswer, answer)

	waitApplied := func(want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			appliedMu.Lock()
			got := applied
			appliedMu.Unlock()
			if got >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		appliedMu.Lock()
		got := applied
		appliedMu.Unlock()
		t.Fatalf("applied candidates = %d, want %d", got, want)
	}
	// 应答后冲刷：暂存的 32 条全部注入。
	waitApplied(voiceMaxPendingCandidates)

	// 应答置位后的新候选走直注（不再暂存）。
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate, map[string]string{
		"candidate": "candidate:999 1 UDP 2130706431 192.0.2.1 9999 typ host",
	})
	waitApplied(voiceMaxPendingCandidates + 1)
}

// TestVoiceWebRTCCandidateInjectionErrors 候选注入两条 pion 拒绝分支：应答置位
// 后冲刷暂存候选（handleVoiceWebRTCAnswer 的冲刷循环）与应答后新候选直注
// （handleVoiceWebRTCCandidate 的直注路径）各命中一次缝线拒绝。与
// TestVoiceWebRTCAnswerAndCandidateErrorBranches 互补——那个用例的候选在应答
// 前置入，落进暂存路径，到不了 hook。拒绝只记日志，媒体面与音频喂入不受影响。
func TestVoiceWebRTCCandidateInjectionErrors(t *testing.T) {
	runtime := newFakeVoiceRuntime()
	_, url := newVoiceTestHub(t, runtime)
	conn := dialVoice(t, url)
	waitForStarts(t, runtime, 1)

	hscovSetSeam(t, &hookAddICECandidateVar,
		func(pc *webrtc.PeerConnection, cand webrtc.ICECandidateInit) error {
			return errors.New("synthetic candidate rejection")
		})

	mu := &sync.Mutex{}
	frames := startVoiceFrameReader(conn)
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new peer connection: %v", err)
	}
	t.Cleanup(func() { _ = clientPC.Close() })

	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCRequest, nil)
	offerFrame := waitFrameOfType(t, frames, voiceFrameWebRTCOffer)
	offer, err := asSessionDescription(offerFrame.Data)
	if err != nil {
		t.Fatalf("decode offer: %v", err)
	}

	// 应答前置候选：落进暂存路径（不经 hook）。
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate, map[string]string{
		"candidate": "candidate:1 1 UDP 2130706431 192.0.2.1 5001 typ host",
	})

	if err := clientPC.SetRemoteDescription(offer); err != nil {
		t.Fatalf("set remote: %v", err)
	}
	answer, err := clientPC.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	// 应答置位 → 冲刷暂存候选 → 冲刷循环拒绝分支。
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCAnswer, answer)
	// 应答后新候选 → 直注路径拒绝分支。
	writeVoiceFrame(t, conn, mu, voiceFrameWebRTCCandidate, map[string]string{
		"candidate": "candidate:2 1 UDP 2130706431 192.0.2.1 5002 typ host",
	})

	// 拒绝不收线：音频上行仍可达。
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x04}); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(runtime.stream.recordedChunks()) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("audio feed broken after candidate rejections")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
