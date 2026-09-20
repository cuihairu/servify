package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	auditplatform "servify/apps/server/internal/platform/audit"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/usersecurity"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type UserSecurityHandler struct {
	service   *usersecurity.Service
	jwtSecret string
	logger    *logrus.Logger
	policy    sessionRiskPolicy
	resolver  *configscope.Resolver
	ipIntel   sessionIPIntelligence
}

type batchRevokeTokensRequest struct {
	UserIDs []uint `json:"user_ids" binding:"required,min=1"`
}

type userSecurityPreviewItem struct {
	UserID           uint   `json:"user_id"`
	Username         string `json:"username"`
	Name             string `json:"name"`
	Role             string `json:"role"`
	Status           string `json:"status"`
	TokenVersion     int    `json:"token_version"`
	NextTokenVersion int    `json:"next_token_version"`
	LastLogin        any    `json:"last_login"`
	TokenValidAfter  any    `json:"token_valid_after"`
}

type revokeSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type revokeTokenRequest struct {
	Token  string `json:"token" binding:"required"`
	Reason string `json:"reason"`
}

type revokeAllSessionsRequest struct {
	ExceptSessionID string `json:"except_session_id"`
	Reason          string `json:"reason"`
}

type revokedTokenListItem struct {
	JTI       string `json:"jti"`
	UserID    uint   `json:"user_id"`
	SessionID string `json:"session_id"`
	TokenUse  string `json:"token_use"`
	Reason    string `json:"reason"`
	ExpiresAt any    `json:"expires_at"`
	RevokedAt any    `json:"revoked_at"`
}

func NewUserSecurityHandler(service *usersecurity.Service, logger *logrus.Logger) *UserSecurityHandler {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &UserSecurityHandler{service: service, logger: logger, policy: defaultSessionRiskPolicy(), ipIntel: heuristicSessionIPIntelligence{}}
}

func (h *UserSecurityHandler) WithJWTSecret(secret string) *UserSecurityHandler {
	if h != nil {
		h.jwtSecret = strings.TrimSpace(secret)
	}
	return h
}

func (h *UserSecurityHandler) WithSessionRiskResolver(resolver *configscope.Resolver) *UserSecurityHandler {
	if h != nil {
		h.resolver = resolver
	}
	return h
}

func (h *UserSecurityHandler) WithSessionIPIntelligence(provider sessionIPIntelligence) *UserSecurityHandler {
	if h != nil && provider != nil {
		h.ipIntel = provider
	}
	return h
}

// RevokeTokens 强制失效用户已有 token
// @Summary 强制失效用户 token
// @Description 提升 token_version 并刷新 token_valid_after，使该用户旧 token 失效
// @Tags 安全管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/{id}/revoke-tokens [post]
func (h *UserSecurityHandler) RevokeTokens(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid user ID",
			Message: "ID must be a valid number",
		})
		return
	}

	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), uint(id))
	if err != nil {
		h.logger.Errorf("Failed to load user %d: %v", id, err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "User not found",
			Message: err.Error(),
		})
		return
	}
	auditplatform.SetBefore(c, gin.H{
		"user_id":           user.ID,
		"role":              user.Role,
		"status":            user.Status,
		"token_version":     user.TokenVersion,
		"token_valid_after": user.TokenValidAfter,
		"last_login":        user.LastLogin,
	})

	version, err := h.service.RevokeTokens(c.Request.Context(), uint(id))
	if err != nil {
		h.logger.Errorf("Failed to revoke user %d tokens: %v", id, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to revoke user tokens",
			Message: err.Error(),
		})
		return
	}
	updated, err := h.service.GetUser(c.Request.Context(), uint(id))
	if err == nil && updated != nil {
		auditplatform.SetAfter(c, gin.H{
			"user_id":           updated.ID,
			"role":              updated.Role,
			"status":            updated.Status,
			"token_version":     updated.TokenVersion,
			"token_valid_after": updated.TokenValidAfter,
			"last_login":        updated.LastLogin,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "User tokens revoked successfully",
		"user_id":       id,
		"role":          user.Role,
		"token_version": version,
	})
}

