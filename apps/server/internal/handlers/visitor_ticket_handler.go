package handlers

import (
	"net/http"
	"strings"

	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// VisitorTicketHandler 访客工单处理器（M3 移动 SDK 配套 §10 #4）：
// 挂在 /api/v1 免认证链（与访客 WS 同前缀），从会话页发起工单创建，
// ai_summary 携带 AI 会话摘要供坐席侧直接了解上下文。
type VisitorTicketHandler struct {
	visitorService ticketdelivery.VisitorTicketService
	logger         *logrus.Logger
}

// NewVisitorTicketHandler 创建访客工单处理器
func NewVisitorTicketHandler(visitorService ticketdelivery.VisitorTicketService, logger *logrus.Logger) *VisitorTicketHandler {
	return &VisitorTicketHandler{
		visitorService: visitorService,
		logger:         logger,
	}
}

// CreateVisitorTicket 创建访客工单
// @Summary 创建访客工单
// @Description 访客（免认证）从会话页创建工单；按 session 归属租户 scope，携带 AI 会话摘要
// @Tags 工单
// @Accept json
// @Produce json
// @Param ticket body contract.CreateVisitorTicketRequest true "访客工单信息"
// @Success 201 {object} models.Ticket
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/tickets [post]
func (h *VisitorTicketHandler) CreateVisitorTicket(c *gin.Context) {
	var req ticketcontract.CreateVisitorTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	ticket, err := h.visitorService.CreateVisitorTicket(c.Request.Context(), &req)
	if err != nil {
		if strings.Contains(err.Error(), "session not found") {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "Session not found",
				Message: err.Error(),
			})
			return
		}
		h.logger.Errorf("Failed to create visitor ticket: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to create visitor ticket",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, ticket)
}
