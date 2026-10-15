// voice_media.go 语音翻译 WebRTC 音轨下发媒体层（Phase 3，
// docs/realtime-translation-design.md Phase 3「与 RA-7 SFU-lite 共基建」）：
// 服务端把 TTS 产出的音频写成一条 PCMU RTP 轨，客户端挂 MediaStream 播放——
// 替代 Phase 2 的 WS base64 音频帧（客户端拉取播放）。
//
// 编码选 G.711 μ-law（PCMU/8000/1）而非 Opus：μ-law 为纯查表编码，纯 Go
// 零依赖（Opus 需编码器依赖与 CGO 面）；pion 默认 media engine 与 Chrome/
// Safari 默认音频编解码清单均含 PCMU（payload 0），接收面无条件可达。
//
// 8kHz 是 μ-law 的定义样本率，TTS 产出的 24kHz PCM16 先线性重采样到 8kHz
// 再编码；320 样本/包 = 40ms 节拍，是 WebRTC 音频标准帧长。
//
// RTP 时钟戳按样本数推进（8000Hz × 40ms = 320 样本/包），播放节奏由客户端
// 抖动缓冲吸收节拍漂移——时间戳是解码节奏的唯一依据，不依赖墙钟精度。
package realtime

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	// voiceClockRate μ-law 定义样本率（Hz），即 RTP 时钟速率。
	voiceClockRate = 8000
	// voiceFrameSamples 单包样本数（8000Hz × 40ms），也是 RTP 时钟戳步进。
	voiceFrameSamples = 320
	// voiceFrameDuration 单包节拍（墙钟），40ms 为 WebRTC 音频标准帧长。
	voiceFrameDuration = 40 * time.Millisecond
	// voicePayloadType PCMU 的 IANA 默认 payload type，与 pion 默认 media
	// engine 广播值（a=rtpmap:0 PCMU/8000）一致。
	voicePayloadType = 0
	// voiceMuLawBias μ-law 线性编码偏置（ITU-T G.711 / RFC 3551）。
	voiceMuLawBias = 0x84
)

// voiceMuEnd μ-law 各段线性上限（ITU-T G.711 表，seg 0..7）。
var voiceMuEnd = []int{0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF, 0x3FFF, 0x7FFF}

// voiceNextSSRC 每轨分配唯一 SSRC（RTP 流标识，PC 内唯一即可，不要求可预测）。
var voiceNextSSRC uint32 = 0x10000001

// voiceMuLawTrackCodec 轨的 codec 能力（PCMU/8000/1）；payload type 由 SDP
// 协商决定（pion 默认 media engine 广播 PCMU 为 payload 0）。
var voiceMuLawTrackCodec = webrtc.RTPCodecCapability{
	MimeType:   webrtc.MimeTypePCMU,
	ClockRate: voiceClockRate,
	Channels:  1,
}

