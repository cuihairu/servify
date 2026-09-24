package handlers

import (
	"net/http"
	"strconv"
	"strings"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// 访客增量拉取的单页上限：默认页大小与 clamp 上限分开——默认值让 SDK
// 首拉不必带参，上限防全量 dump（长会话历史按游标分页拉取）。
const (
	visitorMessagesDefaultLimit = 50
	visitorMessagesMaxLimit     = 200
)

// VisitorMessagesHandler 访客会话消息增量拉取处理器（M3 移动 SDK 配套
// §10 #1）：挂在 /api/v1 免认证链（与访客 WS 同前缀），SDK 以
// lastMessageId 游标补拉断连期间增量（重连恢复/回前台/推送点开）。
type VisitorMessagesHandler struct {
	messagesService conversationdelivery.VisitorMessagesService
	logger          *logrus.Logger
}

// NewVisitorMessagesHandler 创建访客消息增量拉取处理器
func NewVisitorMessagesHandler(messagesService conversationdelivery.VisitorMessagesService, logger *logrus.Logger) *VisitorMessagesHandler {
	return &VisitorMessagesHandler{
		messagesService: messagesService,
		logger:          logger,
	}
}

// ListAfter 按游标增量拉取会话消息
// @Summary 访客增量拉取会话消息
// @Description 访客（免认证）按消息 ID 单调游标拉取晚于游标的会话消息（升序）；after_id 为空从会话头拉全量，has_more 为 true 时以末条消息 id 续拉
// @Tags 会话
// @Produce json
// @Param session_id path string true "会话 ID"
// @Param after_id query string false "游标：晚于此消息 ID 的增量；空=从会话头拉全量"
// @Param limit query int false "单页上限（默认 50，最大 200）"
// @Success 200 {object} delivery.VisitorMessagesPage
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/sessions/{session_id}/messages [get]
func (h *VisitorMessagesHandler) ListAfter(c *gin.Context) {
	sessionID := c.Param("session_id")
	afterID := c.Query("after_id")

	limit := visitorMessagesDefaultLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "Invalid limit",
				Message: "limit must be a positive integer",
			})
			return
		}
		limit = parsed
	}
	if limit > visitorMessagesMaxLimit {
		limit = visitorMessagesMaxLimit
	}

	page, err := h.messagesService.ListMessagesAfter(c.Request.Context(), sessionID, afterID, limit)
	if err != nil {
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
		h.logger.Errorf("Failed to list visitor messages: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to list messages",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, page)
}
