package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
)

type ConversationWorkspaceHandler struct {
	service  conversationdelivery.HandlerService
	realtime realtimeplatform.RealtimeGateway
}

func NewConversationWorkspaceHandler(service conversationdelivery.HandlerService, realtime realtimeplatform.RealtimeGateway) *ConversationWorkspaceHandler {
	return &ConversationWorkspaceHandler{service: service, realtime: realtime}
}

func (h *ConversationWorkspaceHandler) GetSession(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Conversation service unavailable",
			Message: "conversation service is not configured",
		})
		return
	}

	sessionID := c.Param("id")
	dto, err := h.service.GetConversation(c.Request.Context(), sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, conversationdelivery.ErrConversationNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to load conversation",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": dto})
}

func (h *ConversationWorkspaceHandler) ListMessages(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Conversation service unavailable",
			Message: "conversation service is not configured",
		})
		return
	}

	sessionID := c.Param("id")
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	before := c.Query("before")

	var items []conversationdelivery.ConversationMessageDTO
	var err error
	if before != "" {
		items, err = h.service.ListMessagesBefore(c.Request.Context(), sessionID, before, limit)
	} else {
		items, err = h.service.ListMessages(c.Request.Context(), sessionID, limit)
	}

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, conversationdelivery.ErrConversationNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to load conversation messages",
			Message: err.Error(),
		})
		return
	}

	// Repository returns latest-first; management UI needs chronological order.
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}

	c.JSON(http.StatusOK, gin.H{
		"data": items,
	})
}

func (h *ConversationWorkspaceHandler) SendMessage(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Conversation service unavailable",
			Message: "conversation service is not configured",
		})
		return
	}

	sessionID := c.Param("id")
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: "content is required",
		})
		return
	}

	item, err := h.service.SendAgentMessage(c.Request.Context(), sessionID, content)
	if err != nil {
		status := http.StatusInternalServerError
		errLabel := "Failed to send message"
		if errors.Is(err, conversationdelivery.ErrConversationNotFound) {
			status = http.StatusNotFound
			errLabel = "Conversation not found"
		}
		c.JSON(status, ErrorResponse{
			Error:   errLabel,
			Message: err.Error(),
		})
		return
	}

	if h.realtime != nil {
		h.realtime.SendToSession(sessionID, realtimeplatform.Message{
			Type: "agent-message",
			Data: map[string]interface{}{
				"content": item.Content,
				"sender":  item.Sender,
			},
			SessionID: sessionID,
			Timestamp: time.Now(),
		})
	}

	c.JSON(http.StatusCreated, SuccessResponse{
		Message: "Message sent successfully",
		Data:    item,
	})
}

func (h *ConversationWorkspaceHandler) AssignAgent(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Conversation service unavailable",
			Message: "conversation service is not configured",
		})
		return
	}

	sessionID := c.Param("id")
	var req struct {
		AgentID uint `json:"agent_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	dto, err := h.service.AssignAgent(c.Request.Context(), sessionID, req.AgentID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, conversationdelivery.ErrConversationNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to assign agent",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Message: "Agent assigned successfully",
		Data:    dto,
	})
}

func (h *ConversationWorkspaceHandler) Transfer(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Conversation service unavailable",
			Message: "conversation service is not configured",
		})
		return
	}

	sessionID := c.Param("id")
	var req struct {
		ToAgentID uint `json:"to_agent_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	dto, err := h.service.Transfer(c.Request.Context(), sessionID, req.ToAgentID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, conversationdelivery.ErrConversationNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to transfer conversation",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Message: "Conversation transferred successfully",
		Data:    dto,
	})
}

func (h *ConversationWorkspaceHandler) CloseSession(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Conversation service unavailable",
			Message: "conversation service is not configured",
		})
		return
	}

	sessionID := c.Param("id")
	dto, err := h.service.Close(c.Request.Context(), sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, conversationdelivery.ErrConversationNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to close conversation",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Message: "Conversation closed successfully",
		Data:    dto,
	})
}

func RegisterConversationWorkspaceRoutes(r *gin.RouterGroup, handler *ConversationWorkspaceHandler) {
	omni := r.Group("/omni")
	{
		omni.GET("/sessions/:id", handler.GetSession)
		omni.GET("/sessions/:id/messages", handler.ListMessages)
		omni.POST("/sessions/:id/messages", handler.SendMessage)
		omni.POST("/sessions/:id/assign", handler.AssignAgent)
		omni.POST("/sessions/:id/transfer", handler.Transfer)
		omni.POST("/sessions/:id/close", handler.CloseSession)
	}
}
