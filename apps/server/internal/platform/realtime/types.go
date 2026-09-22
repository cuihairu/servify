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

// ICEConfigSource 是 ICE 下发的消费侧窄接口（REST 面 /api/v1/rtc/ice-servers
// 用；WS 建联推送在 hub 侧经同名可选能力接口收敛到同一实现）。
// 实现为 *WebRTCService；ok=false 表示没有任何可下发条目。
type ICEConfigSource interface {
	ICEConfigPayload() (map[string]interface{}, bool)
}
