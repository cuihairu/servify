package handlers

import (
	"net/http"
	"strconv"

	"servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"

	"github.com/gin-gonic/gin"
)

// WebhookHandler 管理出站 Webhook 订阅与投递日志
type WebhookHandler struct {
	service webhookdelivery.HandlerService
}

func NewWebhookHandler(service webhookdelivery.HandlerService) *WebhookHandler {
	return &WebhookHandler{service: service}
}

// ListEndpoints 获取 webhook 端点列表
func (h *WebhookHandler) ListEndpoints(c *gin.Context) {
	endpoints, err := h.service.ListEndpoints(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list webhooks", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, endpoints)
}

// CreateEndpoint 创建 webhook 端点，secret 明文仅在本次响应返回
func (h *WebhookHandler) CreateEndpoint(c *gin.Context) {
	var req webhookdelivery.EndpointCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	ep, secret, err := h.service.CreateEndpoint(c.Request.Context(), &req)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to create webhook", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"endpoint": ep, "secret": secret})
}

// UpdateEndpoint 更新 webhook 端点
func (h *WebhookHandler) UpdateEndpoint(c *gin.Context) {
	id, err := parseUintParam(c)
	if err != nil {
		return
	}
	var req webhookdelivery.EndpointUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	ep, err := h.service.UpdateEndpoint(c.Request.Context(), uint(id), &req)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to update webhook", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, ep)
}

// DeleteEndpoint 删除 webhook 端点
func (h *WebhookHandler) DeleteEndpoint(c *gin.Context) {
	id, err := parseUintParam(c)
	if err != nil {
		return
	}
	if err := h.service.DeleteEndpoint(c.Request.Context(), uint(id)); err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to delete webhook", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "deleted"})
}

// RotateSecret 轮换签名密钥，明文仅在本次响应返回
func (h *WebhookHandler) RotateSecret(c *gin.Context) {
	id, err := parseUintParam(c)
	if err != nil {
		return
	}
	ep, secret, err := h.service.RotateEndpointSecret(c.Request.Context(), uint(id))
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to rotate webhook secret", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"endpoint": ep, "secret": secret})
}

// TestEndpoint 向端点同步发送一条 ping 测试事件
func (h *WebhookHandler) TestEndpoint(c *gin.Context) {
	id, err := parseUintParam(c)
	if err != nil {
		return
	}
	delivery, err := h.service.TestEndpoint(c.Request.Context(), uint(id))
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to send test event", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, delivery)
}

// ListDeliveries 分页查询投递日志（可按 endpoint_id / status 过滤）
func (h *WebhookHandler) ListDeliveries(c *gin.Context) {
	query := application.DeliveryListQuery{
		Status: c.Query("status"),
	}
	if v := c.Query("endpoint_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			query.EndpointID = uint(id)
		}
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
	deliveries, total, err := h.service.ListDeliveries(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list deliveries", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": deliveries, "total": total, "page": query.Page, "page_size": query.PageSize})
}

// Redeliver 将失败/死信投递重置为 pending 立即重投
func (h *WebhookHandler) Redeliver(c *gin.Context) {
	id, err := parseUintParam(c)
	if err != nil {
		return
	}
	delivery, err := h.service.RedeliverDelivery(c.Request.Context(), uint(id))
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to redeliver", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, delivery)
}

// ListSupportedEvents 返回可订阅事件白名单
func (h *WebhookHandler) ListSupportedEvents(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"events": h.service.SupportedEvents()})
}

func parseUintParam(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return 0, err
	}
	return id, nil
}

// RegisterWebhookRoutes 注册 webhook 管理路由
func RegisterWebhookRoutes(r *gin.RouterGroup, handler *WebhookHandler) {
	webhooks := r.Group("/webhooks")
	{
		webhooks.GET("", handler.ListEndpoints)
		webhooks.POST("", handler.CreateEndpoint)
		webhooks.PUT("/:id", handler.UpdateEndpoint)
		webhooks.DELETE("/:id", handler.DeleteEndpoint)
		webhooks.POST("/:id/secret", handler.RotateSecret)
		webhooks.POST("/:id/test", handler.TestEndpoint)
		webhooks.GET("/events", handler.ListSupportedEvents)
		webhooks.GET("/deliveries", handler.ListDeliveries)
		webhooks.POST("/deliveries/:id/redeliver", handler.Redeliver)
	}
}