// pcm16FromWAV 解析 RIFF/WAVE（PCM16）：遍历 chunk 找 fmt 与 data，多声道按
// 声道平均混成单声道（TTS 产出通常单声道，混音兜底立体声形态）。非 PCM
// （audio format != 1）或非 16bit 采样位宽返回错误。
func pcm16FromWAV(data []byte) (samples []int16, sampleRate int, channels int, err error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, errors.New("wav: not a RIFF WAVE container")
	}
	pos := 12
	var haveFmt, haveData bool
	var fmtChunk, dataChunk []byte
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(data) {
			return nil, 0, 0, errors.New("wav: truncated chunk")
		}
		switch id {
		case "fmt ":
			fmtChunk = data[body : body+size]
			haveFmt = true
		case "data":
			dataChunk = data[body : body+size]
			haveData = true
		}
		pos = body + size + (size & 1) // RIFF chunk 按偶数边界对齐
	}
	if !haveFmt || !haveData {
		return nil, 0, 0, errors.New("wav: missing fmt or data chunk")
	}
	if len(fmtChunk) < 16 {
		return nil, 0, 0, errors.New("wav: short fmt chunk")
	}
	if binary.LittleEndian.Uint16(fmtChunk[0:2]) != 1 {
		return nil, 0, 0, errors.New("wav: unsupported audio format, expect PCM")
	}
	ch := int(binary.LittleEndian.Uint16(fmtChunk[2:4]))
	rate := int(binary.LittleEndian.Uint16(fmtChunk[4:6]))
	bits := int(binary.LittleEndian.Uint16(fmtChunk[14:16]))
	if ch <= 0 || rate <= 0 || bits != 16 {
		return nil, 0, 0, errors.New("wav: unsupported channel/rate/bit-width")
	}
	if len(dataChunk) < 2*ch || (len(dataChunk)-2*ch)%2 != 0 {
		return nil, 0, 0, errors.New("wav: odd sample in data chunk")
	}
	samples = make([]int16, len(dataChunk)/(2*ch))
	for i := range samples {
		base := i * ch * 2
		var sum int
		for c := 0; c < ch; c++ {
			sum += int(int16(binary.LittleEndian.Uint16(dataChunk[base+c*2 : base+c*2+2])))
		}
		samples[i] = int16(sum / ch)
	}
	return samples, rate, ch, nil
}

