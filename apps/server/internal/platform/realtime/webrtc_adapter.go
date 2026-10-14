package realtime

import (
	"github.com/pion/webrtc/v4"
)

type WebRTCAdapter struct {
	service *WebRTCService
}

func NewWebRTCAdapter(service *WebRTCService) *WebRTCAdapter {
	return &WebRTCAdapter{service: service}
}

func (a *WebRTCAdapter) ConnectionStats(sessionID string) (map[string]interface{}, error) {
	return a.service.GetConnectionStats(sessionID)
}

func (a *WebRTCAdapter) ConnectionCount() int {
	return a.service.GetConnectionCount()
}

// HandleOffer/HandleAnswer/HandleICECandidate 保持 RTCGateway 形状（REST 面
// 无 WS 连接身份，信令归属传空，按会话回退路由）。
func (a *WebRTCAdapter) HandleOffer(sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	return a.service.HandleOffer("", sessionID, offer)
}

func (a *WebRTCAdapter) HandleAnswer(sessionID string, answer webrtc.SessionDescription) error {
	return a.service.HandleAnswer("", sessionID, answer)
}

func (a *WebRTCAdapter) HandleICECandidate(sessionID string, candidate webrtc.ICECandidateInit) error {
	return a.service.HandleICECandidate("", sessionID, candidate)
}

func (a *WebRTCAdapter) CloseConnection(sessionID string) error {
	return a.service.CloseConnection(sessionID)
}