// BatchRevokeTokens 批量强制失效用户已有 token
// @Summary 批量强制失效用户 token
// @Description 批量提升 token_version 并刷新 token_valid_after，使这些用户旧 token 失效
// @Tags 安全管理
// @Accept json
// @Produce json
// @Param payload body object true "用户 ID 列表"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/revoke-tokens [post]
func (h *UserSecurityHandler) BatchRevokeTokens(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	var req batchRevokeTokensRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	before := make([]gin.H, 0, len(req.UserIDs))
	for _, userID := range req.UserIDs {
		user, err := h.service.GetUser(c.Request.Context(), userID)
		if err != nil {
			h.logger.Errorf("Failed to load user %d: %v", userID, err)
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "User not found",
				Message: err.Error(),
			})
			return
		}
		before = append(before, gin.H{
			"user_id":           user.ID,
			"role":              user.Role,
			"status":            user.Status,
			"token_version":     user.TokenVersion,
			"token_valid_after": user.TokenValidAfter,
			"last_login":        user.LastLogin,
		})
	}
	auditplatform.SetBefore(c, before)

	versions, err := h.service.BatchRevokeTokens(c.Request.Context(), req.UserIDs)
	if err != nil {
		h.logger.Errorf("Failed to batch revoke user tokens: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to batch revoke user tokens",
			Message: err.Error(),
		})
		return
	}

	after := make([]gin.H, 0, len(req.UserIDs))
	results := make([]gin.H, 0, len(req.UserIDs))
	for _, userID := range req.UserIDs {
		user, err := h.service.GetUser(c.Request.Context(), userID)
		if err == nil && user != nil {
			after = append(after, gin.H{
				"user_id":           user.ID,
				"role":              user.Role,
				"status":            user.Status,
				"token_version":     user.TokenVersion,
				"token_valid_after": user.TokenValidAfter,
				"last_login":        user.LastLogin,
			})
		}
		results = append(results, gin.H{
			"user_id":       userID,
			"token_version": versions[userID],
		})
	}
	auditplatform.SetAfter(c, after)

	c.JSON(http.StatusOK, gin.H{
		"message": "User tokens revoked successfully",
		"count":   len(results),
		"items":   results,
	})
}

// QueryUsersSecurity 批量查询用户安全状态预览
// @Summary 批量查询用户安全状态预览
// @Description 返回多个用户当前安全状态，并给出下一次 revoke 后的 token_version 预览
// @Tags 安全管理
// @Accept json
// @Produce json
// @Param payload body object true "用户 ID 列表"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/query [post]
func (h *UserSecurityHandler) QueryUsersSecurity(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	var req batchRevokeTokensRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	users, err := h.service.GetUsers(c.Request.Context(), req.UserIDs)
	if err != nil {
		h.logger.Errorf("Failed to query user security state: %v", err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "User not found",
			Message: err.Error(),
		})
		return
	}

	items := make([]userSecurityPreviewItem, 0, len(users))
	for _, user := range users {
		items = append(items, userSecurityPreviewItem{
			UserID:           user.ID,
			Username:         user.Username,
			Name:             user.Name,
			Role:             user.Role,
			Status:           user.Status,
			TokenVersion:     user.TokenVersion,
			NextTokenVersion: user.TokenVersion + 1,
			LastLogin:        user.LastLogin,
			TokenValidAfter:  user.TokenValidAfter,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"count": len(items),
		"items": items,
	})
}

// ListUserSessions 获取用户 auth session 列表
// @Summary 获取用户 auth session 列表
// @Description 返回用户当前 auth session 状态，用于更细粒度 session 失效操作
// @Tags 安全管理
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/{id}/sessions [get]
func (h *UserSecurityHandler) ListUserSessions(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid user ID",
			Message: "ID must be a valid number",
		})
		return
	}
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	if _, err := h.service.GetUser(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "User not found",
			Message: err.Error(),
		})
		return
	}

	sessions, err := h.service.ListUserSessions(c.Request.Context(), uint(id))
	if err != nil {
		h.logger.Errorf("Failed to list sessions for user %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to list user sessions",
			Message: err.Error(),
		})
		return
	}

	policy := h.sessionRiskPolicy(c.Request.Context())
	riskContext := buildSessionRiskContext(sessions, policy, h.ipIntel)
	items := make([]gin.H, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, mapSessionResponse(session, false, riskContext, policy, h.ipIntel))
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id": uint(id),
		"count":   len(items),
		"items":   items,
	})
}

func (h *UserSecurityHandler) sessionRiskPolicy(ctx context.Context) sessionRiskPolicy {
	if h != nil && h.resolver != nil {
		return sessionRiskPolicyFromConfig(h.resolver.ResolveSessionRisk(ctx, nil))
	}
	if h == nil {
		return defaultSessionRiskPolicy()
	}
	return h.policy
}

