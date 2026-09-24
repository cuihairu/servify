package handlers

import (
	"net/http"
	"strings"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// VisitorReadRequest 访客已读游标推进请求体
type VisitorReadRequest struct {
	LastReadMessageID string `json:"last_read_message_id" binding:"required"`
}

// VisitorReadHandler 访客未读数/已读游标处理器（M3 移动 SDK 配套 §10 #3）：
// 挂 /api/v1 免认证链（与访客 WS、增量补拉同前缀同安全模型）。SDK 推进
// 已读游标换回最新未读数（点开推送/回前台一次性对齐），服务端口径与 D7
// 客户端推导一致（只计 agent/system 来源）。
type VisitorReadHandler struct {
	readService conversationdelivery.VisitorReadService
	logger      *logrus.Logger
}

// NewVisitorReadHandler 创建访客未读面处理器
func NewVisitorReadHandler(readService conversationdelivery.VisitorReadService, logger *logrus.Logger) *VisitorReadHandler {
	return &VisitorReadHandler{readService: readService, logger: logger}
}

// MarkRead 推进访客已读游标并返回最新未读状态
// @Summary 推进访客已读游标
// @Description 访客（免认证）以 last_read_message_id 推进会话已读游标并返回推进后的未读数与游标回显；游标只前进不后退，消息不存在或不属于该会话 400，会话不存在 404
// @Tags 会话
// @Accept json
// @Produce json
// @Param session_id path string true "会话 ID"
// @Param request body VisitorReadRequest true "已读游标"
// @Success 200 {object} delivery.VisitorReadState
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/sessions/{session_id}/read [post]
func (h *VisitorReadHandler) MarkRead(c *gin.Context) {
	sessionID := c.Param("session_id")

	var req VisitorReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: "last_read_message_id is required",
		})
		return
	}

	state, err := h.readService.MarkRead(c.Request.Context(), sessionID, strings.TrimSpace(req.LastReadMessageID))
	if err != nil {
		h.respondReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

// Unread 返回访客当前未读状态
// @Summary 查询访客未读状态
// @Description 访客（免认证）查询会话未读数（只计 agent/system 来源且 ID 大于已读游标）与当前游标回显；会话不存在 404
// @Tags 会话
// @Produce json
// @Param session_id path string true "会话 ID"
// @Success 200 {object} delivery.VisitorReadState
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/sessions/{session_id}/unread [get]
func (h *VisitorReadHandler) Unread(c *gin.Context) {
	state, err := h.readService.UnreadState(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		h.respondReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

// respondReadError 复用 §10 #1 的错误映射口径：会话失效 404、游标非法 400、
// 其余 500 兜底（不向访客泄露内部错误细节）。
func (h *VisitorReadHandler) respondReadError(c *gin.Context, err error) {
	if strings.Contains(err.Error(), "session not found") {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "Session not found",
			Message: err.Error(),
		})
		return
	}
	if strings.Contains(err.Error(), "invalid message cursor") {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid message cursor",
			Message: err.Error(),
		})
		return
	}
	if h.logger != nil {
		h.logger.WithError(err).Error("visitor read state request failed")
	}
	c.JSON(http.StatusInternalServerError, ErrorResponse{
		Error:   "Internal error",
		Message: "failed to update visitor read state",
	})
}
