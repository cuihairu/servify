package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"net/http"
	"servify/apps/server/internal/platform/realtime"
	"strings"
)

type WebSocketHandler struct {
	wsHub realtime.RealtimeGateway
}

func NewWebSocketHandler(wsHub realtime.RealtimeGateway) *WebSocketHandler {
	return &WebSocketHandler{
		wsHub: wsHub,
	}
}

func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	h.wsHub.HandleWebSocket(c)
}

func (h *WebSocketHandler) GetStats(c *gin.Context) {
	stats := map[string]interface{}{
		"connected_clients": h.wsHub.ClientCount(),
		"status":            "running",
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

type WebRTCHandler struct {
	webrtcService realtime.RTCGateway
}

func NewWebRTCHandler(webrtcService realtime.RTCGateway) *WebRTCHandler {
	return &WebRTCHandler{
		webrtcService: webrtcService,
	}
}

func (h *WebRTCHandler) GetStats(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": map[string]interface{}{
				"connection_count": h.webrtcService.ConnectionCount(),
				"scope":            "all",
				"status":           "running",
			},
		})
		return
	}

	stats, err := h.webrtcService.ConnectionStats(sessionID)
	if err != nil {
		logrus.Errorf("Failed to get WebRTC stats: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to get connection stats",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

func (h *WebRTCHandler) GetConnections(c *gin.Context) {
	count := h.webrtcService.ConnectionCount()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": map[string]interface{}{
			"connection_count": count,
		},
	})
}

// PlatformStatsReader 是 MessageHandler 的消费侧窄接口：只读平台消息路由的
// 统计视图（完整运行契约仍在 services.MessageRouterRuntime，随 realtime 收窄迁移）。
type PlatformStatsReader interface {
	GetPlatformStats() map[string]interface{}
}

type MessageHandler struct {
	messageRouter PlatformStatsReader
}

func NewMessageHandler(messageRouter PlatformStatsReader) *MessageHandler {
	return &MessageHandler{
		messageRouter: messageRouter,
	}
}

func (h *MessageHandler) GetPlatformStats(c *gin.Context) {
	stats := h.messageRouter.GetPlatformStats()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}
