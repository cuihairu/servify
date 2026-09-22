package handlers

import (
	"net/http"

	"servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
)

// RTCICEHandler 下发服务端装配的 ICE 配置（/api/v1/rtc/ice-servers，与 WS
// 建联推送的 webrtc-ice-config 同形；docs/TURN_DEPLOYMENT.md 切片三）。
// 客户端凭 TURN 条目的 ttl 在到期前重新拉取换取新凭据。
type RTCICEHandler struct {
	iceSource realtime.ICEConfigSource
}

func NewRTCICEHandler(iceSource realtime.ICEConfigSource) *RTCICEHandler {
	return &RTCICEHandler{iceSource: iceSource}
}

// GetIceServers 返回当前装配的 ICE 配置：网关未装配 503；空配置（STUN/TURN
// 均未启用）返回空列表，客户端回退宿主自备/直连——REST 面本身始终可答。
func (h *RTCICEHandler) GetIceServers(c *gin.Context) {
	if h.iceSource == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "WebRTC gateway is not wired",
		})
		return
	}
	payload, ok := h.iceSource.ICEConfigPayload()
	entries := payload["ice_servers"]
	if !ok || entries == nil {
		entries = []interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"ice_servers": entries,
		},
	})
}
