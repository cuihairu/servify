package handlers

import (
	"net/http"
	"strconv"

	shiftdelivery "servify/apps/server/internal/modules/shift/delivery"

	"github.com/gin-gonic/gin"
)

// ShiftHandler 班次管理处理器
type ShiftHandler struct {
	shiftService shiftdelivery.HandlerService
}

// NewShiftHandler 创建班次处理器
func NewShiftHandler(shiftService shiftdelivery.HandlerService) *ShiftHandler {
	return &ShiftHandler{shiftService: shiftService}
}

// CreateShift 创建班次
func (h *ShiftHandler) CreateShift(c *gin.Context) {
	var req shiftdelivery.ShiftCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: err.Error(),
		})
		return
	}

	shift, err := h.shiftService.CreateShift(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "CREATE_FAILED",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, shift)
}

// ListShifts 获取班次列表
func (h *ShiftHandler) ListShifts(c *gin.Context) {
	var req shiftdelivery.ShiftListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_QUERY",
			Message: err.Error(),
		})
		return
	}

	shifts, total, err := h.shiftService.ListShifts(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "LIST_FAILED",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     shifts,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// UpdateShift 更新班次
func (h *ShiftHandler) UpdateShift(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的班次ID",
		})
		return
	}

	var req shiftdelivery.ShiftUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: err.Error(),
		})
		return
	}

	shift, err := h.shiftService.UpdateShift(c.Request.Context(), uint(id), &req)
	if err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "shift not found" {
			status = http.StatusNotFound
		} else if err.Error() == "end_time must be after start_time" {
			status = http.StatusBadRequest
		}

		c.JSON(status, ErrorResponse{
			Error:   "UPDATE_FAILED",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, shift)
}

// DeleteShift 删除班次
func (h *ShiftHandler) DeleteShift(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_ID",
			Message: "无效的班次ID",
		})
		return
	}

	if err := h.shiftService.DeleteShift(c.Request.Context(), uint(id)); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "shift not found" {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "DELETE_FAILED",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "班次删除成功"})
}

// GetShiftStats 获取班次统计
func (h *ShiftHandler) GetShiftStats(c *gin.Context) {
	stats, err := h.shiftService.GetShiftStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "STATS_FAILED",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// RegisterShiftRoutes 注册班次路由
func RegisterShiftRoutes(r *gin.RouterGroup, handler *ShiftHandler) {
	shift := r.Group("/shifts")
	{
		shift.POST("", handler.CreateShift)
		shift.GET("", handler.ListShifts)
		shift.PUT("/:id", handler.UpdateShift)
		shift.DELETE("/:id", handler.DeleteShift)
		shift.GET("/stats", handler.GetShiftStats)
	}
}
