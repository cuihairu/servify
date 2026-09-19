package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"servify/apps/server/internal/models"
	sladelivery "servify/apps/server/internal/modules/sla/delivery"

	"github.com/gin-gonic/gin"
)

// SLAHandler SLA管理处理器
// @Summary SLA配置管理
// @Tags SLA
type SLAHandler struct {
	slaService    sladelivery.SLAService
	ticketService SLATicketReader
}

type SLATicketReader interface {
	GetTicketByID(ctx context.Context, ticketID uint) (*models.Ticket, error)
}

// NewSLAHandler 创建SLA处理器
func NewSLAHandler(slaService sladelivery.SLAService, ticketService SLATicketReader) *SLAHandler {
	return &SLAHandler{
		slaService:    slaService,
		ticketService: ticketService,
	}
}

// CreateSLAConfig 创建SLA配置
// @Summary 创建SLA配置
// @Description 创建新的SLA服务等级协议配置
// @Tags SLA
// @Accept json
// @Produce json
// 注意：swag v1.16 不解析跨包类型别名，注解引用 application 包真实 struct（仅文档引用）。
// @Param config body application.SLAConfigCreateRequest true "SLA配置信息"
// @Success 201 {object} models.SLAConfig "创建成功"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 409 {object} ErrorResponse "配置冲突"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/configs [post]
func (h *SLAHandler) CreateSLAConfig(c *gin.Context) {
	var req sladelivery.SLAConfigCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: "请求参数格式错误: " + err.Error(),
		})
		return
	}

	config, err := h.slaService.CreateSLAConfig(c.Request.Context(), &req)
	if err != nil {
		lowerErr := strings.ToLower(err.Error())
		if isInvalidInputError(err) && strings.Contains(lowerErr, "priority") {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "INVALID_PRIORITY",
				Message: "无效的优先级: " + req.Priority,
			})
			return
		}
		if isInvalidInputError(err) || strings.Contains(lowerErr, "must be less than") || strings.Contains(lowerErr, "must be at least") {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "INVALID_CONFIG",
				Message: "SLA配置参数无效: " + err.Error(),
			})
			return
		}
		if isConflictError(err) {
			c.JSON(http.StatusConflict, ErrorResponse{
				Error:   "CONFIG_EXISTS",
				Message: "该优先级/客户等级的SLA配置已存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "CREATE_FAILED",
			Message: "创建SLA配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, config)
}

// GetSLAConfig 获取SLA配置详情
// @Summary 获取SLA配置详情
// @Description 根据ID获取SLA配置的详细信息
// @Tags SLA
// @Produce json
// @Param id path int true "SLA配置ID"
// @Success 200 {object} models.SLAConfig "SLA配置详情"
// @Failure 400 {object} ErrorResponse "参数错误"
// @Failure 404 {object} ErrorResponse "配置不存在"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/configs/{id} [get]
func (h *SLAHandler) GetSLAConfig(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的配置ID",
		})
		return
	}

	config, err := h.slaService.GetSLAConfig(c.Request.Context(), uint(id))
	if err != nil {
		if isNotFoundError(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "CONFIG_NOT_FOUND",
				Message: "SLA配置不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "GET_FAILED",
			Message: "获取SLA配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, config)
}

// ListSLAConfigs 获取SLA配置列表
// @Summary 获取SLA配置列表
// @Description 分页获取SLA配置列表，支持筛选和排序
// @Tags SLA
// @Produce json
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(20)
// @Param priority query []string false "优先级筛选" collectionFormat(multi)
// @Param customer_tier query []string false "客户等级筛选" collectionFormat(multi)
// @Param active query boolean false "是否启用筛选"
// @Param sort_by query string false "排序字段" default(created_at)
// @Param sort_order query string false "排序方向" Enums(asc,desc) default(desc)
// @Success 200 {object} PaginatedResponse "SLA配置列表"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/configs [get]
func (h *SLAHandler) ListSLAConfigs(c *gin.Context) {
	var req sladelivery.SLAConfigListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_QUERY",
			Message: "查询参数错误: " + err.Error(),
		})
		return
	}

	// 设置默认值
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	configs, total, err := h.slaService.ListSLAConfigs(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "LIST_FAILED",
			Message: "获取SLA配置列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     configs,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// UpdateSLAConfig 更新SLA配置
// @Summary 更新SLA配置
// @Description 更新现有的SLA配置信息
// @Tags SLA
// @Accept json
// @Produce json
// @Param id path int true "SLA配置ID"
// @Param config body application.SLAConfigUpdateRequest true "更新的配置信息"
// @Success 200 {object} models.SLAConfig "更新后的配置"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 404 {object} ErrorResponse "配置不存在"
// @Failure 409 {object} ErrorResponse "配置冲突"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/configs/{id} [put]
func (h *SLAHandler) UpdateSLAConfig(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的配置ID",
		})
		return
	}

	var req sladelivery.SLAConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: "请求参数格式错误: " + err.Error(),
		})
		return
	}

	config, err := h.slaService.UpdateSLAConfig(c.Request.Context(), uint(id), &req)
	if err != nil {
		lowerErr := strings.ToLower(err.Error())
		if isNotFoundError(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "CONFIG_NOT_FOUND",
				Message: "SLA配置不存在",
			})
			return
		}
		if isInvalidInputError(err) && strings.Contains(lowerErr, "priority") {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "INVALID_PRIORITY",
				Message: "无效的优先级",
			})
			return
		}
		if isInvalidInputError(err) || strings.Contains(lowerErr, "must be less than") || strings.Contains(lowerErr, "must be at least") {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "INVALID_CONFIG",
				Message: "SLA配置参数无效: " + err.Error(),
			})
			return
		}
		if isConflictError(err) {
			c.JSON(http.StatusConflict, ErrorResponse{
				Error:   "CONFIG_EXISTS",
				Message: "该优先级/客户等级的SLA配置已存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "UPDATE_FAILED",
			Message: "更新SLA配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, config)
}

// DeleteSLAConfig 删除SLA配置
// @Summary 删除SLA配置
// @Description 删除指定的SLA配置（仅当无关联违约记录时）
// @Tags SLA
// @Produce json
// @Param id path int true "SLA配置ID"
// @Success 200 {object} SuccessResponse "删除成功"
// @Failure 400 {object} ErrorResponse "参数错误"
// @Failure 404 {object} ErrorResponse "配置不存在"
// @Failure 409 {object} ErrorResponse "有关联数据无法删除"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/configs/{id} [delete]
func (h *SLAHandler) DeleteSLAConfig(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的配置ID",
		})
		return
	}

	if err := h.slaService.DeleteSLAConfig(c.Request.Context(), uint(id)); err != nil {
		if isNotFoundError(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "CONFIG_NOT_FOUND",
				Message: "SLA配置不存在",
			})
			return
		}
		if strings.Contains(err.Error(), "cannot delete SLA config") {
			c.JSON(http.StatusConflict, ErrorResponse{
				Error:   "HAS_VIOLATIONS",
				Message: "该配置有关联的违约记录，无法删除",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "DELETE_FAILED",
			Message: "删除SLA配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Message: "SLA配置删除成功",
	})
}

