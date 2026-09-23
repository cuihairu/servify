package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	auditplatform "servify/apps/server/internal/platform/audit"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type ticketResponse struct {
	ID           uint                   `json:"id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description,omitempty"`
	AISummary    string                 `json:"ai_summary,omitempty"`
	CustomerID   uint                   `json:"customer_id"`
	CustomerName string                 `json:"customer_name,omitempty"`
	AgentID      *uint                  `json:"agent_id,omitempty"`
	AgentName    string                 `json:"agent_name,omitempty"`
	SessionID    *string                `json:"session_id,omitempty"`
	Category     string                 `json:"category"`
	Priority     string                 `json:"priority"`
	Status       string                 `json:"status"`
	Source       string                 `json:"source,omitempty"`
	Tags         string                 `json:"tags"`
	TagList      []string               `json:"tag_list,omitempty"`
	CustomFields map[string]interface{} `json:"custom_fields,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	ResolvedAt   *time.Time             `json:"resolved_at,omitempty"`
	ClosedAt     *time.Time             `json:"closed_at,omitempty"`
}

func buildTicketResponse(ticket *models.Ticket) *ticketResponse {
	if ticket == nil {
		return nil
	}

	resp := &ticketResponse{
		ID:           ticket.ID,
		Title:        ticket.Title,
		Description:  ticket.Description,
		AISummary:    ticket.AISummary,
		CustomerID:   ticket.CustomerID,
		AgentID:      ticket.AgentID,
		SessionID:    ticket.SessionID,
		Category:     ticket.Category,
		Priority:     ticket.Priority,
		Status:       ticket.Status,
		Source:       ticket.Source,
		Tags:         ticket.Tags,
		TagList:      splitCSV(ticket.Tags),
		CustomFields: buildTicketCustomFields(ticket),
		CreatedAt:    ticket.CreatedAt,
		UpdatedAt:    ticket.UpdatedAt,
		ResolvedAt:   ticket.ResolvedAt,
		ClosedAt:     ticket.ClosedAt,
	}

	if ticket.Customer.Name != "" {
		resp.CustomerName = ticket.Customer.Name
	}
	if ticket.Agent != nil && ticket.Agent.Name != "" {
		resp.AgentName = ticket.Agent.Name
	}

	return resp
}

func buildTicketResponses(items []models.Ticket) []ticketResponse {
	out := make([]ticketResponse, 0, len(items))
	for i := range items {
		if resp := buildTicketResponse(&items[i]); resp != nil {
			out = append(out, *resp)
		}
	}
	return out
}

func buildTicketCustomFields(ticket *models.Ticket) map[string]interface{} {
	if ticket == nil || len(ticket.CustomFieldValues) == 0 {
		return nil
	}

	fields := make(map[string]interface{}, len(ticket.CustomFieldValues))
	for _, item := range ticket.CustomFieldValues {
		if item.CustomField.Key == "" {
			continue
		}
		fields[item.CustomField.Key] = item.Value
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func splitCSV(tags string) []string {
	if strings.TrimSpace(tags) == "" {
		return nil
	}

	raw := strings.Split(tags, ",")
	out := make([]string, 0, len(raw))
	for _, tag := range raw {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TicketHandler 工单处理器
type TicketHandler struct {
	ticketService ticketdelivery.HandlerService
	logger        *logrus.Logger
}

// NewTicketHandler 创建工单处理器
func NewTicketHandler(ticketService ticketdelivery.HandlerService, logger *logrus.Logger) *TicketHandler {
	return &TicketHandler{
		ticketService: ticketService,
		logger:        logger,
	}
}

// CreateTicket 创建工单
// @Summary 创建工单
// @Description 创建新的客服工单
// @Tags 工单
// @Accept json
// @Produce json
// @Param ticket body contract.CreateTicketRequest true "工单信息"
// @Success 201 {object} models.Ticket
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets [post]
func (h *TicketHandler) CreateTicket(c *gin.Context) {
	var req ticketcontract.CreateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	ticket, err := h.ticketService.CreateTicket(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorf("Failed to create ticket: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to create ticket",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, ticket)
}

// GetTicket 获取工单详情
// @Summary 获取工单详情
// @Description 根据ID获取工单的详细信息
// @Tags 工单
// @Accept json
// @Produce json
// @Param id path int true "工单ID"
// @Success 200 {object} models.Ticket
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/tickets/{id} [get]
func (h *TicketHandler) GetTicket(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid ticket ID",
			Message: "ID must be a valid number",
		})
		return
	}

	ticket, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(id))
	if err != nil {
		h.logger.Errorf("Failed to get ticket %d: %v", id, err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "Ticket not found",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, buildTicketResponse(ticket))
}

// UpdateTicket 更新工单
// @Summary 更新工单
// @Description 更新工单信息
// @Tags 工单
// @Accept json
// @Produce json
// @Param id path int true "工单ID"
// @Param ticket body contract.UpdateTicketRequest true "更新信息"
// @Success 200 {object} models.Ticket
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/{id} [put]
func (h *TicketHandler) UpdateTicket(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid ticket ID",
			Message: "ID must be a valid number",
		})
		return
	}

	var req ticketcontract.UpdateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	// 从上下文获取用户ID（假设已经通过中间件设置）
	userID, exists := c.Get("user_id")
	if !exists {
		userID = uint(0) // 系统操作
	}

	before, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(id))
	if err == nil && before != nil {
		auditplatform.SetBefore(c, before)
	}

	ticket, err := h.ticketService.UpdateTicket(c.Request.Context(), uint(id), &req, userID.(uint))
	if err != nil {
		h.logger.Errorf("Failed to update ticket %d: %v", id, err)
		statusCode := ticketErrorToStatusCode(err)
		c.JSON(statusCode, ErrorResponse{
			Error:   "Failed to update ticket",
			Message: err.Error(),
		})
		return
	}

	auditplatform.SetAfter(c, ticket)
	c.JSON(http.StatusOK, buildTicketResponse(ticket))
}

// ListTickets 获取工单列表
// @Summary 获取工单列表
// @Description 获取工单列表，支持分页和过滤
// @Tags 工单
// @Accept json
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页大小"
// @Param status query []string false "状态过滤"
// @Param priority query []string false "优先级过滤"
// @Param category query []string false "分类过滤"
// @Param agent_id query int false "客服ID过滤"
// @Param customer_id query int false "客户ID过滤"
// @Param search query string false "搜索关键词"
// @Param sort_by query string false "排序字段"
// @Param sort_order query string false "排序方向"
// @Success 200 {object} PaginatedResponse{data=[]models.Ticket}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets [get]
func (h *TicketHandler) ListTickets(c *gin.Context) {
	var req ticketcontract.ListTicketRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid query parameters",
			Message: err.Error(),
		})
		return
	}

	req.CustomFieldFilters = extractCustomFieldFilters(c)

	tickets, total, err := h.ticketService.ListTickets(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorf("Failed to list tickets: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to list tickets",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     buildTicketResponses(tickets),
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// ExportTicketsCSV 导出工单 CSV（包含自定义字段列）
// @Summary 导出工单 CSV
// @Description 导出工单数据为 CSV，支持与 ListTickets 相同的过滤参数，并额外支持 cf.<key>=<value> 过滤自定义字段
// @Tags 工单
// @Accept json
// @Produce text/csv
// @Param limit query int false "最多导出条数（默认 1000，最大 5000）"
// @Success 200 {string} string "csv"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/export [get]
func (h *TicketHandler) ExportTicketsCSV(c *gin.Context) {
	var req ticketcontract.ListTicketRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid query parameters", Message: err.Error()})
		return
	}
	req.CustomFieldFilters = extractCustomFieldFilters(c)

	limit := 1000
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	req.Page = 1
	req.PageSize = limit

	fields, err := h.ticketService.ListTicketCustomFields(c.Request.Context(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load custom fields", Message: err.Error()})
		return
	}

	tickets, _, err := h.ticketService.ListTickets(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to export tickets", Message: err.Error()})
		return
	}

	header := []string{"id", "title", "status", "priority", "category", "customer_id", "agent_id", "tags", "created_at", "updated_at"}
	for _, f := range fields {
		header = append(header, "cf."+f.Key)
	}

	var buf bytes.Buffer
	w := newCSVWriter(&buf)
	// 表头为定长列（基础 10 列 + 自定义字段名），远小于 csv.Writer 内部
	// 4KB bufio 缓冲，Write 不会失败；行写入与 Flush 错误在下方统一处理。
	_ = w.Write(header)
	for _, t := range tickets {
		cf := make(map[string]string)
		for _, v := range t.CustomFieldValues {
			if v.CustomField.Key != "" {
				cf[v.CustomField.Key] = v.Value
			}
		}
		agentID := ""
		if t.AgentID != nil {
			agentID = fmt.Sprintf("%d", *t.AgentID)
		}
		row := []string{
			fmt.Sprintf("%d", t.ID),
			t.Title,
			t.Status,
			t.Priority,
			t.Category,
			fmt.Sprintf("%d", t.CustomerID),
			agentID,
			t.Tags,
			t.CreatedAt.Format(time.RFC3339),
			t.UpdatedAt.Format(time.RFC3339),
		}
		for _, f := range fields {
			row = append(row, cf[f.Key])
		}
		if err := w.Write(row); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to write csv", Message: err.Error()})
			return
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to write csv", Message: err.Error()})
		return
	}

	filename := fmt.Sprintf("tickets_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// AssignTicket 分配工单
// @Summary 分配工单
// @Description 将工单分配给指定客服
// @Tags 工单
// @Accept json
// @Produce json
// @Param id path int true "工单ID"
// @Param assignment body map[string]uint true "分配信息"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/{id}/assign [post]
func (h *TicketHandler) AssignTicket(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid ticket ID",
			Message: "ID must be a valid number",
		})
		return
	}

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

	// 从上下文获取分配者ID
	assignerID, exists := c.Get("user_id")
	if !exists {
		assignerID = uint(0)
	}

	before, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(id))
	if err == nil && before != nil {
		auditplatform.SetBefore(c, before)
	}

	if err := h.ticketService.AssignTicket(c.Request.Context(), uint(id), req.AgentID, assignerID.(uint)); err != nil {
		h.logger.Errorf("Failed to assign ticket %d to agent %d: %v", id, req.AgentID, err)
		statusCode := ticketErrorToStatusCode(err)
		c.JSON(statusCode, ErrorResponse{
			Error:   "Failed to assign ticket",
			Message: err.Error(),
		})
		return
	}

	if after, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(id)); err == nil && after != nil {
		auditplatform.SetAfter(c, after)
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "Ticket assigned successfully",
		"ticket_id": id,
		"agent_id":  req.AgentID,
	})
}

// AddComment 添加工单评论
// @Summary 添加工单评论
// @Description 为工单添加评论或内部备注
// @Tags 工单
// @Accept json
// @Produce json
// @Param id path int true "工单ID"
// @Param comment body map[string]string true "评论信息"
// @Success 201 {object} models.TicketComment
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/{id}/comments [post]
func (h *TicketHandler) AddComment(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid ticket ID",
			Message: "ID must be a valid number",
		})
		return
	}

	var req struct {
		Content string `json:"content" binding:"required"`
		Type    string `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error:   "Unauthorized",
			Message: "User not authenticated",
		})
		return
	}

	comment, err := h.ticketService.AddComment(c.Request.Context(), uint(id), userID.(uint), req.Content, req.Type)
	if err != nil {
		h.logger.Errorf("Failed to add comment to ticket %d: %v", id, err)
		statusCode := ticketErrorToStatusCode(err)
		c.JSON(statusCode, ErrorResponse{
			Error:   "Failed to add comment",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, comment)
}

// CloseTicket 关闭工单
// @Summary 关闭工单
// @Description 关闭指定的工单
// @Tags 工单
// @Accept json
// @Produce json
// @Param id path int true "工单ID"
// @Param close_info body map[string]string true "关闭信息"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/{id}/close [post]
func (h *TicketHandler) CloseTicket(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid ticket ID",
			Message: "ID must be a valid number",
		})
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error:   "Unauthorized",
			Message: "User not authenticated",
		})
		return
	}

	before, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(id))
	if err == nil && before != nil {
		auditplatform.SetBefore(c, before)
	}

	if err := h.ticketService.CloseTicket(c.Request.Context(), uint(id), userID.(uint), req.Reason); err != nil {
		h.logger.Errorf("Failed to close ticket %d: %v", id, err)
		statusCode := ticketErrorToStatusCode(err)
		c.JSON(statusCode, ErrorResponse{
			Error:   "Failed to close ticket",
			Message: err.Error(),
		})
		return
	}

	if after, err := h.ticketService.GetTicketByID(c.Request.Context(), uint(id)); err == nil && after != nil {
		auditplatform.SetAfter(c, after)
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "Ticket closed successfully",
		"ticket_id": id,
	})
}

