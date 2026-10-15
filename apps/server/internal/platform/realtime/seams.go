package realtime

// 本文件聚合仅供测试注入的包级 seam。每个变量的默认值都保持生产行为，
// 生产代码不得在运行时改写；测试通过替换变量来驱动错误分支
//（pion API 失败）。auth 模块的 seam 已随模块迁移至
// internal/modules/auth/application/seams.go。
//
// 形态：hookX 是调用点（RLock），hookXVar 是测试替换槽（Write Lock）。
// 收线 goroutine（读泵 defer、音轨节拍循环）可能在测试结束后仍在触碰 seam，
// 若直接读写裸变量，-race 会报跨测试数据竞态；hookMu 使两侧同步。
import (
	"sync"

	"github.com/pion/webrtc/v4"
)

var hookMu sync.RWMutex

var (
	hookPeerConnectionCreateAnswerVar = func(pc *webrtc.PeerConnection, options *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
		return pc.CreateAnswer(options)
	}
	hookPeerConnectionCreateAnswer = func(pc *webrtc.PeerConnection, options *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookPeerConnectionCreateAnswerVar(pc, options)
	}

	hookPeerConnectionCreateOfferVar = func(pc *webrtc.PeerConnection, options *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
		return pc.CreateOffer(options)
	}
	hookPeerConnectionCreateOffer = func(pc *webrtc.PeerConnection, options *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookPeerConnectionCreateOfferVar(pc, options)
	}

	hookPeerConnectionSetLocalDescriptionVar = func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
		return pc.SetLocalDescription(desc)
	}
	hookPeerConnectionSetLocalDescription = func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookPeerConnectionSetLocalDescriptionVar(pc, desc)
	}

	hookPeerConnectionCloseVar = func(pc *webrtc.PeerConnection) error {
		return pc.Close()
	}
	hookPeerConnectionClose = func(pc *webrtc.PeerConnection) error {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookPeerConnectionCloseVar(pc)
	}

	// hookNewTrackLocalStaticRTPVar 是媒体桥接的本地轨构造缝线：codec 能力非法
	// 等形态在真实链路里由发布端 SDP 决定，无法稳定构造，注入错误以覆盖
	// 转发分支的失败处理。
	hookNewTrackLocalStaticRTPVar = func(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
		return webrtc.NewTrackLocalStaticRTP(codec, id, streamID)
	}
	hookNewTrackLocalStaticRTP = func(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookNewTrackLocalStaticRTPVar(codec, id, streamID)
	}

	// hookTrackLocalStaticRTPWriteVar 是语音音轨写包缝线：DTLS 链路破损时 pion
	// 才会在写包返回错误（轨关闭场景下 pion 反而静默 no-op），注入错误覆盖
	// 节拍循环的收线分支。
	hookTrackLocalStaticRTPWriteVar = func(track *webrtc.TrackLocalStaticRTP, packet []byte) (int, error) {
		return track.Write(packet)
	}
	hookTrackLocalStaticRTPWrite = func(track *webrtc.TrackLocalStaticRTP, packet []byte) (int, error) {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookTrackLocalStaticRTPWriteVar(track, packet)
	}

	// hookNewVoicePeerConnectionVar 是语音音轨 PC 构造缝线：装配形态的 ICE 配置
	// 非法在真实链路不可稳定构造，注入错误覆盖信令面建连失败分支。
	hookNewVoicePeerConnectionVar = func(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
		return webrtc.NewPeerConnection(config)
	}
	hookNewVoicePeerConnection = func(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookNewVoicePeerConnectionVar(config)
	}

	hookPeerConnectionSetRemoteDescriptionVar = func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
		return pc.SetRemoteDescription(desc)
	}
	hookPeerConnectionSetRemoteDescription = func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookPeerConnectionSetRemoteDescriptionVar(pc, desc)
	}

	// hookPeerConnectionAddTransceiverFromTrackVar 是语音音轨挂轨缝线：对合法
	// 本地轨该操作真实链路不会失败，注入错误覆盖协商面挂轨失败分支。
	hookPeerConnectionAddTransceiverFromTrackVar = func(pc *webrtc.PeerConnection, track webrtc.TrackLocal, init webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error) {
		return pc.AddTransceiverFromTrack(track, init)
	}
	hookPeerConnectionAddTransceiverFromTrack = func(pc *webrtc.PeerConnection, track webrtc.TrackLocal, init webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error) {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookPeerConnectionAddTransceiverFromTrackVar(pc, track, init)
	}

	// hookAddICECandidateVar 是候选注入缝线：空候选形态 pion 内部校验拒绝，
	// 注入确定性错误覆盖候选投递失败分支。
	hookAddICECandidateVar = func(pc *webrtc.PeerConnection, candidate webrtc.ICECandidateInit) error {
		return pc.AddICECandidate(candidate)
	}
	hookAddICECandidate = func(pc *webrtc.PeerConnection, candidate webrtc.ICECandidateInit) error {
		hookMu.RLock()
		defer hookMu.RUnlock()
		return hookAddICECandidateVar(pc, candidate)
	}
)
