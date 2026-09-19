package realtime

import (
	"time"

	"github.com/gin-gonic/gin"
)

type WebSocketAdapter struct {
	hub *WebSocketHub
}

func NewWebSocketAdapter(hub *WebSocketHub) *WebSocketAdapter {
	return &WebSocketAdapter{hub: hub}
}

func (a *WebSocketAdapter) HandleWebSocket(c *gin.Context) {
	a.hub.HandleWebSocket(c)
}

func (a *WebSocketAdapter) SendToSession(sessionID string, message Message) {
	a.hub.SendToSession(sessionID, WebSocketMessage{
		Type:      message.Type,
		Data:      message.Data,
		SessionID: sessionID,
		Timestamp: defaultTimestamp(message.Timestamp),
	})
}

func (a *WebSocketAdapter) ClientCount() int {
	return a.hub.GetClientCount()
}

func defaultTimestamp(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now()
	}
	return ts
}