// GetSLAConfigByPriority 根据优先级获取SLA配置
// @Summary 根据优先级获取SLA配置
// @Description 根据工单优先级获取对应的SLA配置
// @Tags SLA
// @Produce json
// @Param priority path string true "优先级" Enums(low,normal,high,urgent)
// @Param customer_tier query string false "客户等级（可选，匹配具体等级，否则返回默认配置）"
// @Success 200 {object} models.SLAConfig "SLA配置详情"
// @Failure 404 {object} ErrorResponse "配置不存在"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/configs/priority/{priority} [get]
func (h *SLAHandler) GetSLAConfigByPriority(c *gin.Context) {
	priority := c.Param("priority")
	customerTier := c.Query("customer_tier")

	config, err := h.slaService.GetSLAConfigByPriority(c.Request.Context(), priority, customerTier)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "GET_FAILED",
			Message: "获取SLA配置失败: " + err.Error(),
		})
		return
	}

	if config == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "CONFIG_NOT_FOUND",
			Message: "该优先级暂无SLA配置",
		})
		return
	}

	c.JSON(http.StatusOK, config)
}

// ListSLAViolations 获取SLA违约列表
// @Summary 获取SLA违约列表
// @Description 分页获取SLA违约记录，支持多种筛选条件
// @Tags SLA
// @Produce json
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(20)
// @Param ticket_id query int false "工单ID筛选"
// @Param sla_config_id query int false "SLA配置ID筛选"
// @Param violation_type query []string false "违约类型筛选" collectionFormat(multi)
// @Param resolved query boolean false "是否已解决筛选"
// @Param date_from query string false "开始日期筛选 (YYYY-MM-DD)"
// @Param date_to query string false "结束日期筛选 (YYYY-MM-DD)"
// @Param sort_by query string false "排序字段" default(created_at)
// @Param sort_order query string false "排序方向" Enums(asc,desc) default(desc)
// @Success 200 {object} PaginatedResponse "SLA违约列表"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/violations [get]
func (h *SLAHandler) ListSLAViolations(c *gin.Context) {
	var req sladelivery.SLAViolationListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_QUERY",
			Message: "查询参数错误: " + err.Error(),
		})
		return
	}

	// 设置默认值
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	violations, total, err := h.slaService.ListSLAViolations(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "LIST_FAILED",
			Message: "获取SLA违约列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     violations,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// ResolveSLAViolation 解决SLA违约
// @Summary 解决SLA违约
// @Description 标记指定的SLA违约为已解决状态
// @Tags SLA
// @Produce json
// @Param id path int true "违约记录ID"
// @Success 200 {object} SuccessResponse "解决成功"
// @Failure 400 {object} ErrorResponse "参数错误"
// @Failure 404 {object} ErrorResponse "违约记录不存在"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/violations/{id}/resolve [post]
func (h *SLAHandler) ResolveSLAViolation(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的违约记录ID",
		})
		return
	}

	if err := h.slaService.ResolveSLAViolation(c.Request.Context(), uint(id)); err != nil {
		if isNotFoundError(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "VIOLATION_NOT_FOUND",
				Message: "SLA违约记录不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "RESOLVE_FAILED",
			Message: "解决SLA违约失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Message: "SLA违约已标记为解决",
	})
}

