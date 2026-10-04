package delivery

// V1.0 收敛 B2-3（docs/v1-convergence-plan.md §4）：statistics 旧 handler
// （原 internal/handlers/statistics_*.go）迁移式收口进 analytics 模块——
// 路由、请求参数与响应 JSON 形状保持不变，仅归属从顶层 handlers 包
// 移交本模块 delivery 层。

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// statisticsErrorResponse 统计端点错误响应（与原 handlers.ErrorResponse
// 同一 JSON 形状，保持 API 契约不变）。
type statisticsErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

// StatisticsHandler 统计数据处理器（analytics 模块自有 HTTP 面）。
type StatisticsHandler struct {
	statsService HandlerService
	logger       *logrus.Logger
}

// NewStatisticsHandler 创建统计处理器。
func NewStatisticsHandler(statsService HandlerService, logger *logrus.Logger) *StatisticsHandler {
	return &StatisticsHandler{
		statsService: statsService,
		logger:       logger,
	}
}

// GetDashboardStats 获取仪表板统计数据
// @Summary 获取仪表板统计数据
// @Description 获取系统概览统计信息，包括用户、工单、会话等总体数据
// @Tags 统计
// @Accept json
// @Produce json
// @Success 200 {object} contract.DashboardStats
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/dashboard [get]
func (h *StatisticsHandler) GetDashboardStats(c *gin.Context) {
	stats, err := h.statsService.GetDashboardStats(c.Request.Context())
	if err != nil {
		h.logError("Failed to get dashboard stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get dashboard statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetTimeRangeStats 获取时间范围统计
// @Summary 获取时间范围统计数据
// @Description 获取指定时间范围内的详细统计数据
// @Tags 统计
// @Accept json
// @Produce json
// @Param start_date query string true "开始日期 (YYYY-MM-DD)"
// @Param end_date query string true "结束日期 (YYYY-MM-DD)"
// @Success 200 {array} contract.TimeRangeStats
// @Failure 400 {object} statisticsErrorResponse
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/time-range [get]
func (h *StatisticsHandler) GetTimeRangeStats(c *gin.Context) {
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")

	if startDateStr == "" || endDateStr == "" {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Missing required parameters",
			Message: "start_date and end_date are required",
		})
		return
	}

	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid start_date format",
			Message: "Use YYYY-MM-DD format",
		})
		return
	}

	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid end_date format",
			Message: "Use YYYY-MM-DD format",
		})
		return
	}

	if endDate.Before(startDate) {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid date range",
			Message: "end_date must be after start_date",
		})
		return
	}

	stats, err := h.statsService.GetTimeRangeStats(c.Request.Context(), startDate, endDate)
	if err != nil {
		h.logError("Failed to get time range stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get time range statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetAgentPerformanceStats 获取客服绩效统计
// @Summary 获取客服绩效统计
// @Description 获取客服人员的工作绩效统计数据
// @Tags 统计
// @Accept json
// @Produce json
// @Param start_date query string true "开始日期 (YYYY-MM-DD)"
// @Param end_date query string true "结束日期 (YYYY-MM-DD)"
// @Param limit query int false "限制结果数量"
// @Success 200 {array} contract.AgentPerformanceStats
// @Failure 400 {object} statisticsErrorResponse
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/agent-performance [get]
func (h *StatisticsHandler) GetAgentPerformanceStats(c *gin.Context) {
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	limitStr := c.DefaultQuery("limit", "10")

	if startDateStr == "" || endDateStr == "" {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Missing required parameters",
			Message: "start_date and end_date are required",
		})
		return
	}

	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid start_date format",
			Message: "Use YYYY-MM-DD format",
		})
		return
	}

	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid end_date format",
			Message: "Use YYYY-MM-DD format",
		})
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 10
	}

	stats, err := h.statsService.GetAgentPerformanceStats(c.Request.Context(), startDate, endDate, limit)
	if err != nil {
		h.logError("Failed to get agent performance stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get agent performance statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetTicketCategoryStats 获取工单分类统计
// @Summary 获取工单分类统计
// @Description 获取不同类别工单的数量统计
// @Tags 统计
// @Accept json
// @Produce json
// @Param start_date query string false "开始日期 (YYYY-MM-DD)"
// @Param end_date query string false "结束日期 (YYYY-MM-DD)"
// @Success 200 {array} contract.CategoryStats
// @Failure 400 {object} statisticsErrorResponse
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/ticket-category [get]
func (h *StatisticsHandler) GetTicketCategoryStats(c *gin.Context) {
	startDate, endDate, ok := h.parseOptionalRange(c)
	if !ok {
		return
	}

	stats, err := h.statsService.GetTicketCategoryStats(c.Request.Context(), startDate, endDate)
	if err != nil {
		h.logError("Failed to get ticket category stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get ticket category statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetTicketPriorityStats 获取工单优先级统计
// @Summary 获取工单优先级统计
// @Description 获取不同优先级工单的数量统计
// @Tags 统计
// @Accept json
// @Produce json
// @Param start_date query string false "开始日期 (YYYY-MM-DD)"
// @Param end_date query string false "结束日期 (YYYY-MM-DD)"
// @Success 200 {array} contract.CategoryStats
// @Failure 400 {object} statisticsErrorResponse
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/ticket-priority [get]
func (h *StatisticsHandler) GetTicketPriorityStats(c *gin.Context) {
	startDate, endDate, ok := h.parseOptionalRange(c)
	if !ok {
		return
	}

	stats, err := h.statsService.GetTicketPriorityStats(c.Request.Context(), startDate, endDate)
	if err != nil {
		h.logError("Failed to get ticket priority stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get ticket priority statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// parseOptionalRange 解析可选 start_date/end_date（缺省最近 30 天），
// 参数非法时直接写出 400 并返回 false。
func (h *StatisticsHandler) parseOptionalRange(c *gin.Context) (time.Time, time.Time, bool) {
	startDateStr := c.DefaultQuery("start_date", "")
	endDateStr := c.DefaultQuery("end_date", "")

	if startDateStr == "" || endDateStr == "" {
		// 默认使用最近30天
		endDate := time.Now()
		return endDate.AddDate(0, 0, -30), endDate, true
	}

	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid start_date format",
			Message: "Use YYYY-MM-DD format",
		})
		return time.Time{}, time.Time{}, false
	}

	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid end_date format",
			Message: "Use YYYY-MM-DD format",
		})
		return time.Time{}, time.Time{}, false
	}
	return startDate, endDate, true
}

// GetCustomerSourceStats 获取客户来源统计
// @Summary 获取客户来源统计
// @Description 获取不同来源客户的数量统计
// @Tags 统计
// @Accept json
// @Produce json
// @Success 200 {array} contract.CategoryStats
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/customer-source [get]
func (h *StatisticsHandler) GetCustomerSourceStats(c *gin.Context) {
	stats, err := h.statsService.GetCustomerSourceStats(c.Request.Context())
	if err != nil {
		h.logError("Failed to get customer source stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get customer source statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetRemoteAssistTicketStats 获取远程协助工单统计
// @Summary 获取远程协助工单统计
// @Description 获取远程协助来源工单的总量、待处理、已解决、已关闭统计
// @Tags 统计
// @Accept json
// @Produce json
// @Success 200 {object} contract.RemoteAssistTicketStats
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/remote-assist-tickets [get]
func (h *StatisticsHandler) GetRemoteAssistTicketStats(c *gin.Context) {
	stats, err := h.statsService.GetRemoteAssistTicketStats(c.Request.Context())
	if err != nil {
		h.logError("Failed to get remote assist ticket stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to get remote assist ticket statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// UpdateDailyStats 手动更新每日统计
// @Summary 手动更新每日统计
// @Description 手动触发指定日期的统计数据更新
// @Tags 统计
// @Accept json
// @Produce json
// @Param date query string false "日期 (YYYY-MM-DD)，默认为今天"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} statisticsErrorResponse
// @Failure 500 {object} statisticsErrorResponse
// @Router /api/statistics/update-daily [post]
func (h *StatisticsHandler) UpdateDailyStats(c *gin.Context) {
	dateStr := c.DefaultQuery("date", time.Now().Format("2006-01-02"))

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, statisticsErrorResponse{
			Error:   "Invalid date format",
			Message: "Use YYYY-MM-DD format",
		})
		return
	}

	if err := h.statsService.UpdateDailyStats(c.Request.Context(), date); err != nil {
		h.logError("Failed to update daily stats", err)
		c.JSON(http.StatusInternalServerError, statisticsErrorResponse{
			Error:   "Failed to update daily statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Daily statistics updated successfully",
		"date":    dateStr,
	})
}

func (h *StatisticsHandler) logError(msg string, err error) {
	if h.logger != nil {
		h.logger.Errorf("%s: %v", msg, err)
	}
}

// RegisterStatisticsRoutes 注册统计相关路由（含导出，B2-3 收口后由
// analytics 模块直接向 router 提供）。
func RegisterStatisticsRoutes(r *gin.RouterGroup, handler *StatisticsHandler, exportHandler *StatisticsExportHandler) {
	stats := r.Group("/statistics")
	{
		stats.GET("/dashboard", handler.GetDashboardStats)
		stats.GET("/time-range", handler.GetTimeRangeStats)
		stats.GET("/agent-performance", handler.GetAgentPerformanceStats)
		stats.GET("/ticket-category", handler.GetTicketCategoryStats)
		stats.GET("/ticket-priority", handler.GetTicketPriorityStats)
		stats.GET("/customer-source", handler.GetCustomerSourceStats)
		stats.GET("/remote-assist-tickets", handler.GetRemoteAssistTicketStats)
		stats.GET("/export", exportHandler.ExportStatistics)
		stats.POST("/update-daily", handler.UpdateDailyStats)
	}
}
