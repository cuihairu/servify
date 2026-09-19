package handlers

import (
	"net/http"
	"strconv"

	workspacedelivery "servify/apps/server/internal/modules/workspace/delivery"

	"github.com/gin-gonic/gin"
)

// WorkspaceHandler 全渠道工作台
type WorkspaceHandler struct {
	service workspacedelivery.HandlerService
}

func NewWorkspaceHandler(service workspacedelivery.HandlerService) *WorkspaceHandler {
	return &WorkspaceHandler{service: service}
}

// GetOverview 获取全渠道工作台概览
// @Summary 全渠道工作台概览
// @Description 返回渠道会话、队列、在线客服等汇总信息（WorkspaceOverview）
// @Tags 全渠道
// @Produce json
// @Param limit query int false "返回最近会话条数，默认10"
// @Success 200 {object} object
// @Failure 500 {object} ErrorResponse
// @Router /api/omni/workspace [get]
func (h *WorkspaceHandler) GetOverview(c *gin.Context) {
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	overview, err := h.service.GetOverview(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to load workspace overview",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, overview)
}

// RegisterWorkspaceRoutes 注册全渠道工作台路由
func RegisterWorkspaceRoutes(r *gin.RouterGroup, handler *WorkspaceHandler) {
	omni := r.Group("/omni")
	{
		omni.GET("/workspace", handler.GetOverview)
	}
}
