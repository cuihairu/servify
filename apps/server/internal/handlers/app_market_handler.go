package handlers

import (
	"net/http"
	"strconv"

	appintegrationdelivery "servify/apps/server/internal/modules/app_integration/delivery"

	"github.com/gin-gonic/gin"
)

// AppMarketHandler 应用市场管理
type AppMarketHandler struct {
	service appintegrationdelivery.HandlerService
}

// NewAppMarketHandler 创建处理器
func NewAppMarketHandler(service appintegrationdelivery.HandlerService) *AppMarketHandler {
	return &AppMarketHandler{service: service}
}

// ListIntegrations 获取集成列表
func (h *AppMarketHandler) ListIntegrations(c *gin.Context) {
	var req appintegrationdelivery.AppIntegrationListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid query", Message: err.Error()})
		return
	}
	items, total, err := h.service.List(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load integrations", Message: err.Error()})
		return
	}
	pages := 0
	if req.PageSize > 0 {
		pages = int((total + int64(req.PageSize) - 1) / int64(req.PageSize))
	}
	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
		Pages:    pages,
	})
}

// CreateIntegration 新增集成
func (h *AppMarketHandler) CreateIntegration(c *gin.Context) {
	var req appintegrationdelivery.AppIntegrationCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	item, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		status := http.StatusBadRequest
		if isConflictError(err) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: "Failed to create integration", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

// UpdateIntegration 更新集成
func (h *AppMarketHandler) UpdateIntegration(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	var req appintegrationdelivery.AppIntegrationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	item, err := h.service.Update(c.Request.Context(), uint(id), &req)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to update integration", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

// DeleteIntegration 移除集成
func (h *AppMarketHandler) DeleteIntegration(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	if err := h.service.Delete(c.Request.Context(), uint(id)); err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to delete integration", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "integration deleted"})
}

// RegisterAppIntegrationRoutes 注册路由
func RegisterAppIntegrationRoutes(r *gin.RouterGroup, handler *AppMarketHandler) {
	if r == nil || handler == nil {
		return
	}
	apps := r.Group("/apps/integrations")
	{
		apps.GET("", handler.ListIntegrations)
		apps.POST("", handler.CreateIntegration)
		apps.PUT("/:id", handler.UpdateIntegration)
		apps.DELETE("/:id", handler.DeleteIntegration)
	}
}
