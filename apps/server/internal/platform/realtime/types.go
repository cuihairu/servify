package realtime

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
)

type Message struct {
	Type      string
	Data      interface{}
	SessionID string
	Timestamp time.Time
}

type RealtimeGateway interface {
	HandleWebSocket(*gin.Context)
	SendToSession(sessionID string, message Message)
	ClientCount() int
}

type RTCGateway interface {
	ConnectionStats(sessionID string) (map[string]interface{}, error)
	ConnectionCount() int
	HandleOffer(sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error)
	HandleAnswer(sessionID string, answer webrtc.SessionDescription) error
	HandleICECandidate(sessionID string, candidate webrtc.ICECandidateInit) error
	CloseConnection(sessionID string) error
}