// GetSLAStats 获取SLA统计信息
// @Summary 获取SLA统计信息
// @Description 获取SLA服务质量的详细统计数据和趋势分析
// @Tags SLA
// @Produce json
// @Success 200 {object} application.SLAStatsResponse "SLA统计数据"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/stats [get]
func (h *SLAHandler) GetSLAStats(c *gin.Context) {
	stats, err := h.slaService.GetSLAStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "STATS_FAILED",
			Message: "获取SLA统计失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// CheckTicketSLA 检查工单SLA状态
// @Summary 检查工单SLA状态
// @Description 检查指定工单的SLA合规状态，如有违约则创建记录
// @Tags SLA
// @Produce json
// @Param ticket_id path int true "工单ID"
// @Success 200 {object} models.SLAViolation "SLA违约信息（如有）"
// @Success 204 "无SLA违约"
// @Failure 400 {object} ErrorResponse "参数错误"
// @Failure 404 {object} ErrorResponse "工单不存在"
// @Failure 500 {object} ErrorResponse "服务器错误"
// @Router /api/sla/check/ticket/{ticket_id} [post]
func (h *SLAHandler) CheckTicketSLA(c *gin.Context) {
	idParam := c.Param("ticket_id")
	ticketID, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的工单ID",
		})
		return
	}

	// 获取真实工单信息
	ticket, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(ticketID))
	if err != nil {
		if isNotFoundError(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "TICKET_NOT_FOUND",
				Message: "工单不存在或已删除",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "GET_TICKET_FAILED",
			Message: "获取工单失败: " + err.Error(),
		})
		return
	}

	violation, err := h.slaService.CheckSLAViolation(c.Request.Context(), ticket)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "CHECK_FAILED",
			Message: "检查SLA状态失败: " + err.Error(),
		})
		return
	}

	if violation == nil {
		c.JSON(http.StatusNoContent, nil)
		return
	}

	c.JSON(http.StatusOK, violation)
}

// RegisterSLARoutes 注册SLA相关路由
func RegisterSLARoutes(r *gin.RouterGroup, handler *SLAHandler) {
	sla := r.Group("/sla")
	{
		// SLA配置管理
		configs := sla.Group("/configs")
		{
			configs.POST("", handler.CreateSLAConfig)                          // 创建SLA配置
			configs.GET("", handler.ListSLAConfigs)                            // 获取SLA配置列表
			configs.GET("/:id", handler.GetSLAConfig)                          // 获取SLA配置详情
			configs.PUT("/:id", handler.UpdateSLAConfig)                       // 更新SLA配置
			configs.DELETE("/:id", handler.DeleteSLAConfig)                    // 删除SLA配置
			configs.GET("/priority/:priority", handler.GetSLAConfigByPriority) // 根据优先级获取SLA配置
		}

		// SLA违约管理
		violations := sla.Group("/violations")
		{
			violations.GET("", handler.ListSLAViolations)                // 获取SLA违约列表
			violations.POST("/:id/resolve", handler.ResolveSLAViolation) // 解决SLA违约
		}

		// SLA统计和监控
		sla.GET("/stats", handler.GetSLAStats)                       // 获取SLA统计信息
		sla.POST("/check/ticket/:ticket_id", handler.CheckTicketSLA) // 检查工单SLA状态
	}
}
