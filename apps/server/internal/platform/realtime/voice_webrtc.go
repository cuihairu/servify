// voice_webrtc.go 语音翻译 WebRTC 音轨下发的信令与媒体路由半边（Phase 3，
// docs/realtime-translation-design.md Phase 3「与 RA-7 SFU-lite 共基建」）。
//
// 形态：客户端在语音 WS 上发 webrtc-request，服务端建 PC + sendonly 音轨并
// 发起 offer（服务端是媒体发送方，与会话 WS 的客户端发起 offer 相反方向，
// 帧词表同 RA-7：webrtc-offer / webrtc-answer / webrtc-candidate）。译文
// 合成音（wav）解回 PCM16 喂进对侧说话方客户端的音轨——纯 WS 客户端不受
// 影响：不请求即不建连，Phase 2 帧契约零变更。
//
// 并发不变量（与 voice_hub.go 同源）：pion 回调（ICE 候选、连接态）与
// readPump 都不直接触碰 Send，信令帧经 hub.signal 命令通道由 Run 事件循环
// 单点投递。media 字段写入只发生在 readPump，路由/格式协商读方持 hub 锁。
package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/sirupsen/logrus"
)

// 语音通道控制帧词表（PROTOCOL §9）。
const (
	voiceFrameWebRTCRequest   = "webrtc-request"
	voiceFrameWebRTCOffer     = "webrtc-offer"
	voiceFrameWebRTCAnswer    = "webrtc-answer"
	voiceFrameWebRTCCandidate = "webrtc-candidate"
	voiceFrameWebRTCError     = "webrtc-error"

	voiceErrWebRTCUnavail = "webrtc_unavailable" // 音轨面建连失败（不收线，WS 通道继续可用）

	// voiceMaxPendingCandidates 应答前置候选暂存上限：正常 ICE 收敛远低于此，
	// 只挡无应答连接的候选洪水（超限截头保留最新）。
	voiceMaxPendingCandidates = 32
)

// voiceClientMedia 单连接的 WebRTC 音轨面：一条 sendonly PCMU 轨 + PC 生命
// 周期。由 readPump 串行触达（request/收线），pion 回调只经 hub.signal 投递
// 或 runOnce 启动节拍循环。
type voiceClientMedia struct {
	pc    *webrtc.PeerConnection
	track *VoiceMediaTrack

	// pendingCandidates 应答前置候选暂存：RFC 8829 允许候选先于 answer 到达
	// （trickle 顺序无关），pion 的 AddICECandidate 在远端描述置位前直接报
	// InvalidStateError（不缓冲），故先按帧序暂存、应答置位后冲刷。仅
	// readPump 串行触达（answer/candidate 同连接同泵），无需加锁。
	pendingCandidates []webrtc.ICECandidateInit

	// runOnce 连接建立后启动节拍循环（只跑一次；断线重连不重启——轨的
	// seq/ts 连续性由 VoiceMediaTrack 自身持有，重启只会重复计拍）。
	runOnce sync.Once
	// closeOnce 收线幂等（request 失败清理与连接收线双入口）。
	closeOnce sync.Once
	cancel    context.CancelFunc
}

// newVoiceClientMedia 建 PC + 音轨并挂 pion 回调。失败时已建资源就地回收，
// 调用方无需再收线。iceServers 由调用方在锁外取好（本函数不触 hub 锁）。
func newVoiceClientMedia(iceServers []webrtc.ICEServer, c *VoiceClient) (*voiceClientMedia, error) {
	pc, err := hookNewVoicePeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		return nil, err
	}
	track, err := newVoiceMediaTrack(0)
	if err != nil {
		_ = pc.Close() // PC 就地回收，失败不影响返回错误
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &voiceClientMedia{pc: pc, track: track, cancel: cancel}

	// 服务端候选定向下发本连接（json.Marshal 的载荷是可序列化标量，不会失败）。
	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		candidateData, _ := json.Marshal(cand.ToJSON())
		c.signalFrame(voiceFrameWebRTCCandidate, map[string]interface{}{
			"candidate": json.RawMessage(candidateData),
		})
	})

	// 连接建立即启动节拍循环写包；其余状态无需动作（收线统一走 cancel）。
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state != webrtc.PeerConnectionStateConnected {
			return
		}
		m.runOnce.Do(func() { go m.track.Run(ctx) })
	})
	return m, nil
}

