package handlers

import (
	"net/http"
	"strings"

	pushcontract "servify/apps/server/internal/modules/push/contract"
	pushdelivery "servify/apps/server/internal/modules/push/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// VisitorPushHandler 访客推送注册处理器（M3 移动 SDK 配套 §10 #5）：
// 挂在 /api/v1 免认证链（与访客 WS 同前缀），SDK 在 connect 后上报
// FCM/APNs token，服务端按 session 绑定租户 scope 供下发侧消费。
type VisitorPushHandler struct {
	pushService pushdelivery.PushRegistrationService
	logger      *logrus.Logger
}

// NewVisitorPushHandler 创建访客推送注册处理器
func NewVisitorPushHandler(pushService pushdelivery.PushRegistrationService, logger *logrus.Logger) *VisitorPushHandler {
	return &VisitorPushHandler{
		pushService: pushService,
		logger:      logger,
	}
}

// RegisterPushToken 注册访客推送 token
// @Summary 注册访客推送 token
// @Description 访客（免认证）上报 FCM/APNs 推送 token；按 session 归属租户 scope，同 session+platform 幂等（重复注册保活、换 token 更新）
// @Tags 推送
// @Accept json
// @Produce json
// @Param registration body contract.RegisterPushTokenRequest true "推送注册信息"
// @Success 201 {object} contract.PushTokenRegistration
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/push/register [post]
func (h *VisitorPushHandler) RegisterPushToken(c *gin.Context) {
	var req pushcontract.RegisterPushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	registration, err := h.pushService.RegisterPushToken(c.Request.Context(), &req)
	if err != nil {
		if strings.Contains(err.Error(), "session not found") {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "Session not found",
				Message: err.Error(),
			})
			return
		}
		if strings.Contains(err.Error(), "unsupported platform") {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "Unsupported platform",
				Message: err.Error(),
			})
			return
		}
		h.logger.Errorf("Failed to register push token: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to register push token",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, registration)
}