// GetTicketStats 获取工单统计
// @Summary 获取工单统计
// @Description 获取工单相关的统计数据
// @Tags 工单
// @Accept json
// @Produce json
// @Param agent_id query int false "客服ID，用于获取特定客服的统计"
// @Success 200 {object} contract.TicketStats
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/stats [get]
func (h *TicketHandler) GetTicketStats(c *gin.Context) {
	var agentID *uint
	if agentIDStr := c.Query("agent_id"); agentIDStr != "" {
		if id, err := strconv.ParseUint(agentIDStr, 10, 32); err == nil {
			agentIDValue := uint(id)
			agentID = &agentIDValue
		}
	}

	stats, err := h.ticketService.GetTicketStats(c.Request.Context(), agentID)
	if err != nil {
		h.logger.Errorf("Failed to get ticket stats: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to get ticket statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// BulkUpdateTickets 批量更新工单
// @Summary 批量更新工单
// @Description 批量更新工单（状态/标签/指派/取消指派）
// @Tags 工单
// @Accept json
// @Produce json
// @Param payload body contract.BulkUpdateTicketRequest true "批量更新请求"
// @Success 200 {object} contract.BulkUpdateResult
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/tickets/bulk [post]
func (h *TicketHandler) BulkUpdateTickets(c *gin.Context) {
	var req ticketcontract.BulkUpdateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		userID = uint(0)
	}

	result, err := h.ticketService.BulkUpdateTickets(c.Request.Context(), &req, userID.(uint))
	if err != nil {
		h.logger.Errorf("Failed to bulk update tickets: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to bulk update tickets",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// RegisterTicketRoutes 注册工单相关路由
func RegisterTicketRoutes(r *gin.RouterGroup, handler *TicketHandler) {
	tickets := r.Group("/tickets")
	{
		tickets.POST("", handler.CreateTicket)
		tickets.POST("/bulk", handler.BulkUpdateTickets)
		tickets.GET("", handler.ListTickets)
		tickets.GET("/export", handler.ExportTicketsCSV)
		tickets.GET("/stats", handler.GetTicketStats)
		tickets.GET("/:id", handler.GetTicket)
		tickets.PUT("/:id", handler.UpdateTicket)
		tickets.POST("/:id/assign", handler.AssignTicket)
		tickets.POST("/:id/comments", handler.AddComment)
		tickets.POST("/:id/close", handler.CloseTicket)
		tickets.GET("/:id/conversations", handler.GetRelatedConversations)
	}
}

// GetRelatedConversations 获取工单关联的会话列表
func (h *TicketHandler) GetRelatedConversations(c *gin.Context) {
	idStr := c.Param("id")
	ticketID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid ticket ID",
			Message: err.Error(),
		})
		return
	}

	sessions, err := h.ticketService.GetRelatedConversations(c.Request.Context(), uint(ticketID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to load related conversations",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": sessions,
	})
}

func extractCustomFieldFilters(c *gin.Context) map[string]string {
	out := map[string]string{}
	q := c.Request.URL.Query()
	for k, vals := range q {
		if !strings.HasPrefix(k, "cf.") && !strings.HasPrefix(k, "cf_") {
			continue
		}
		key := strings.TrimPrefix(k, "cf.")
		key = strings.TrimPrefix(key, "cf_")
		if key == "" || len(vals) == 0 {
			continue
		}
		val := strings.TrimSpace(vals[0])
		if val == "" {
			continue
		}
		out[key] = val
	}
	return out
}

// ticketErrorToStatusCode 根据错误内容映射HTTP状态码
func ticketErrorToStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	errMsg := strings.ToLower(err.Error())

	// 404 Not Found
	if strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "record not found") {
		return http.StatusNotFound
	}

	// 400 Bad Request - 验证错误
	if strings.Contains(errMsg, "required") ||
		strings.Contains(errMsg, "invalid") ||
		strings.Contains(errMsg, "must be") ||
		strings.Contains(errMsg, "validation") {
		return http.StatusBadRequest
	}

	// 409 Conflict - 状态冲突、资源不可用
	if strings.Contains(errMsg, "already") ||
		strings.Contains(errMsg, "conflict") ||
		strings.Contains(errMsg, "not available") ||
		strings.Contains(errMsg, "status transition") {
		return http.StatusConflict
	}

	// 403 Forbidden - 权限不足
	if strings.Contains(errMsg, "permission") ||
		strings.Contains(errMsg, "forbidden") ||
		strings.Contains(errMsg, "unauthorized") {
		return http.StatusForbidden
	}

	// 默认500
	return http.StatusInternalServerError
}
