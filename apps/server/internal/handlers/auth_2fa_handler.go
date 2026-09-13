package handlers

import (
	"context"
	"errors"
	"net/http"

	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
)

// auth2FAService 是 TOTP 两步验证端点的窄服务面（AuthHandler 的 authService
// 已覆盖 Login；这里补绑定/解绑/恢复码与挑战步换取会话）。
type auth2FAService interface {
	SetupTwoFactor(ctx context.Context, userID uint) (*services.TwoFactorSetup, error)
	EnableTwoFactor(ctx context.Context, userID uint, secret, code string) ([]string, error)
	DisableTwoFactor(ctx context.Context, userID uint, password, code string) error
	RegenerateRecoveryCodes(ctx context.Context, userID uint, code string) ([]string, error)
	RecoveryCodesRemaining(ctx context.Context, userID uint) (int64, error)
	VerifyTwoFactorLogin(ctx context.Context, req services.TwoFactorVerifyInput, meta services.AuthSessionMetadata) (*services.AuthResult, error)
}

// Auth2FAHandler 处理 TOTP 两步验证端点。
type Auth2FAHandler struct {
	service auth2FAService
}

// NewAuth2FAHandler creates a new Auth2FAHandler.
func NewAuth2FAHandler(service auth2FAService) *Auth2FAHandler {
	return &Auth2FAHandler{service: service}
}

type twoFactorVerifyRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
}

type twoFactorEnableRequest struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

type twoFactorDisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

type twoFactorRegenerateRequest struct {
	Code string `json:"code"`
}

// VerifyLogin godoc
// @Summary Complete two-factor login challenge
// @Tags auth
// @Accept json
// @Produce json
// @Param body body twoFactorVerifyRequest true "Challenge token and authenticator or recovery code"
// @Success 200 {object} tokenResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/2fa/verify [post]
func (h *Auth2FAHandler) VerifyLogin(c *gin.Context) {
	var req twoFactorVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}
	result, err := h.service.VerifyTwoFactorLogin(c.Request.Context(), services.TwoFactorVerifyInput{
		ChallengeToken: req.ChallengeToken,
		Code:           req.Code,
	}, authSessionMetadataFromRequest(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAuthInvalid2FAChallenge):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "登录挑战已失效，请重新登录"})
		case errors.Is(err, services.ErrAuthInvalid2FACode):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "验证码错误"})
		case errors.Is(err, services.ErrAuthUserDisabled):
			c.JSON(http.StatusForbidden, gin.H{"error": "账号已被禁用"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "验证失败"})
		}
		return
	}
	c.JSON(http.StatusOK, tokenResponse{
		Token:            result.Token,
		ExpiresIn:        result.ExpiresIn,
		RefreshToken:     result.RefreshToken,
		RefreshExpiresIn: result.RefreshExpiresIn,
		User:             mapUserResponse(result.User),
	})
}

// Setup godoc
// @Summary Generate a TOTP secret for two-factor enrollment
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} services.TwoFactorSetup
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /api/v1/auth/2fa/setup [post]
func (h *Auth2FAHandler) Setup(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	setup, err := h.service.SetupTwoFactor(c.Request.Context(), userID)
	if err != nil {
		h.writeSetupError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": setup})
}

// Enable godoc
// @Summary Enable two-factor auth with the verified secret
// @Tags auth
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body twoFactorEnableRequest true "Secret from setup and authenticator code"
// @Success 200 {object} map[string][]string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /api/v1/auth/2fa/enable [post]
func (h *Auth2FAHandler) Enable(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	var req twoFactorEnableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}
	codes, err := h.service.EnableTwoFactor(c.Request.Context(), userID, req.Secret, req.Code)
	if err != nil {
		h.writeSetupError(c, err)
		return
	}
	// 恢复码明文只在本次响应出现一次，服务端不留底。
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"recovery_codes": codes}})
}

// Disable godoc
// @Summary Disable two-factor auth
// @Tags auth
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body twoFactorDisableRequest true "Account password and authenticator or recovery code"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /api/v1/auth/2fa/disable [post]
func (h *Auth2FAHandler) Disable(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	var req twoFactorDisableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}
	if err := h.service.DisableTwoFactor(c.Request.Context(), userID, req.Password, req.Code); err != nil {
		switch {
		case errors.Is(err, services.ErrTwoFactorNotEnabled):
			c.JSON(http.StatusBadRequest, gin.H{"error": "尚未启用两步验证"})
		case errors.Is(err, services.ErrAuthInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		case errors.Is(err, services.ErrAuthInvalid2FACode):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "验证码错误"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "解绑失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "两步验证已解除"}})
}

// RecoveryCodes godoc
// @Summary Show remaining recovery code count
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]int64
// @Router /api/v1/auth/2fa/recovery-codes [get]
func (h *Auth2FAHandler) RecoveryCodes(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	remaining, err := h.service.RecoveryCodesRemaining(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"remaining": remaining}})
}

// RegenerateRecoveryCodes godoc
// @Summary Replace recovery codes with a fresh batch
// @Tags auth
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body twoFactorRegenerateRequest true "Current authenticator code"
// @Success 200 {object} map[string][]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/2fa/recovery-codes/regenerate [post]
func (h *Auth2FAHandler) RegenerateRecoveryCodes(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	var req twoFactorRegenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}
	codes, err := h.service.RegenerateRecoveryCodes(c.Request.Context(), userID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrTwoFactorNotEnabled):
			c.JSON(http.StatusBadRequest, gin.H{"error": "尚未启用两步验证"})
		case errors.Is(err, services.ErrAuthInvalid2FACode):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "验证码错误"})
		case errors.Is(err, services.ErrTwoFactorDisabled):
			c.JSON(http.StatusForbidden, gin.H{"error": "两步验证已被管理员关闭"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重置失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"recovery_codes": codes}})
}

func (h *Auth2FAHandler) writeSetupError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrTwoFactorDisabled):
		c.JSON(http.StatusForbidden, gin.H{"error": "两步验证已被管理员关闭"})
	case errors.Is(err, services.ErrTwoFactorAlreadyEnabled):
		c.JSON(http.StatusBadRequest, gin.H{"error": "已启用两步验证"})
	case errors.Is(err, services.ErrAuthInvalid2FACode):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "验证码错误"})
	case errors.Is(err, services.ErrInvalidAuthInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
	default:
		// 用户不存在与其他存储层错误统一走这里（与 AuthHandler.GetCurrentUser 口径一致）
		c.JSON(http.StatusInternalServerError, gin.H{"error": "操作失败"})
	}
}