// RevokeSession 失效单个 auth session
// @Summary 失效单个 auth session
// @Description 吊销单个登录/刷新 session，使其后续 token 校验失败
// @Tags 安全管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param payload body object true "session_id"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/{id}/sessions/revoke [post]
func (h *UserSecurityHandler) RevokeSession(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid user ID",
			Message: "ID must be a valid number",
		})
		return
	}
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	var req revokeSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	sessions, err := h.service.ListUserSessions(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "User not found",
			Message: err.Error(),
		})
		return
	}
	for _, session := range sessions {
		if session.ID == req.SessionID {
			auditplatform.SetBefore(c, gin.H{
				"user_id":           id,
				"session_id":        session.ID,
				"status":            session.Status,
				"token_version":     session.TokenVersion,
				"last_refreshed_at": session.LastRefreshedAt,
				"revoked_at":        session.RevokedAt,
			})
			break
		}
	}

	session, err := h.service.RevokeSession(c.Request.Context(), uint(id), req.SessionID)
	if err != nil {
		h.logger.Errorf("Failed to revoke session %s for user %d: %v", req.SessionID, id, err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "Session not found",
			Message: err.Error(),
		})
		return
	}
	auditplatform.SetAfter(c, gin.H{
		"user_id":           id,
		"session_id":        session.ID,
		"status":            session.Status,
		"token_version":     session.TokenVersion,
		"last_refreshed_at": session.LastRefreshedAt,
		"revoked_at":        session.RevokedAt,
	})

	c.JSON(http.StatusOK, gin.H{
		"message":       "User session revoked successfully",
		"user_id":       uint(id),
		"session_id":    session.ID,
		"token_version": session.TokenVersion,
		"status":        session.Status,
	})
}

// RevokeAllSessions 失效用户的全部 auth session
// @Summary 失效用户全部 auth session
// @Description 吊销指定用户的全部活跃 session，可选保留一个 session
// @Tags 安全管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param payload body object false "except_session_id"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/{id}/sessions/revoke-all [post]
func (h *UserSecurityHandler) RevokeAllSessions(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid user ID",
			Message: "ID must be a valid number",
		})
		return
	}
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	if _, err := h.service.GetUser(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "User not found",
			Message: err.Error(),
		})
		return
	}

	var req revokeAllSessionsRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "Invalid request body",
				Message: err.Error(),
			})
			return
		}
	}
	req.ExceptSessionID = strings.TrimSpace(req.ExceptSessionID)

	beforeSessions, err := h.service.ListUserSessions(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to list user sessions",
			Message: err.Error(),
		})
		return
	}
	before := make([]gin.H, 0, len(beforeSessions))
	for _, session := range beforeSessions {
		if strings.EqualFold(session.Status, "active") && session.RevokedAt == nil && session.ID != req.ExceptSessionID {
			before = append(before, gin.H{
				"user_id":           id,
				"session_id":        session.ID,
				"status":            session.Status,
				"token_version":     session.TokenVersion,
				"last_refreshed_at": session.LastRefreshedAt,
				"revoked_at":        session.RevokedAt,
			})
		}
	}
	auditplatform.SetBefore(c, before)

	result, err := h.service.RevokeAllSessions(c.Request.Context(), uint(id), req.ExceptSessionID)
	if err != nil {
		h.logger.Errorf("Failed to revoke all sessions for user %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to revoke user sessions",
			Message: err.Error(),
		})
		return
	}

	after := make([]gin.H, 0, len(result.Sessions))
	items := make([]gin.H, 0, len(result.Sessions))
	for _, session := range result.Sessions {
		after = append(after, gin.H{
			"user_id":           id,
			"session_id":        session.ID,
			"status":            session.Status,
			"token_version":     session.TokenVersion,
			"last_refreshed_at": session.LastRefreshedAt,
			"revoked_at":        session.RevokedAt,
		})
		items = append(items, gin.H{
			"session_id":    session.ID,
			"status":        session.Status,
			"token_version": session.TokenVersion,
			"revoked_at":    session.RevokedAt,
		})
	}
	auditplatform.SetAfter(c, after)

	c.JSON(http.StatusOK, gin.H{
		"message":           "User sessions revoked successfully",
		"user_id":           uint(id),
		"count":             result.Count,
		"except_session_id": req.ExceptSessionID,
		"items":             items,
	})
}