// close 收线媒体面（幂等）：先停节拍循环（cancel），再关 PC（pion Close
// 会终止 ICE/DTLS 并解除轨绑定）。
func (m *voiceClientMedia) close() {
	m.closeOnce.Do(func() {
		m.cancel()
		if err := hookPeerConnectionClose(m.pc); err != nil {
			logrus.Warnf("Voice webrtc pc close failed: %v", err)
		}
	})
}

// closeMedia 摘除并收线本连接的音轨面。hub 锁只用于与路由/格式协商读方
// 互斥摘除字段；PC 关闭（pion 内部清理）在锁外执行。
func (c *VoiceClient) closeMedia() {
	c.Hub.mutex.Lock()
	media := c.media
	c.media = nil
	c.Hub.mutex.Unlock()
	if media != nil {
		media.close()
	}
}

// handleVoiceWebRTCRequest 音轨请求：幂等（重复请求忽略——媒体面已存在，
// 客户端沿用既有 offer 应答即可）。建面成功即发起一次协商：加轨 → offer →
// 置本地描述 → 定向下发。任一步失败回收整面并回 webrtc-error（通道不收线，
// 纯 WS 语义继续可用）。
func (c *VoiceClient) handleVoiceWebRTCRequest() {
	hub := c.Hub
	iceServers := hub.currentICEServers() // 锁外取：建面持锁期间不再触 hub 锁
	hub.mutex.Lock()
	if c.media != nil {
		hub.mutex.Unlock()
		logrus.Warnf("Duplicate webrtc-request from voice client %s ignored", c.ID)
		return
	}
	media, err := newVoiceClientMedia(iceServers, c)
	if err != nil {
		hub.mutex.Unlock()
		logrus.Errorf("Voice webrtc media setup failed (session=%s): %v", c.SessionID, err)
		c.voiceWebRTCError("webrtc media unavailable")
		return
	}
	c.media = media
	hub.mutex.Unlock()

	if _, err := hookPeerConnectionAddTransceiverFromTrack(media.pc, media.track.Track(), webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	}); err != nil {
		c.failVoiceMedia(media, "add track: "+err.Error())
		return
	}
	offer, err := hookPeerConnectionCreateOffer(media.pc, nil)
	if err != nil {
		c.failVoiceMedia(media, "create offer: "+err.Error())
		return
	}
	if err := hookPeerConnectionSetLocalDescription(media.pc, offer); err != nil {
		c.failVoiceMedia(media, "set local description: "+err.Error())
		return
	}
	c.signalFrame(voiceFrameWebRTCOffer, offer)
	logrus.Infof("Voice webrtc offer sent (session=%s speaker=%s client=%s)", c.SessionID, c.Speaker, c.ID)
}

// failVoiceMedia 建面/协商失败统一出口：回收整面 + 回 webrtc-error（真实
// 错误细节只进服务端日志，不回客户端）。
func (c *VoiceClient) failVoiceMedia(media *voiceClientMedia, cause string) {
	logrus.Errorf("Voice webrtc negotiation failed (session=%s): %s", c.SessionID, cause)
	media.close()
	c.closeMedia() // 摘除字段，回到"无媒体"形态可重新请求
	c.voiceWebRTCError("webrtc media unavailable")
}

// handleVoiceWebRTCAnswer 客户端应答：置远端描述后 ICE 自动推进，并冲刷
// 应答前置候选（远端描述未置位期间暂存的入站候选）。
func (c *VoiceClient) handleVoiceWebRTCAnswer(data interface{}) {
	c.Hub.mutex.RLock()
	media := c.media
	c.Hub.mutex.RUnlock()
	if media == nil {
		logrus.Warnf("Voice webrtc answer without media (client=%s) ignored", c.ID)
		return
	}
	answer, err := asSessionDescription(data)
	if err != nil {
		logrus.Warnf("Voice webrtc answer decode failed (client=%s): %v", c.ID, err)
		return
	}
	if err := hookPeerConnectionSetRemoteDescription(media.pc, answer); err != nil {
		logrus.Warnf("Voice webrtc answer rejected (client=%s): %v", c.ID, err)
	}
	for _, pending := range media.takePendingCandidates() {
		if err := hookAddICECandidate(media.pc, pending); err != nil {
			logrus.Warnf("Voice webrtc pending candidate rejected (client=%s): %v", c.ID, err)
		}
	}
}

