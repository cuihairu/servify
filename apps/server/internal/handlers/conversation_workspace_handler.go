package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type ConversationWorkspaceHandler struct {
	service           conversationdelivery.HandlerService
	realtime          realtimeplatform.RealtimeGateway
	translator        translationdelivery.RealtimeTranslateService
	historyTranslator translationdelivery.HistoryTranslateService
}

// NewConversationWorkspaceHandler 创建会话工作台处理器；translator 为可选
// 依赖（Phase 1 刀三：坐席 → 访客方向自动翻译，按会话 visitor 读向偏好
// 异步翻译并广播 message-translated 帧；未注入时保持既有行为）。
func NewConversationWorkspaceHandler(service conversationdelivery.HandlerService, realtime realtimeplatform.RealtimeGateway, translator translationdelivery.RealtimeTranslateService) *ConversationWorkspaceHandler {
	return &ConversationWorkspaceHandler{service: service, realtime: realtime, translator: translator}
}

// WithHistoryTranslator 注入历史消息批量标注服务（Phase 1 收尾：工作台
// 历史面按 agent 读向偏好批量翻译访客消息，译文以 §4.4 metadata 保留键
// 附在响应上，不改存储；未注入时保持既有行为）。
func (h *ConversationWorkspaceHandler) WithHistoryTranslator(t translationdelivery.HistoryTranslateService) *ConversationWorkspaceHandler {
	h.historyTranslator = t
	return h
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

	h.annotateHistory(c.Request.Context(), sessionID, items)

	c.JSON(http.StatusOK, gin.H{
		"data": items,
	})
}

// annotateHistory 批量标注历史消息译文（Phase 1 收尾，§4.4 metadata 保留
// 键）：只标访客侧（sender != "agent"）且尚未带译文的非空消息；批量翻译
// 是增强能力——服务未注入、会话无偏好、provider 未配置、批量失败（含存储
// 错误）一律静默返回原列表，不影响历史加载主链路；译文只写响应 DTO，
// 不回写存储。
func (h *ConversationWorkspaceHandler) annotateHistory(ctx context.Context, sessionID string, items []conversationdelivery.ConversationMessageDTO) {
	if h.historyTranslator == nil || len(items) == 0 {
		return
	}
	indexes := make([]int, 0, len(items))
	texts := make([]string, 0, len(items))
	for i, item := range items {
		if item.Sender == "agent" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		if item.Metadata["translation"] != "" {
			continue
		}
		indexes = append(indexes, i)
		texts = append(texts, item.Content)
	}
	if len(indexes) == 0 {
		return
	}
	translated, err := h.historyTranslator.TranslateHistory(ctx, sessionID, texts)
	if err != nil || translated == nil {
		return
	}
	for j, i := range indexes {
		if j >= len(translated.Texts) || translated.Texts[j] == "" {
			continue
		}
		if items[i].Metadata == nil {
			items[i].Metadata = map[string]string{}
		}
		items[i].Metadata["translation"] = translated.Texts[j]
		items[i].Metadata["translation_lang"] = translated.TargetLang
	}
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

	// 坐席 → 访客方向自动翻译（Phase 1 刀三，docs/realtime-translation-
	// design.md §1.4）：与 hub 刀二同一契约/同一帧型，落库广播后的异步
	// 旁路（消费会话 visitor 读向偏好）；无偏好与 provider 未配置静默，
	// 其余失败只记日志，不影响消息主链路。
	if h.translator != nil {
		go h.translateAgentMessage(sessionID, content)
	}

	c.JSON(http.StatusCreated, SuccessResponse{
		Message: "Message sent successfully",
		Data:    item,
	})
}

// translateAgentMessage 翻译单条坐席消息并广播 message-translated 帧；
// 失败不回传任何帧。
func (h *ConversationWorkspaceHandler) translateAgentMessage(sessionID, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := h.translator.TranslateSessionMessage(ctx, sessionID, text)
	if err != nil {
		if !errors.Is(err, translationdelivery.ErrTranslationUnavailable) {
			logrus.WithField("session_id", sessionID).Warnf("Agent message translation failed: %v", err)
		}
		return
	}
	if result == nil || h.realtime == nil {
		return
	}
	h.realtime.SendToSession(sessionID, realtimeplatform.Message{
		Type:      "message-translated",
		Data:      result,
		SessionID: sessionID,
		Timestamp: time.Now(),
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