// GetUserSecurity 获取用户安全状态
// @Summary 获取用户安全状态
// @Description 返回用户当前状态、token_version、token_valid_after 和最近登录时间
// @Tags 安全管理
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/users/{id} [get]
func (h *UserSecurityHandler) GetUserSecurity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid user ID",
			Message: "ID must be a valid number",
		})
		return
	}

	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), uint(id))
	if err != nil {
		h.logger.Errorf("Failed to load user %d security state: %v", id, err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "User not found",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":           user.ID,
		"role":              user.Role,
		"status":            user.Status,
		"token_version":     user.TokenVersion,
		"token_valid_after": user.TokenValidAfter,
		"last_login":        user.LastLogin,
	})
}

// RevokeToken 显式吊销单个 JWT
// @Summary 显式吊销单个 JWT
// @Description 将指定 JWT 的 jti 加入 revoke list，使其在过期前立即失效
// @Tags 安全管理
// @Accept json
// @Produce json
// @Param payload body object true "token"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/security/tokens/revoke [post]
func (h *UserSecurityHandler) RevokeToken(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}
	if strings.TrimSpace(h.jwtSecret) == "" {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "JWT secret unavailable",
			Message: "jwt secret not configured",
		})
		return
	}

	var req revokeTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	result, err := h.service.RevokeJWT(c.Request.Context(), req.Token, h.jwtSecret, req.Reason)
	if err != nil {
		if usersecurity.IsNotFound(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "Token target not found",
				Message: "token target is outside request scope or no longer exists",
			})
			return
		}
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Failed to revoke token",
			Message: err.Error(),
		})
		return
	}
	auditplatform.SetAfter(c, gin.H{
		"jti":        result.JTI,
		"user_id":    result.UserID,
		"session_id": result.SessionID,
		"token_use":  result.TokenUse,
		"reason":     result.Reason,
		"expires_at": result.ExpiresAt,
		"revoked_at": result.RevokedAt,
	})

	c.JSON(http.StatusOK, gin.H{
		"message":    "Token revoked successfully",
		"jti":        result.JTI,
		"user_id":    result.UserID,
		"session_id": result.SessionID,
		"token_use":  result.TokenUse,
		"expires_at": result.ExpiresAt,
		"revoked_at": result.RevokedAt,
	})
}

// ListRevokedTokens 查询 revoke list
// @Summary 查询 revoke list
// @Description 返回显式加入 denylist 的 JWT 记录，支持按 jti/user/session/token_use 过滤
// @Tags 安全管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Router /api/security/tokens/revoked [get]
func (h *UserSecurityHandler) ListRevokedTokens(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "User security service unavailable",
			Message: "user security service not configured",
		})
		return
	}

	var userID *uint
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "Invalid user ID",
				Message: "user_id must be a valid number",
			})
			return
		}
		value := uint(parsed)
		userID = &value
	}
	page, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page", "1")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page_size", "20")))
	activeOnly := strings.EqualFold(strings.TrimSpace(c.DefaultQuery("active_only", "false")), "true")

	items, total, err := h.service.ListRevokedTokens(c.Request.Context(), usersecurity.RevokedTokenListQuery{
		JTI:        strings.TrimSpace(c.Query("jti")),
		UserID:     userID,
		SessionID:  strings.TrimSpace(c.Query("session_id")),
		TokenUse:   strings.TrimSpace(c.Query("token_use")),
		ActiveOnly: activeOnly,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to list revoked tokens",
			Message: err.Error(),
		})
		return
	}

	resp := make([]revokedTokenListItem, 0, len(items))
	for _, item := range items {
		resp = append(resp, revokedTokenListItem{
			JTI:       item.JTI,
			UserID:    item.UserID,
			SessionID: item.SessionID,
			TokenUse:  item.TokenUse,
			Reason:    item.Reason,
			ExpiresAt: item.ExpiresAt,
			RevokedAt: item.RevokedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"count": total,
		"items": resp,
	})
}

func RegisterUserSecurityRoutes(r *gin.RouterGroup, handler *UserSecurityHandler) {
	if handler == nil {
		return
	}
	security := r.Group("/security")
	{
		security.GET("/users/:id", handler.GetUserSecurity)
		security.GET("/users/:id/sessions", handler.ListUserSessions)
		security.POST("/users/:id/sessions/revoke-all", handler.RevokeAllSessions)
		security.GET("/tokens/revoked", handler.ListRevokedTokens)
		security.POST("/users/:id/sessions/revoke", handler.RevokeSession)
		security.POST("/tokens/revoke", handler.RevokeToken)
		security.POST("/users/query", handler.QueryUsersSecurity)
		security.POST("/users/revoke-tokens", handler.BatchRevokeTokens)
		security.POST("/users/:id/revoke-tokens", handler.RevokeTokens)
	}
}
