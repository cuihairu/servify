package realtime

import (
	"servify/apps/server/internal/services"

	"github.com/pion/webrtc/v4"
)

type WebRTCAdapter struct {
	service *services.WebRTCService
}

func NewWebRTCAdapter(service *services.WebRTCService) *WebRTCAdapter {
	return &WebRTCAdapter{service: service}
}

func (a *WebRTCAdapter) ConnectionStats(sessionID string) (map[string]interface{}, error) {
	return a.service.GetConnectionStats(sessionID)
}

func (a *WebRTCAdapter) ConnectionCount() int {
	return a.service.GetConnectionCount()
}

func (a *WebRTCAdapter) HandleOffer(sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	return a.service.HandleOffer(sessionID, offer)
}

func (a *WebRTCAdapter) HandleAnswer(sessionID string, answer webrtc.SessionDescription) error {
	return a.service.HandleAnswer(sessionID, answer)
}

func (a *WebRTCAdapter) HandleICECandidate(sessionID string, candidate webrtc.ICECandidateInit) error {
	return a.service.HandleICECandidate(sessionID, candidate)
}

func (a *WebRTCAdapter) CloseConnection(sessionID string) error {
	return a.service.CloseConnection(sessionID)
}
