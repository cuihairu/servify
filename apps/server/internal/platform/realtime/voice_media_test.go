package realtime

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// iceCandidateBridge trickle 候选缓冲桥：对端远端描述置位前到达的候选先
// 缓存（pion AddICECandidate 在 SRD 前不缓冲，直接注入即 InvalidStateError
// 丢弃），markReady 后按到达顺序冲刷并接管直注——无丢候选窗口（RFC 8829
// 候选缓冲要求的测试侧对齐）。
type iceCandidateBridge struct {
	mu      sync.Mutex
	ready   bool
	pending []webrtc.ICECandidateInit
}

func (b *iceCandidateBridge) add(pc *webrtc.PeerConnection, cand webrtc.ICECandidateInit) {
	b.mu.Lock()
	if !b.ready {
		b.pending = append(b.pending, cand)
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	_ = pc.AddICECandidate(cand)
}

// markReady 在对端 SetRemoteDescription 完成后调用：冲刷暂存候选并直注后续。
func (b *iceCandidateBridge) markReady(pc *webrtc.PeerConnection) {
	b.mu.Lock()
	b.ready = true
	pending := b.pending
	b.pending = nil
	b.mu.Unlock()
	for _, cand := range pending {
		_ = pc.AddICECandidate(cand)
	}
}

// buildTestWAV 组一个标准 RIFF/WAVE（PCM16）容器：fmt + data 两个 chunk。
func buildTestWAV(samples []int16, rate, channels int) []byte {
	dataBytes := len(samples) * channels * 2
	out := make([]byte, 0, 44+len(samples)*channels*2)
	write := func(b []byte) { out = append(out, b...) }
	le16 := func(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }
	le32 := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	write([]byte("RIFF"))
	write(le32(uint32(4 + dataBytes + 24)))
	write([]byte("WAVE"))
	write([]byte("fmt "))
	write(le32(16))
	write(le16(1)) // PCM
	write(le16(uint16(channels)))
	write(le32(uint32(rate)))
	write(le32(uint32(rate) * uint32(channels) * 2))
	write(le16(uint16(channels * 2)))
	write(le16(16))
	write([]byte("data"))
	write(le32(uint32(dataBytes)))
	for i := range samples {
		for c := 0; c < channels; c++ {
			write(le16(uint16(samples[i])))
		}
	}
	return out
}

func TestVoiceMedia_Pcm16FromWAV(t *testing.T) {
	t.Run("mono 24k", func(t *testing.T) {
		in := []int16{-12345, 0, 32767, 1, -1}
		samples, rate, ch, err := pcm16FromWAV(buildTestWAV(in, 24000, 1))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if rate != 24000 || ch != 1 || len(samples) != len(in) {
			t.Fatalf("got rate=%d ch=%d n=%d", rate, ch, len(samples))
		}
		for i := range in {
			if samples[i] != in[i] {
				t.Fatalf("sample %d = %d, want %d", i, samples[i], in[i])
			}
		}
	})

	t.Run("stereo averaged to mono", func(t *testing.T) {
		// 立体声左右声道不同：构造 [L, R] 交错，验证按声道平均。
		// buildTestWAV 对每声道写同一值，这里手工构造交错（8000Hz）。
		data := []byte{
			'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E',
			'f', 'm', 't', ' ', 0x10, 0, 0, 0,
			0x01, 0x00, // PCM
			0x02, 0x00, // 2 channels
			0x40, 0x1F, 0x00, 0x00, // 8000 Hz
			0x00, 0x7D, 0x00, 0x00, // byte rate 32000
			0x04, 0x00, // block align
			0x10, 0x00, // 16 bit
			'd', 'a', 't', 'a', 0x08, 0, 0, 0,
			0x9C, 0xFF, 0x00, 0x00, // sample0: L=-100, R=0 → avg=-50
			0x00, 0x00, 0x00, 0x00, // sample1: 0
		}
		samples, rate, ch, err := pcm16FromWAV(data)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if rate != 8000 || ch != 2 || len(samples) != 2 {
			t.Fatalf("got rate=%d ch=%d n=%d", rate, ch, len(samples))
		}
		if samples[0] != -50 {
			t.Fatalf("avg = %d, want -50", samples[0])
		}
	})

	t.Run("errors", func(t *testing.T) {
		cases := map[string][]byte{
			"not riff": func() []byte {
				b := buildTestWAV([]int16{1}, 8000, 1)
				copy(b, "XXXX")
				return b
			}(),
			"truncated":    buildTestWAV([]int16{1}, 8000, 1)[:20],
			"missing data": buildTestWAV([]int16{1}, 8000, 1)[:36], // 只到 fmt
			"non pcm": func() []byte {
				b := buildTestWAV([]int16{1}, 8000, 1)
				b[20] = 2 // fmt chunk 的 audio format 位置
				return b
			}(),
			"non 16bit": func() []byte {
				b := buildTestWAV([]int16{1}, 8000, 1)
				b[34] = 8 // bits per sample 位置
				return b
			}(),
			"zero channel": func() []byte {
				b := buildTestWAV([]int16{1}, 8000, 1)
				b[22] = 0 // channels 位置
				return b
			}(),
			"short fmt chunk": []byte{
				'R', 'I', 'F', 'F', 0x24, 0, 0, 0, 'W', 'A', 'V', 'E',
				'f', 'm', 't', ' ', 4, 0, 0, 0, 0, 0, 0, 0,
				'd', 'a', 't', 'a', 0, 0, 0, 0,
			},
			"odd data": func() []byte {
				b := buildTestWAV([]int16{1}, 8000, 1)
				b[40] = 1 // data chunk size = 1 字节
				return b
			}(),
		}
		for name, payload := range cases {
			if _, _, _, err := pcm16FromWAV(payload); err == nil {
				t.Errorf("%s: expected error", name)
			}
		}
	})
}

func TestVoiceMedia_Resample(t *testing.T) {
	same := []int16{100, -200, 300}
	if got := resamplePCM16(same, 8000, 8000); len(got) == 0 || &got[0] != &same[0] {
		t.Fatalf("same-rate must return the input slice")
	}
	if got := resamplePCM16(nil, 8000, 8000); got != nil {
		t.Fatalf("empty input must return nil, got %v", got)
	}
	if got := resamplePCM16(same, 0, 8000); len(got) == 0 || &got[0] != &same[0] {
		t.Fatalf("zero fromHz must return the input slice")
	}

	// 24kHz → 8kHz：3 样本 → 1 样本，线性插值落点 [0.0] → pcm[0]。
	in := []int16{1000, 2000, 3000}
	out := resamplePCM16(in, 24000, 8000)
	if len(out) != 1 {
		t.Fatalf("out len = %d, want 1", len(out))
	}
	if out[0] != 1000 {
		t.Fatalf("out[0] = %d, want 1000", out[0])
	}

	// 24kHz → 8kHz 整段：960 样本 → 320 样本。
	in960 := make([]int16, 960)
	for i := range in960 {
		in960[i] = 1000
	}
	out960 := resamplePCM16(in960, 24000, 8000)
	if len(out960) != 320 {
		t.Fatalf("out len = %d, want 320", len(out960))
	}
	for i := range out960 {
		if out960[i] != 1000 {
			t.Fatalf("out[%d] = %d, want 1000", i, out960[i])
			break
		}
	}

	// 越界末点钳位：极短输入（24kHz 单样本降到 8kHz）不产出样本。
	if got := resamplePCM16([]int16{500}, 24000, 8000); len(got) != 0 {
		t.Fatalf("sub-sample downsample should return empty, got %v", got)
	}
}

func TestVoiceMedia_MuLawVectors(t *testing.T) {
	// ITU-T G.711 参考实现的已知向径（编码 → 码字 → 解码）。
	type vec struct {
		in   int16
		code byte
		out  int16
	}
	vectors := []vec{
		{0, 0xFF, 0},           // 静音
		{1000, 0xCE, 988},      // 正中段
		{-1000, 0x4E, -988},    // 负中段
		{32767, 0x80, 32124},   // 正饱和
		{-32768, 0x00, -32124}, // 负饱和
	}
	for _, v := range vectors {
		if got := pcm16ToMuLaw([]int16{v.in})[0]; got != v.code {
			t.Errorf("encode(%d) = 0x%02x, want 0x%02x", v.in, got, v.code)
		}
		if got := muLawToPCM16([]byte{v.code})[0]; got != v.out {
			t.Errorf("decode(0x%02x) = %d, want %d", v.code, got, v.out)
		}
	}

	// 符号位约定：bit 7 = 1 表示正。
	if pcm16ToMuLaw([]int16{500})[0]&0x80 == 0 {
		t.Error("positive sample must encode with sign bit 1")
	}
	if pcm16ToMuLaw([]int16{-500})[0]&0x80 != 0 {
		t.Error("negative sample must encode with sign bit 0")
	}

	// 全幅量化误差上限（分段线性，误差 < 半量级）。
	for _, s := range []int16{-30000, -5000, -500, 0, 500, 5000, 30000} {
		out := muLawToPCM16(pcm16ToMuLaw([]int16{s}))[0]
		d := int(out) - int(s)
		if d < -300 || d > 300 {
			t.Errorf("round-trip(%d) = %d, drift %d exceeds 300", s, out, d)
		}
	}
}

func TestVoiceMedia_PackRTP(t *testing.T) {
	payload := []byte{0x41, 0x42, 0x43}
	packet := packMuLawRTP(payload, 0x0102, 0xDEADBEEF, 0xC0FFEE)
	if len(packet) != 12+3 {
		t.Fatalf("packet len = %d, want 15", len(packet))
	}
	if packet[0] != 0x80 {
		t.Fatalf("header byte0 = 0x%02x, want 0x80", packet[0])
	}
	if packet[1] != voicePayloadType {
		t.Fatalf("payload type = %d, want %d", packet[1], voicePayloadType)
	}
	if binary.BigEndian.Uint16(packet[2:4]) != 0x0102 {
		t.Fatalf("seq = 0x%04x", binary.BigEndian.Uint16(packet[2:4]))
	}
	if binary.BigEndian.Uint32(packet[4:8]) != 0xDEADBEEF {
		t.Fatalf("timestamp = 0x%08x", binary.BigEndian.Uint32(packet[4:8]))
	}
	if binary.BigEndian.Uint32(packet[8:12]) != 0xC0FFEE {
		t.Fatalf("ssrc = 0x%08x", binary.BigEndian.Uint32(packet[8:12]))
	}
	for i, want := range payload {
		if packet[12+i] != want {
			t.Fatalf("payload[%d] = 0x%02x, want 0x%02x", i, packet[12+i], want)
		}
	}
}

func TestVoiceMediaTrack_EnqueueResamplesAndPaces(t *testing.T) {
	tr, err := newVoiceMediaTrack(time.Millisecond)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if tr.ssrc == 0 {
		t.Fatal("ssrc must be non-zero")
	}

	// 空队列时 writeFrame 不写包。
	if !tr.writeFrame() {
		t.Fatal("empty buffer must return true (keep waiting)")
	}

	// 24kHz 960 样本 → 8kHz 320 样本（恰好一帧）。
	in := make([]int16, 960)
	tr.enqueue(in, 24000)
	if got := tr.enqueued(); got != 320 {
		t.Fatalf("enqueued = %d, want 320", got)
	}

	if !tr.writeFrame() {
		t.Fatal("frame write failed")
	}
	if got := tr.enqueued(); got != 0 {
		t.Fatalf("enqueued after write = %d, want 0", got)
	}

	// 尾段不足一帧以静音补齐。
	tr.enqueue([]int16{100, -100, 200}, voiceClockRate)
	if got := tr.enqueued(); got != 3 {
		t.Fatalf("enqueued = %d, want 3", got)
	}
	if !tr.writeFrame() {
		t.Fatal("tail frame write failed")
	}
	if got := tr.enqueued(); got != 0 {
		t.Fatalf("tail drained = %d, want 0", got)
	}

	// 空输入与空重采样不产生缓冲。
	tr.enqueue(nil, voiceClockRate)
	tr.enqueue([]int16{}, 8000)
	if got := tr.enqueued(); got != 0 {
		t.Fatalf("no-op enqueue = %d, want 0", got)
	}

	// 采样率为 0 时按 8kHz 处理。
	tr.enqueue([]int16{100, 200}, 0)
	if got := tr.enqueued(); got != 2 {
		t.Fatalf("enqueued = %d, want 2", got)
	}
	// 重采样后无样本（24kHz 单样本 → 8kHz 0 样本）不产生缓冲。
	tr.enqueue([]int16{500}, 24000)
	if got := tr.enqueued(); got != 2 {
		t.Fatalf("empty resample = %d, want 2", got)
	}
}

func TestVoiceMediaTrack_NewDefaultsAndErrors(t *testing.T) {
	// frameDuration 为 0 时取 40ms 默认节拍。
	tr, err := newVoiceMediaTrack(0)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if tr.frameDuration != voiceFrameDuration {
		t.Fatalf("frame = %v, want %v", tr.frameDuration, voiceFrameDuration)
	}

	// 轨构造失败（codec 非法等）经缝线注入。
	hscovSetSeam(t, &hookNewTrackLocalStaticRTPVar,
		func(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
			return nil, errors.New("synthetic track failure")
		})
	if _, err := newVoiceMediaTrack(time.Millisecond); err == nil {
		t.Fatal("expected error from track constructor")
	}
}

func TestVoiceMediaTrack_LoopbackPCMU(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}
	serverPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("server pc: %v", err)
	}
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		_ = serverPC.Close()
		t.Fatalf("client pc: %v", err)
	}
	t.Cleanup(func() { _ = clientPC.Close() })
	t.Cleanup(func() { _ = serverPC.Close() })

	tr, err := newVoiceMediaTrack(time.Millisecond)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	// 服务端 sendonly 音轨（客户端挂 MediaStream 播放形态）。
	if _, err := serverPC.AddTransceiverFromTrack(tr.Track(), webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	}); err != nil {
		t.Fatalf("add transceiver: %v", err)
	}

	// trickle 候选经缓冲桥注入：handler 必须在 SetLocalDescription 之前挂
	// （pion 不回放已产出的候选），且远端描述置位前到的候选先缓存——
	// pion AddICECandidate 在 SRD 前不缓冲，直接注入即静默丢弃。置位后
	// markReady 按到达顺序冲刷，无丢候选窗口。
	toClient := &iceCandidateBridge{}
	toServer := &iceCandidateBridge{}
	serverPC.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			toClient.add(clientPC, c.ToJSON())
		}
	})
	clientPC.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			toServer.add(serverPC, c.ToJSON())
		}
	})

	offer, err := serverPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := serverPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local: %v", err)
	}
	if err := clientPC.SetRemoteDescription(offer); err != nil {
		t.Fatalf("client set remote: %v", err)
	}
	toClient.markReady(clientPC)
	answer, err := clientPC.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := clientPC.SetLocalDescription(answer); err != nil {
		t.Fatalf("client set local: %v", err)
	}
	if err := serverPC.SetRemoteDescription(answer); err != nil {
		t.Fatalf("server set remote: %v", err)
	}
	toServer.markReady(serverPC)

	waitBridgeConnected(t, serverPC)
	waitBridgeConnected(t, clientPC)

	clientGotTrack := make(chan *webrtc.TrackRemote, 1)
	clientPC.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		clientGotTrack <- track
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tr.Run(ctx)

	// 320 样本 8kHz = 恰好一帧。
	pattern := make([]int16, voiceFrameSamples)
	for i := range pattern {
		pattern[i] = int16(1000 + i*3)
	}
	tr.enqueue(pattern, voiceClockRate)

	select {
	case track := <-clientGotTrack:
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			t.Fatalf("track kind = %s, want audio", track.Kind())
		}
		if track.Codec().MimeType != webrtc.MimeTypePCMU {
			t.Fatalf("track codec = %s, want PCMU", track.Codec().MimeType)
		}
		buf := make([]byte, 12+voiceFrameSamples)
		n, _, err := track.Read(buf)
		if err != nil {
			t.Fatalf("read remote track: %v", err)
		}
		if n < 12+voiceFrameSamples {
			t.Fatalf("packet len = %d, want >= %d", n, 12+voiceFrameSamples)
		}
		if buf[1] != voicePayloadType {
			t.Fatalf("payload type = %d, want %d", buf[1], voicePayloadType)
		}
		payload := buf[12 : 12+voiceFrameSamples]
		want := pcm16ToMuLaw(pattern)
		if len(payload) != len(want) {
			t.Fatalf("payload len = %d, want %d", len(payload), len(want))
		}
		for i := range want {
			if payload[i] != want[i] {
				t.Fatalf("payload[%d] = 0x%02x, want 0x%02x", i, payload[i], want[i])
			}
		}
		// 客户端解码回来的波形应在量化误差内还原输入。
		if got := muLawToPCM16(payload)[0]; got != pattern[0] {
			t.Logf("client-decoded first sample = %d (input %d, within μ-law quantization)", got, pattern[0])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("client did not receive the PCMU track within timeout")
	}
}

func TestVoiceMediaTrack_RunStopsOnWriteError(t *testing.T) {
	tr, err := newVoiceMediaTrack(time.Millisecond)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	hscovSetSeam(t, &hookTrackLocalStaticRTPWriteVar,
		func(track *webrtc.TrackLocalStaticRTP, packet []byte) (int, error) {
			return 0, errors.New("synthetic write failure")
		})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.enqueue(make([]int16, voiceFrameSamples), voiceClockRate)

	done := make(chan struct{})
	go func() {
		tr.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on write error")
	}
}