// handleVoiceWebRTCCandidate 客户端候选注入：应答未置远端描述时暂存
// （pion 无候选缓冲，直接注入会被 InvalidStateError 丢弃）。
func (c *VoiceClient) handleVoiceWebRTCCandidate(data interface{}) {
	c.Hub.mutex.RLock()
	media := c.media
	c.Hub.mutex.RUnlock()
	if media == nil {
		logrus.Warnf("Voice webrtc candidate without media (client=%s) ignored", c.ID)
		return
	}
	candidate, err := asICECandidate(data)
	if err != nil {
		logrus.Warnf("Voice webrtc candidate decode failed (client=%s): %v", c.ID, err)
		return
	}
	if media.pc.RemoteDescription() == nil {
		if dropped := media.queuePendingCandidate(candidate); dropped > 0 {
			logrus.Warnf("Voice webrtc pending candidates capped at %d (client=%s, dropped %d)",
				voiceMaxPendingCandidates, c.ID, dropped)
		}
		return
	}
	if err := hookAddICECandidate(media.pc, candidate); err != nil {
		logrus.Warnf("Voice webrtc candidate rejected (client=%s): %v", c.ID, err)
	}
}

// queuePendingCandidate 暂存应答前置候选，超过上限截头保留最新并返回丢弃
// 条数（防无应答连接的候选洪水无界增长）。
func (m *voiceClientMedia) queuePendingCandidate(cand webrtc.ICECandidateInit) int {
	m.pendingCandidates = append(m.pendingCandidates, cand)
	if len(m.pendingCandidates) <= voiceMaxPendingCandidates {
		return 0
	}
	dropped := len(m.pendingCandidates) - voiceMaxPendingCandidates
	m.pendingCandidates = m.pendingCandidates[len(m.pendingCandidates)-voiceMaxPendingCandidates:]
	return dropped
}

// takePendingCandidates 取空暂存候选（应答置位后冲刷用）。
func (m *voiceClientMedia) takePendingCandidates() []webrtc.ICECandidateInit {
	pending := m.pendingCandidates
	m.pendingCandidates = nil
	return pending
}

// signalFrame 定向信令帧投递（非阻塞：信号通道满只丢帧记日志，客户端以
// 应答超时自愈；readPump 绝不因信令面阻塞音频上行）。
func (c *VoiceClient) signalFrame(frameType string, data interface{}) {
	select {
	case c.Hub.signal <- &voiceSignal{client: c, message: &WebSocketMessage{
		Type:      frameType,
		Data:      data,
		SessionID: c.SessionID,
		Timestamp: time.Now(),
	}}:
	default:
		logrus.Warnf("Voice signal frame %s dropped (client=%s signal buffer full)", frameType, c.ID)
	}
}

// voiceWebRTCError 音轨面失败回执（不收线：WS 通道与音频上行继续可用）。
func (c *VoiceClient) voiceWebRTCError(message string) {
	c.signalFrame(voiceFrameWebRTCError, map[string]interface{}{
		"code":    voiceErrWebRTCUnavail,
		"message": message,
	})
}

// hubHasWebRTCAudio 会话内是否存在"会消费 speaker 管线产出"的 WebRTC 客户端
// （对侧说话方请求过音轨）。格式协商判据：有则 TTS 按 wav 合成。
func (h *VoiceHub) hubHasWebRTCAudio(sessionID, speaker string) bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	for _, client := range h.clients {
		if client.SessionID != sessionID || client.Speaker == speaker || client.media == nil {
			continue
		}
		return true
	}
	return false
}

// routeVoiceAudio 把 speaker 管线产出的译文音频投进对侧说话方客户端的音轨
// （与 WS 广播同方向语义：产出归对侧消费）。非阻塞：enqueue 只入缓冲。
func (h *VoiceHub) routeVoiceAudio(sessionID, speaker string, samples []int16, rate int) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	for _, client := range h.clients {
		if client.SessionID != sessionID || client.Speaker == speaker || client.media == nil {
			continue
		}
		client.media.track.enqueue(samples, rate)
	}
}
