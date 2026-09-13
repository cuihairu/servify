package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/xuri/excelize/v2"
)

// SatisfactionStatsReader 是导出端点消费满意度统计的窄接口
// （由 handlers.SatisfactionService 的实现满足）。
type SatisfactionStatsReader interface {
	GetSatisfactionStats(ctx context.Context, dateFrom, dateTo *time.Time) (*services.SatisfactionStatsResponse, error)
}

// StatisticsExportHandler 统计数据导出处理器（CSV / Excel）。
type StatisticsExportHandler struct {
	statsService analyticsdelivery.HandlerService
	satisfaction SatisfactionStatsReader
	logger       *logrus.Logger
}

// NewStatisticsExportHandler 创建统计导出处理器。
func NewStatisticsExportHandler(statsService analyticsdelivery.HandlerService, satisfaction SatisfactionStatsReader, logger *logrus.Logger) *StatisticsExportHandler {
	return &StatisticsExportHandler{
		statsService: statsService,
		satisfaction: satisfaction,
		logger:       logger,
	}
}

var statisticsExportTypes = map[string]bool{
	"time_range":        true,
	"agent_performance": true,
	"ticket_category":   true,
	"ticket_priority":   true,
	"customer_source":   true,
	"satisfaction":      true,
}

const (
	exportMaxTimeRangeDays = 366
	exportDefaultDays      = 30
)

// ExportStatistics 导出统计报表
// @Summary 导出统计报表（CSV / Excel）
// @Description 按类型与格式导出统计数据：time_range（时间范围）、agent_performance（客服绩效）、
// @Description ticket_category（工单分类）、ticket_priority（工单优先级）、customer_source（客户来源）、satisfaction（满意度趋势）
// @Tags 统计
// @Produce text/csv
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Param type query string true "报表类型 time_range|agent_performance|ticket_category|ticket_priority|customer_source|satisfaction"
// @Param format query string false "导出格式 csv（默认）|xlsx"
// @Param from query string false "开始日期 YYYY-MM-DD（默认近 30 天；time_range 上限 366 天）"
// @Param to query string false "结束日期 YYYY-MM-DD"
// @Success 200 {file} file
// @Failure 400 {object} ErrorResponse
// @Router /api/statistics/export [get]
func (h *StatisticsExportHandler) ExportStatistics(c *gin.Context) {
	exportType := strings.TrimSpace(c.Query("type"))
	if !statisticsExportTypes[exportType] {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid export type", Message: "type 必须是 time_range、agent_performance、ticket_category、ticket_priority、customer_source 或 satisfaction"})
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "xlsx" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid format", Message: "format 必须是 csv 或 xlsx"})
		return
	}

	from, to, err := parseExportRange(c, exportType)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid date range", Message: err.Error()})
		return
	}

	headers, rows, err := h.buildRows(c.Request.Context(), exportType, from, to)
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to export statistics %s: %v", exportType, err)
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to export statistics", Message: err.Error()})
		return
	}

	ext := "csv"
	contentType := "text/csv; charset=utf-8"
	var payload []byte
	if format == "xlsx" {
		ext = "xlsx"
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		payload, err = buildXLSX(headers, rows)
	} else {
		payload, err = buildCSV(headers, rows)
	}
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to render %s export: %v", exportType, err)
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to render export", Message: err.Error()})
		return
	}

	filename := fmt.Sprintf("servify-%s-%s.%s", exportType, time.Now().Format("20060102_150405"), ext)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, contentType, payload)
}

// parseExportRange 解析 from/to（YYYY-MM-DD，含当日）；time_range 限制上限
// 防止导出无界区间，其余类型同样沿用。默认近 N 天。
func parseExportRange(c *gin.Context, exportType string) (time.Time, time.Time, error) {
	parseDay := func(raw string, def time.Time) (time.Time, error) {
		if strings.TrimSpace(raw) == "" {
			return def, nil
		}
		return time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), time.UTC)
	}

	now := time.Now().UTC()
	defaultTo := now.Truncate(24 * time.Hour)
	defaultFrom := defaultFromFor(defaultTo)

	from, err := parseDay(c.Query("from"), defaultFrom)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from 格式应为 YYYY-MM-DD")
	}
	to, err := parseDay(c.Query("to"), defaultTo)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to 格式应为 YYYY-MM-DD")
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to 不能早于 from")
	}
	if days := int(to.Sub(from).Hours()/24) + 1; days > exportMaxTimeRangeDays {
		return time.Time{}, time.Time{}, fmt.Errorf("导出区间最长 %d 天（当前 %d 天）", exportMaxTimeRangeDays, days)
	}
	return from, to, nil
}

