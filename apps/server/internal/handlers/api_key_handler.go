package handlers

import (
	"errors"
	"net/http"
	"strconv"

	apikeydelivery "servify/apps/server/internal/modules/api_key/delivery"

	"github.com/gin-gonic/gin"
)

// APIKeyHandler 开放平台 API Key 管理面（签发/吊销/删除）。
type APIKeyHandler struct {
	service apikeydelivery.HandlerService
}

func NewAPIKeyHandler(service apikeydelivery.HandlerService) *APIKeyHandler {
	return &APIKeyHandler{service: service}
}

// List godoc
// @Router /api/v1/api-keys [get]
func (h *APIKeyHandler) List(c *gin.Context) {
	keys, err := h.service.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list api keys", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": keys, "total": len(keys)})
}

// Create godoc
// @Router /api/v1/api-keys [post]
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req apikeydelivery.APIKeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	createdBy, _ := c.Get("user_id")
	createdByID, _ := createdBy.(uint)
	row, plaintext, err := h.service.Create(c.Request.Context(), &req, strconv.FormatUint(uint64(createdByID), 10))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to create api key", Message: err.Error()})
		return
	}
	// 明文密钥只在此响应中出现一次，之后任何接口都不再返回。
	c.JSON(http.StatusCreated, gin.H{"api_key": row, "plaintext": plaintext})
}

// Revoke godoc
// @Router /api/v1/api-keys/{id}/revoke [post]
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: "invalid api key id"})
		return
	}
	row, err := h.service.Revoke(c.Request.Context(), uint(id))
	if isNotFoundError(err) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Api key not found", Message: err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to revoke api key", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"api_key": row})
}

// Delete godoc
// @Router /api/v1/api-keys/{id} [delete]
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: "invalid api key id"})
		return
	}
	if err := h.service.Delete(c.Request.Context(), uint(id)); err != nil {
		if errors.Is(err, apikeydelivery.ErrAPIKeyNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Api key not found", Message: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete api key", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": "deleted"})
}

// RegisterAPIKeyRoutes 挂载到 RequireResourcePermission("api_keys") 之后。
func RegisterAPIKeyRoutes(group *gin.RouterGroup, handler *APIKeyHandler) {
	apiKeys := group.Group("/api-keys")
	{
		apiKeys.GET("", handler.List)
		apiKeys.POST("", handler.Create)
		apiKeys.POST("/:id/revoke", handler.Revoke)
		apiKeys.DELETE("/:id", handler.Delete)
	}
}