// resamplePCM16 线性插值重采样到 toHz。同率原样返回，空输入返回空；越界位置
// 取末样本钳位（不补零，零填充留给写帧环节按帧长对齐）。
func resamplePCM16(pcm []int16, fromHz, toHz int) []int16 {
	if fromHz <= 0 || toHz <= 0 || len(pcm) == 0 || fromHz == toHz {
		return pcm
	}
	ratio := float64(fromHz) / float64(toHz)
	outLen := int(float64(len(pcm)) / ratio)
	if outLen <= 0 {
		return nil
	}
	out := make([]int16, outLen)
	last := len(pcm) - 1
	for i := 0; i < outLen; i++ {
		pos := float64(i) * ratio
		i0 := int(pos)
		if i0 > last {
			i0 = last
		}
		frac := pos - float64(i0)
		v := float64(pcm[i0])
		if i0+1 <= last {
			v += float64(pcm[i0+1]-pcm[i0]) * frac
		}
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out
}

// pcm16ToMuLaw 线性 PCM16 → G.711 μ-law（ITU-T G.711 linear2ulaw 参考实现）。
// μ-law 约定 bit 7 = 1 表示正、0 表示负。
func pcm16ToMuLaw(pcm []int16) []byte {
	out := make([]byte, len(pcm))
	for i, s := range pcm {
		v := int(s)
		mask := 0xFF
		if v < 0 {
			v = voiceMuLawBias - v
			mask = 0x7F
		} else {
			v += voiceMuLawBias
		}
		seg := 0
		for seg < len(voiceMuEnd) && v > voiceMuEnd[seg] {
			seg++
		}
		if seg >= len(voiceMuEnd) {
			out[i] = byte(0x7F ^ mask)
			continue
		}
		out[i] = byte(((seg << 4) | ((v >> (seg + 3)) & 0xF)) ^ mask)
	}
	return out
}

// muLawToPCM16 G.711 μ-law → 线性 PCM16（ITU-T G.711 ulaw2linear 参考实现）。
func muLawToPCM16(ml []byte) []int16 {
	out := make([]int16, len(ml))
	for i, b := range ml {
		u := byte(^b)
		t := (int(u&0x0F) << 3) + voiceMuLawBias
		t <<= int((u & 0x70) >> 4)
		var v int
		if u&0x80 != 0 {
			v = voiceMuLawBias - t
		} else {
			v = t - voiceMuLawBias
		}
		out[i] = int16(v)
	}
	return out
}

// packMuLawRTP 组一个 μ-law RTP 包（12 字节头 + 载荷）。
func packMuLawRTP(payload []byte, seq uint16, timestamp, ssrc uint32) []byte {
	buf := make([]byte, 12+len(payload))
	buf[0] = 0x80 // V=2，无 CSRC/扩展/填充
	buf[1] = voicePayloadType
	binary.BigEndian.PutUint16(buf[2:4], seq)
	binary.BigEndian.PutUint32(buf[4:8], timestamp)
	binary.BigEndian.PutUint32(buf[8:12], ssrc)
	copy(buf[12:], payload)
	return buf
}

// VoiceMediaTrack 一条 PCMU RTP 发送轨：入队任意样本率 PCM16（内部重采样到
// 8kHz），Run 按 40ms 节拍取 320 样本编码成 μ-law 写 RTP 包。尾段不足 320
// 样本以静音补齐，避免尾音被截断。
//
// 生命周期：构造后绑定到 PeerConnection（AddTransceiverFromTrack），再启动
// Run（ctx 取消即收线）。未绑定时 Write 为 pion 静默 no-op，包被丢弃。
type VoiceMediaTrack struct {
	track *webrtc.TrackLocalStaticRTP
	ssrc  uint32

	mu      sync.Mutex
	pending []int16 // 8kHz PCM16 待发缓冲

	seq uint16
	ts  uint32

	// frameDuration 测试可注入节拍（生产恒 40ms）。
	frameDuration time.Duration
}

// newVoiceMediaTrack 构造音轨。frameDuration 为 0 时取 40ms。
func newVoiceMediaTrack(frameDuration time.Duration) (*VoiceMediaTrack, error) {
	tr, err := hookNewTrackLocalStaticRTP(voiceMuLawTrackCodec, "voice-translation", "voice")
	if err != nil {
		return nil, err
	}
	frame := frameDuration
	if frame <= 0 {
		frame = voiceFrameDuration
	}
	return &VoiceMediaTrack{
		track:         tr,
		ssrc:          atomic.AddUint32(&voiceNextSSRC, 0x10001),
		frameDuration: frame,
	}, nil
}

// Track 返回底层 pion 轨，供 AddTransceiverFromTrack 绑定。
func (t *VoiceMediaTrack) Track() *webrtc.TrackLocalStaticRTP {
	return t.track
}

// enqueued 已入队待发样本数（测试观测）。
func (t *VoiceMediaTrack) enqueued() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// enqueue 入队一段 PCM16（采样率为 0 时按 voiceClockRate 处理）。
func (t *VoiceMediaTrack) enqueue(samples []int16, sampleRate int) {
	if len(samples) == 0 {
		return
	}
	rate := sampleRate
	if rate <= 0 {
		rate = voiceClockRate
	}
	resampled := resamplePCM16(samples, rate, voiceClockRate)
	if len(resampled) == 0 {
		return
	}
	t.mu.Lock()
	t.pending = append(t.pending, resampled...)
	t.mu.Unlock()
}

// Run 按节拍写包，直到 ctx 取消或轨不可写。
func (t *VoiceMediaTrack) Run(ctx context.Context) {
	timer := time.NewTimer(t.frameDuration)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !t.writeFrame() {
				return // 轨不可写（PC 已收线）
			}
			timer.Reset(t.frameDuration)
		}
	}
}

// writeFrame 取 320 样本（不足则静音补齐）编码写包。空缓冲返回 true（继续等），
// 写失败返回 false（终止节拍）。
func (t *VoiceMediaTrack) writeFrame() bool {
	t.mu.Lock()
	if len(t.pending) == 0 {
		t.mu.Unlock()
		return true
	}
	frame := make([]int16, voiceFrameSamples)
	n := copy(frame, t.pending)
	t.pending = append(t.pending[:0], t.pending[n:]...)
	t.mu.Unlock()

	t.seq++
	t.ts += voiceFrameSamples
	if _, err := hookTrackLocalStaticRTPWrite(t.track, packMuLawRTP(pcm16ToMuLaw(frame), t.seq, t.ts, t.ssrc)); err != nil {
		return false
	}
	return true
}