func defaultFromFor(defaultTo time.Time) time.Time {
	return defaultTo.AddDate(0, 0, -(exportDefaultDays - 1))
}

// buildRows 取数并展平为表头 + 字符串行（CSV 与 XLSX 共用）。
func (h *StatisticsExportHandler) buildRows(ctx context.Context, exportType string, from, to time.Time) ([]string, [][]string, error) {
	switch exportType {
	case "time_range":
		items, err := h.statsService.GetTimeRangeStats(ctx, from, to)
		if err != nil {
			return nil, nil, err
		}
		headers := []string{"日期", "工单数", "会话数", "消息数", "解决工单数", "平均响应时间(秒)", "客户满意度"}
		rows := make([][]string, 0, len(items))
		for _, it := range items {
			rows = append(rows, []string{
				it.Date,
				strconv.FormatInt(it.Tickets, 10),
				strconv.FormatInt(it.Sessions, 10),
				strconv.FormatInt(it.Messages, 10),
				strconv.FormatInt(it.ResolvedTickets, 10),
				strconv.FormatFloat(it.AvgResponseTime, 'f', 2, 64),
				strconv.FormatFloat(it.CustomerSatisfaction, 'f', 2, 64),
			})
		}
		return headers, rows, nil

	case "agent_performance":
		items, err := h.statsService.GetAgentPerformanceStats(ctx, from, to, 0)
		if err != nil {
			return nil, nil, err
		}
		headers := []string{"坐席ID", "坐席", "部门", "工单总数", "解决工单数", "平均响应时间(秒)", "平均解决时长(秒)", "评分"}
		rows := make([][]string, 0, len(items))
		for _, it := range items {
			rows = append(rows, []string{
				strconv.FormatUint(uint64(it.AgentID), 10),
				it.AgentName,
				it.Department,
				strconv.FormatInt(it.TotalTickets, 10),
				strconv.FormatInt(it.ResolvedTickets, 10),
				strconv.FormatFloat(it.AvgResponseTime, 'f', 2, 64),
				strconv.FormatFloat(it.AvgResolutionTime, 'f', 2, 64),
				strconv.FormatFloat(it.Rating, 'f', 2, 64),
			})
		}
		return headers, rows, nil

	case "ticket_category", "ticket_priority":
		var (
			items []analyticscontract.CategoryStats
			err   error
		)
		if exportType == "ticket_category" {
			items, err = h.statsService.GetTicketCategoryStats(ctx, from, to)
		} else {
			items, err = h.statsService.GetTicketPriorityStats(ctx, from, to)
		}
		if err != nil {
			return nil, nil, err
		}
		headers, rows := categoryRows("分类", "工单数", items)
		return headers, rows, nil

	case "customer_source":
		items, err := h.statsService.GetCustomerSourceStats(ctx)
		if err != nil {
			return nil, nil, err
		}
		headers, rows := categoryRows("客户来源", "客户数", items)
		return headers, rows, nil

	case "satisfaction":
		if h.satisfaction == nil {
			return nil, nil, fmt.Errorf("satisfaction service unavailable")
		}
		stats, err := h.satisfaction.GetSatisfactionStats(ctx, &from, &to)
		if err != nil {
			return nil, nil, err
		}
		headers := []string{"日期", "评价数", "平均评分"}
		rows := make([][]string, 0, len(stats.TrendData))
		for _, it := range stats.TrendData {
			rows = append(rows, []string{
				it.Date,
				strconv.Itoa(it.Count),
				strconv.FormatFloat(it.AverageRating, 'f', 2, 64),
			})
		}
		return headers, rows, nil
	}
	return nil, nil, fmt.Errorf("unsupported export type %q", exportType)
}

func categoryRows(labelHeader, countHeader string, items []analyticscontract.CategoryStats) ([]string, [][]string) {
	headers := []string{labelHeader, countHeader}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.Category, strconv.FormatInt(it.Count, 10)})
	}
	return headers, rows
}

// buildCSV 输出带 UTF-8 BOM 的 CSV，保证 Excel 直接打开不乱码。
func buildCSV(headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	if err := w.Write(headers); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// buildXLSX 在内存中构建单工作表 Excel。
func buildXLSX(headers []string, rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	if err := f.SetSheetRow(sheet, "A1", &headers); err != nil {
		return nil, err
	}
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return nil, err
		}
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
