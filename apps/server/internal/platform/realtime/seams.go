package realtime

// 本文件聚合仅供测试注入的包级 seam。每个变量的默认值都保持生产行为，
// 生产代码不得在运行时改写；测试通过替换变量来驱动错误分支
//（pion API 失败）。auth 模块的 seam 已随模块迁移至
// internal/modules/auth/application/seams.go。

import "github.com/pion/webrtc/v4"

var (
	// pion WebRTC 不可注入的错误路径：对合法 SDP/连接这些操作实际不会失败，
	// 通过包级函数变量注入错误以覆盖防御性错误分支。
	hookPeerConnectionCreateAnswer = func(pc *webrtc.PeerConnection, options *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
		return pc.CreateAnswer(options)
	}
	hookPeerConnectionCreateOffer = func(pc *webrtc.PeerConnection, options *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
		return pc.CreateOffer(options)
	}
	hookPeerConnectionSetLocalDescription = func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
		return pc.SetLocalDescription(desc)
	}
	hookPeerConnectionClose = func(pc *webrtc.PeerConnection) error {
		return pc.Close()
	}
	// hookNewTrackLocalStaticRTP 是媒体桥接的本地轨构造缝线：codec 能力非法
	// 等形态在真实链路里由发布端 SDP 决定，无法稳定构造，注入错误以覆盖
	// 转发分支的失败处理。
	hookNewTrackLocalStaticRTP = func(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
		return webrtc.NewTrackLocalStaticRTP(codec, id, streamID)
	}
)
