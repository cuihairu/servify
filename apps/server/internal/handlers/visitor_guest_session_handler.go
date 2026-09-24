package handlers

import (
	"net/http"
	"strings"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// GuestSessionRequest 访客 token 签发请求体
type GuestSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// GuestSessionResponse 访客 token 签发响应：access_token 用于 WS 握手
// /api/v1/ws?access_token=，expires_at 为过期 Unix 秒。
type GuestSessionResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int64  `json:"expires_at"`
}

// VisitorGuestSessionHandler 访客会话 token 签发处理器（M3 移动 SDK 配套
// §10 #2 / 设计文档 D6）：挂在 /api/v1 service 鉴权链（宿主后端持
// service API key 调用），为访客会话换取绑定 session_id 的短期 HS256
// token。开启 security.guest_token.required 后 WS 握手必须携带它签发的
// token；关闭时端点仍可用（token 由 hub 侧按配置决定是否校验）。
type VisitorGuestSessionHandler struct {
	issuer conversationdelivery.GuestTokenIssuer
	logger *logrus.Logger
}

// NewVisitorGuestSessionHandler 创建访客 token 签发处理器
func NewVisitorGuestSessionHandler(issuer conversationdelivery.GuestTokenIssuer, logger *logrus.Logger) *VisitorGuestSessionHandler {
	return &VisitorGuestSessionHandler{issuer: issuer, logger: logger}
}

// Issue 签发绑定 session_id 的访客 token
// @Summary 签发访客 WS 握手 token
// @Description 为访客会话签发绑定 session_id 的短期 HS256 token（宿主后端持 service API key 调用）；token 不要求会话行已存在（行在首条消息持久化时建），有效期由 security.guest_token.ttl 决定（默认 24h）
// @Tags 会话
// @Accept json
// @Produce json
// @Param request body GuestSessionRequest true "访客会话标识"
// @Success 201 {object} GuestSessionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /api/v1/guest/session [post]
func (h *VisitorGuestSessionHandler) Issue(c *gin.Context) {
	var req GuestSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: "session_id is required",
		})
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid session_id",
			Message: "session_id must not be blank",
		})
		return
	}

	token, expiresAt, err := h.issuer.IssueGuestToken(sessionID)
	if err != nil {
		if h.logger != nil {
			h.logger.WithError(err).WithField("session_id", sessionID).Error("guest token issuance failed")
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Issuance failed",
			Message: "failed to issue guest token",
		})
		return
	}

	c.JSON(http.StatusCreated, GuestSessionResponse{
		AccessToken: token,
		TokenType:   "bearer",
		ExpiresAt:   expiresAt,
	})
}
