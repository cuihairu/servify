package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	aidelivery "servify/apps/server/internal/modules/ai/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// AICopilotHandler 坐席 AI 辅助端点：POST /api/v1/ai/copilot，
// body.action 分发 suggest_reply / rewrite / session_summary。
type AICopilotHandler struct {
	copilot *aidelivery.AgentCopilotService
	logger  *logrus.Logger
}

// NewAICopilotHandler 创建坐席辅助处理器；copilot 为 nil 时端点按
// ErrCopilotUnavailable 降级为 503（AI 未装配的部署形态）。
func NewAICopilotHandler(copilot *aidelivery.AgentCopilotService) *AICopilotHandler {
	return &AICopilotHandler{copilot: copilot, logger: logrus.StandardLogger()}
}

// CopilotRequest 坐席辅助 HTTP 请求体（语义见 aidelivery.CopilotRequest）。
type CopilotRequest struct {
	Action    string `json:"action" binding:"required"`
	SessionID string `json:"session_id,omitempty"`
	Draft     string `json:"draft,omitempty"`
	Tone      string `json:"tone,omitempty"`
}

// Copilot 处理坐席辅助请求；哨兵错误→400、AI 未配置→503、其余→500。
func (h *AICopilotHandler) Copilot(c *gin.Context) {
	start := time.Now()

	var req CopilotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, QueryResponse{
			Success:   false,
			Error:     "Invalid request format: " + err.Error(),
			Timestamp: time.Now(),
			Duration:  time.Since(start).String(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.copilot.Copilot(ctx, aidelivery.CopilotRequest{
		Action:    aidelivery.CopilotAction(req.Action),
		SessionID: req.SessionID,
		Draft:     req.Draft,
		Tone:      req.Tone,
	})
	if err != nil {
		switch {
		case errors.Is(err, aidelivery.ErrCopilotUnavailable):
			c.JSON(http.StatusServiceUnavailable, QueryResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now(),
				Duration:  time.Since(start).String(),
			})
		case errors.Is(err, aidelivery.ErrCopilotUnsupportedAction),
			errors.Is(err, aidelivery.ErrCopilotSessionRequired),
			errors.Is(err, aidelivery.ErrCopilotDraftRequired),
			errors.Is(err, aidelivery.ErrCopilotEmptyHistory):
			c.JSON(http.StatusBadRequest, QueryResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now(),
				Duration:  time.Since(start).String(),
			})
		default:
			h.logger.Errorf("AI copilot failed (action=%s): %v", req.Action, err)
			c.JSON(http.StatusInternalServerError, QueryResponse{
				Success:   false,
				Error:     "AI copilot failed: " + err.Error(),
				Timestamp: time.Now(),
				Duration:  time.Since(start).String(),
			})
		}
		return
	}

	c.JSON(http.StatusOK, QueryResponse{
		Success:   true,
		Data:      resp,
		Timestamp: time.Now(),
		Duration:  time.Since(start).String(),
	})
}
