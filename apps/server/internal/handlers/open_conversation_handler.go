package handlers

import (
	"net/http"
	"strconv"
	"strings"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
)

// OpenConversationHandler 开放 API（X-API-Key service principal）只读会话面。
type OpenConversationHandler struct {
	service conversationdelivery.OpenConversationReader
}

func NewOpenConversationHandler(service conversationdelivery.OpenConversationReader) *OpenConversationHandler {
	return &OpenConversationHandler{service: service}
}

// List godoc
// @Router /api/v1/conversations [get]
func (h *OpenConversationHandler) List(c *gin.Context) {
	query := conversationapp.OpenSessionListQuery{
		Status:  strings.TrimSpace(c.Query("status")),
		Channel: strings.TrimSpace(c.Query("channel")),
	}
	if v := c.Query("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			query.Page = p
		}
	}
	if v := c.Query("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil {
			query.PageSize = ps
		}
	}
	items, total, err := h.service.ListConversations(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list conversations", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": query.Page, "page_size": query.PageSize})
}

// Get godoc
// @Router /api/v1/conversations/{id} [get]
func (h *OpenConversationHandler) Get(c *gin.Context) {
	item, err := h.service.GetConversation(c.Request.Context(), c.Param("id"))
	if isNotFoundError(err) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Conversation not found", Message: err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get conversation", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": item})
}

// Messages godoc
// @Router /api/v1/conversations/{id}/messages [get]
func (h *OpenConversationHandler) Messages(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	items, err := h.service.ListRecentMessages(c.Request.Context(), c.Param("id"), limit)
	if isNotFoundError(err) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Conversation not found", Message: err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list messages", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// RegisterOpenConversationRoutes 挂载到 RequireResourcePermission("conversations") 之后。
func RegisterOpenConversationRoutes(group *gin.RouterGroup, handler *OpenConversationHandler) {
	conversations := group.Group("/conversations")
	{
		conversations.GET("", handler.List)
		conversations.GET("/:id", handler.Get)
		conversations.GET("/:id/messages", handler.Messages)
	}
}
