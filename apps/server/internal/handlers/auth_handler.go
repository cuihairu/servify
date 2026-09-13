package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	service  authService
	policy   sessionRiskPolicy
	resolver *configscope.Resolver
	ipIntel  sessionIPIntelligence
}

type authService interface {
	Register(ctx context.Context, req services.RegisterInput, meta services.AuthSessionMetadata) (*services.AuthResult, error)
	Login(ctx context.Context, req services.LoginInput, meta services.AuthSessionMetadata) (*services.LoginOutcome, error)
	GetCurrentUser(ctx context.Context, userID uint) (*models.User, error)
	ListAuthSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error)
	RevokeCurrentSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error)
	RevokeOtherSessions(ctx context.Context, userID uint, currentSessionID string) (int, error)
	RefreshToken(ctx context.Context, refreshToken string, meta services.AuthSessionMetadata) (*services.AuthResult, error)
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(service authService) *AuthHandler {
	return &AuthHandler{service: service, policy: defaultSessionRiskPolicy(), ipIntel: heuristicSessionIPIntelligence{}}
}

func (h *AuthHandler) WithSessionRiskPolicyConfig(cfg config.SessionRiskPolicyConfig) *AuthHandler {
	if h != nil {
		h.policy = sessionRiskPolicyFromConfig(cfg)
	}
	return h
}

func (h *AuthHandler) WithSessionRiskResolver(resolver *configscope.Resolver) *AuthHandler {
	if h != nil {
		h.resolver = resolver
	}
	return h
}

func (h *AuthHandler) WithSessionIPIntelligence(provider sessionIPIntelligence) *AuthHandler {
	if h != nil && provider != nil {
		h.ipIntel = provider
	}
	return h
}

// Register godoc
// @Summary Register a new user
// @Tags auth
// @Accept json
// @Produce json
// @Param body body registerRequest true "Registration data"
// @Success 201 {object} tokenResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效", "message": err.Error()})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" || req.Email == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名、邮箱和密码不能为空"})
		return
	}

	result, err := h.service.Register(c.Request.Context(), services.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
		Phone:    req.Phone,
		Role:     req.Role,
	}, authSessionMetadataFromRequest(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidAuthInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": "用户名、邮箱和密码不能为空"})
		case errors.Is(err, services.ErrAuthUserAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": "用户名或邮箱已存在"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败"})
		}
		return
	}

	c.JSON(http.StatusCreated, tokenResponse{
		Token:            result.Token,
		ExpiresIn:        result.ExpiresIn,
		RefreshToken:     result.RefreshToken,
		RefreshExpiresIn: result.RefreshExpiresIn,
		User:             mapUserResponse(result.User),
	})
}

// Login godoc
// @Summary Login
// @Tags auth
// @Accept json
// @Produce json
// @Param body body loginRequest true "Login credentials"
// @Success 200 {object} tokenResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}

	outcome, err := h.service.Login(c.Request.Context(), services.LoginInput{
		Username: req.Username,
		Password: req.Password,
	}, authSessionMetadataFromRequest(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAuthInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		case errors.Is(err, services.ErrAuthUserDisabled):
			c.JSON(http.StatusForbidden, gin.H{"error": "账号已被禁用"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "登录失败"})
		}
		return
	}
	if outcome.TwoFactorRequired {
		// 两步验证挑战：200 但不带 token 字段，客户端须持挑战 token 调
		// /auth/2fa/verify 换取正式会话（未启用用户响应形状不变）。
		c.JSON(http.StatusOK, gin.H{
			"two_factor_required": true,
			"challenge_token":     outcome.ChallengeToken,
			"expires_in":          outcome.ExpiresIn,
		})
		return
	}
	result := outcome.Result

	c.JSON(http.StatusOK, tokenResponse{
		Token:            result.Token,
		ExpiresIn:        result.ExpiresIn,
		RefreshToken:     result.RefreshToken,
		RefreshExpiresIn: result.RefreshExpiresIn,
		User:             mapUserResponse(result.User),
	})
}

// GetCurrentUser godoc
// @Summary Get current user info
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} userResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/me [get]
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}

	user, err := h.service.GetCurrentUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": mapUserResponse(user),
	})
}

// RefreshToken godoc
// @Summary Refresh JWT token
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} tokenResponse
// @Router /api/v1/auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	refreshToken := extractRefreshToken(c)
	result, err := h.service.RefreshToken(c.Request.Context(), refreshToken, authSessionMetadataFromRequest(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAuthInvalidRefreshToken):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 refresh token"})
		case errors.Is(err, services.ErrAuthUserDisabled):
			c.JSON(http.StatusForbidden, gin.H{"error": "账号已被禁用"})
		default:
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
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

// ListSessions godoc
// @Summary List current user auth sessions
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/sessions [get]
func (h *AuthHandler) ListSessions(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}

	sessions, err := h.service.ListAuthSessions(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
		return
	}
	currentSessionID := authSessionID(c)
	policy := h.sessionRiskPolicy(c.Request.Context())
	riskContext := buildSessionRiskContext(sessions, policy, h.ipIntel)
	items := make([]gin.H, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, mapSessionResponse(session, currentSessionID != "" && session.ID == currentSessionID, riskContext, policy, h.ipIntel))
	}

	c.JSON(http.StatusOK, gin.H{
		"count": len(items),
		"items": items,
	})
}

func (h *AuthHandler) sessionRiskPolicy(ctx context.Context) sessionRiskPolicy {
	if h != nil && h.resolver != nil {
		return sessionRiskPolicyFromConfig(h.resolver.ResolveSessionRisk(ctx, nil))
	}
	if h == nil {
		return defaultSessionRiskPolicy()
	}
	return h.policy
}

// LogoutCurrentSession godoc
// @Summary Logout current auth session
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/sessions/logout-current [post]
func (h *AuthHandler) LogoutCurrentSession(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	sessionID := authSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前 token 不包含 session"})
		return
	}

	session, err := h.service.RevokeCurrentSession(c.Request.Context(), userID, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注销当前会话失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Current session logged out successfully",
		"session_id":    session.ID,
		"status":        session.Status,
		"token_version": session.TokenVersion,
	})
}

// LogoutOtherSessions godoc
// @Summary Logout other auth sessions
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/sessions/logout-others [post]
func (h *AuthHandler) LogoutOtherSessions(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		return
	}
	sessionID := authSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前 token 不包含 session"})
		return
	}

	count, err := h.service.RevokeOtherSessions(c.Request.Context(), userID, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注销其它会话失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":            "Other sessions logged out successfully",
		"current_session_id": sessionID,
		"count":              count,
	})
}

func authUserID(c *gin.Context) (uint, bool) {
	userIDRaw, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}

	switch v := userIDRaw.(type) {
	case float64:
		return uint(v), true
	case uint:
		return v, true
	case int:
		return uint(v), true
	default:
		return 0, false
	}
}

func authSessionID(c *gin.Context) string {
	if raw, ok := c.Get("session_id"); ok {
		if sessionID, ok := raw.(string); ok {
			return strings.TrimSpace(sessionID)
		}
	}
	return ""
}

func extractRefreshToken(c *gin.Context) string {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&req); err == nil && strings.TrimSpace(req.RefreshToken) != "" {
		return strings.TrimSpace(req.RefreshToken)
	}

	ah := c.GetHeader("Authorization")
	if strings.HasPrefix(strings.ToLower(ah), "bearer ") {
		return strings.TrimSpace(ah[len("Bearer "):])
	}
	return ""
}

func authSessionMetadataFromRequest(c *gin.Context) services.AuthSessionMetadata {
	if c == nil {
		return services.AuthSessionMetadata{}
	}
	return services.AuthSessionMetadata{
		DeviceFingerprint: authDeviceFingerprint(c),
		UserAgent:         strings.TrimSpace(c.GetHeader("User-Agent")),
		ClientIP:          strings.TrimSpace(c.ClientIP()),
	}
}

func authDeviceFingerprint(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if explicit := strings.TrimSpace(c.GetHeader("X-Device-ID")); explicit != "" {
		if len(explicit) > 128 {
			return explicit[:128]
		}
		return explicit
	}

	userAgent := strings.TrimSpace(c.GetHeader("User-Agent"))
	clientIP := strings.TrimSpace(c.ClientIP())
	if userAgent == "" && clientIP == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(userAgent + "\n" + clientIP))
	return hex.EncodeToString(sum[:16])
}

type sessionRiskContext struct {
	ActiveSessionCount int
	PublicIPCount      int
	DeviceCount        int
	HotRefreshCount    int
	LatestActive       *models.UserAuthSession
}

type sessionRiskPolicy struct {
	HotRefreshWindow          time.Duration
	RecentRefreshWindow       time.Duration
	TodayRefreshWindow        time.Duration
	RapidChangeWindow         time.Duration
	StaleActivityWindow       time.Duration
	MultiPublicIPThreshold    int
	ManySessionsThreshold     int
	HotRefreshFamilyThreshold int
	MediumRiskScore           int
	HighRiskScore             int
}

func defaultSessionRiskPolicy() sessionRiskPolicy {
	return sessionRiskPolicy{
		HotRefreshWindow:          15 * time.Minute,
		RecentRefreshWindow:       time.Hour,
		TodayRefreshWindow:        24 * time.Hour,
		RapidChangeWindow:         24 * time.Hour,
		StaleActivityWindow:       30 * 24 * time.Hour,
		MultiPublicIPThreshold:    2,
		ManySessionsThreshold:     3,
		HotRefreshFamilyThreshold: 2,
		MediumRiskScore:           2,
		HighRiskScore:             4,
	}
}

func sessionRiskPolicyFromConfig(cfg config.SessionRiskPolicyConfig) sessionRiskPolicy {
	policy := defaultSessionRiskPolicy()
	if cfg.HotRefreshWindowMinutes > 0 {
		policy.HotRefreshWindow = time.Duration(cfg.HotRefreshWindowMinutes) * time.Minute
	}
	if cfg.RecentRefreshWindowMinutes > 0 {
		policy.RecentRefreshWindow = time.Duration(cfg.RecentRefreshWindowMinutes) * time.Minute
	}
	if cfg.TodayRefreshWindowHours > 0 {
		policy.TodayRefreshWindow = time.Duration(cfg.TodayRefreshWindowHours) * time.Hour
	}
	if cfg.RapidChangeWindowHours > 0 {
		policy.RapidChangeWindow = time.Duration(cfg.RapidChangeWindowHours) * time.Hour
	}
	if cfg.StaleActivityWindowDays > 0 {
		policy.StaleActivityWindow = time.Duration(cfg.StaleActivityWindowDays) * 24 * time.Hour
	}
	if cfg.MultiPublicIPThreshold > 0 {
		policy.MultiPublicIPThreshold = cfg.MultiPublicIPThreshold
	}
	if cfg.ManySessionsThreshold > 0 {
		policy.ManySessionsThreshold = cfg.ManySessionsThreshold
	}
	if cfg.HotRefreshFamilyThreshold > 0 {
		policy.HotRefreshFamilyThreshold = cfg.HotRefreshFamilyThreshold
	}
	if cfg.MediumRiskScore > 0 {
		policy.MediumRiskScore = cfg.MediumRiskScore
	}
	if cfg.HighRiskScore > 0 {
		policy.HighRiskScore = cfg.HighRiskScore
	}
	return policy
}

func buildSessionRiskContext(sessions []models.UserAuthSession, policy sessionRiskPolicy, provider sessionIPIntelligence) sessionRiskContext {
	publicIPs := make(map[string]struct{})
	devices := make(map[string]struct{})
	activeCount := 0
	hotRefreshCount := 0
	var latestActive *models.UserAuthSession

	for _, session := range sessions {
		if session.RevokedAt != nil || strings.EqualFold(session.Status, "revoked") {
			continue
		}
		activeCount++
		if isSessionMoreRecent(session, latestActive) {
			copy := session
			latestActive = &copy
		}

		if strings.EqualFold(describeSessionIP(provider, session.ClientIP).NetworkLabel, "public") {
			if ip := strings.TrimSpace(session.ClientIP); ip != "" {
				publicIPs[ip] = struct{}{}
			}
		}
		if device := strings.TrimSpace(session.DeviceFingerprint); device != "" {
			devices[device] = struct{}{}
		}
		if session.LastRefreshedAt != nil && !session.LastRefreshedAt.IsZero() && time.Since(session.LastRefreshedAt.UTC()) <= policy.HotRefreshWindow {
			hotRefreshCount++
		}
	}

	return sessionRiskContext{
		ActiveSessionCount: activeCount,
		PublicIPCount:      len(publicIPs),
		DeviceCount:        len(devices),
		HotRefreshCount:    hotRefreshCount,
		LatestActive:       latestActive,
	}
}

func mapSessionResponse(session models.UserAuthSession, isCurrent bool, riskContext sessionRiskContext, policy sessionRiskPolicy, provider sessionIPIntelligence) gin.H {
	riskScore, riskLevel, riskReasons, networkLabel, locationLabel, drift := describeSessionRisk(session, isCurrent, riskContext, policy, provider)
	refreshRecency, rapidRefresh := classifyRefreshActivity(session, riskContext, policy)
	return gin.H{
		"session_id":               session.ID,
		"status":                   session.Status,
		"token_version":            session.TokenVersion,
		"device_fingerprint":       session.DeviceFingerprint,
		"network_label":            networkLabel,
		"location_label":           locationLabel,
		"risk_score":               riskScore,
		"risk_level":               riskLevel,
		"risk_reasons":             riskReasons,
		"family_public_ip_count":   riskContext.PublicIPCount,
		"family_device_count":      riskContext.DeviceCount,
		"active_session_count":     riskContext.ActiveSessionCount,
		"family_hot_refresh_count": riskContext.HotRefreshCount,
		"reference_session_id":     drift.ReferenceSessionID,
		"ip_drift":                 drift.IPDrift,
		"device_drift":             drift.DeviceDrift,
		"rapid_ip_change":          drift.RapidIPChange,
		"rapid_device_change":      drift.RapidDeviceChange,
		"refresh_recency":          refreshRecency,
		"rapid_refresh_activity":   rapidRefresh,
		"user_agent":               session.UserAgent,
		"client_ip":                session.ClientIP,
		"last_seen_at":             session.LastSeenAt,
		"last_refreshed_at":        session.LastRefreshedAt,
		"revoked_at":               session.RevokedAt,
		"created_at":               session.CreatedAt,
		"updated_at":               session.UpdatedAt,
		"is_current":               isCurrent,
	}
}

type sessionDriftSignals struct {
	ReferenceSessionID string
	IPDrift            bool
	DeviceDrift        bool
	RapidIPChange      bool
	RapidDeviceChange  bool
}

func describeSessionRisk(session models.UserAuthSession, isCurrent bool, riskContext sessionRiskContext, policy sessionRiskPolicy, provider sessionIPIntelligence) (int, string, []string, string, string, sessionDriftSignals) {
	reasons := make([]string, 0, 8)
	ipDesc := describeSessionIP(provider, session.ClientIP)
	networkLabel := ipDesc.NetworkLabel
	locationLabel := ipDesc.LocationLabel
	drift := detectSessionDrift(session, riskContext, policy)
	score := 0

	if session.RevokedAt != nil || strings.EqualFold(session.Status, "revoked") {
		score += 3
		reasons = append(reasons, "revoked_session")
	}
	if !isCurrent {
		score++
		reasons = append(reasons, "not_current_session")
	}
	if strings.EqualFold(networkLabel, "public") {
		score++
		reasons = append(reasons, "public_network")
	}
	if strings.EqualFold(locationLabel, "documentation") || strings.EqualFold(locationLabel, "shared_address_space") {
		score++
		reasons = append(reasons, "non_geolocatable_network")
	}
	if strings.EqualFold(networkLabel, "public") && riskContext.PublicIPCount >= policy.MultiPublicIPThreshold {
		score++
		reasons = append(reasons, "multi_public_ip_family")
	}
	if riskContext.ActiveSessionCount >= policy.ManySessionsThreshold {
		score++
		reasons = append(reasons, "many_active_sessions")
	}
	if drift.IPDrift {
		score++
		reasons = append(reasons, "ip_drift")
	}
	if drift.DeviceDrift {
		score++
		reasons = append(reasons, "device_drift")
	}
	if drift.RapidIPChange {
		score++
		reasons = append(reasons, "rapid_ip_change")
	}
	if drift.RapidDeviceChange {
		score++
		reasons = append(reasons, "rapid_device_change")
	}
	if _, rapidRefresh := classifyRefreshActivity(session, riskContext, policy); rapidRefresh {
		score++
		reasons = append(reasons, "rapid_refresh_activity")
	}
	if session.LastSeenAt == nil || time.Since(session.LastSeenAt.UTC()) > policy.StaleActivityWindow {
		score++
		reasons = append(reasons, "stale_activity")
	}
	if strings.TrimSpace(session.DeviceFingerprint) == "" {
		score++
		reasons = append(reasons, "missing_device_fingerprint")
	}

	switch {
	case score >= policy.HighRiskScore:
		return score, "high", reasons, networkLabel, locationLabel, drift
	case score >= policy.MediumRiskScore:
		return score, "medium", reasons, networkLabel, locationLabel, drift
	default:
		return score, "low", reasons, networkLabel, locationLabel, drift
	}
}

func classifyRefreshActivity(session models.UserAuthSession, riskContext sessionRiskContext, policy sessionRiskPolicy) (string, bool) {
	if session.LastRefreshedAt == nil || session.LastRefreshedAt.IsZero() {
		return "unknown", false
	}
	age := time.Since(session.LastRefreshedAt.UTC())
	switch {
	case age <= policy.HotRefreshWindow:
		return "hot", riskContext.HotRefreshCount >= policy.HotRefreshFamilyThreshold
	case age <= policy.RecentRefreshWindow:
		return "recent", false
	case age <= policy.TodayRefreshWindow:
		return "today", false
	default:
		return "stale", false
	}
}

func detectSessionDrift(session models.UserAuthSession, riskContext sessionRiskContext, policy sessionRiskPolicy) sessionDriftSignals {
	if riskContext.LatestActive == nil || riskContext.LatestActive.ID == "" || riskContext.LatestActive.ID == session.ID {
		return sessionDriftSignals{}
	}
	ref := riskContext.LatestActive
	candidateTime := sessionRecencyTime(session)
	referenceTime := sessionRecencyTime(*ref)
	recentWindow := referenceTime.Sub(candidateTime)
	if recentWindow < 0 {
		recentWindow = -recentWindow
	}
	ipDrift := strings.TrimSpace(session.ClientIP) != "" && strings.TrimSpace(ref.ClientIP) != "" && strings.TrimSpace(session.ClientIP) != strings.TrimSpace(ref.ClientIP)
	deviceDrift := strings.TrimSpace(session.DeviceFingerprint) != "" && strings.TrimSpace(ref.DeviceFingerprint) != "" && strings.TrimSpace(session.DeviceFingerprint) != strings.TrimSpace(ref.DeviceFingerprint)
	return sessionDriftSignals{
		ReferenceSessionID: ref.ID,
		IPDrift:            ipDrift,
		DeviceDrift:        deviceDrift,
		RapidIPChange:      ipDrift && recentWindow <= policy.RapidChangeWindow,
		RapidDeviceChange:  deviceDrift && recentWindow <= policy.RapidChangeWindow,
	}
}

func isSessionMoreRecent(candidate models.UserAuthSession, current *models.UserAuthSession) bool {
	if current == nil {
		return true
	}
	return sessionRecencyTime(candidate).After(sessionRecencyTime(*current))
}

func sessionRecencyTime(session models.UserAuthSession) time.Time {
	switch {
	case session.LastSeenAt != nil && !session.LastSeenAt.IsZero():
		return session.LastSeenAt.UTC()
	case session.LastRefreshedAt != nil && !session.LastRefreshedAt.IsZero():
		return session.LastRefreshedAt.UTC()
	case !session.UpdatedAt.IsZero():
		return session.UpdatedAt.UTC()
	default:
		return session.CreatedAt.UTC()
	}
}

func classifyNetworkLabel(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "unknown"
	}
	if parsed.IsLoopback() {
		return "loopback"
	}
	if isSharedAddressSpaceIP(parsed) {
		return "private"
	}
	if isPrivateIP(parsed) {
		return "private"
	}
	return "public"
}

func classifyLocationLabel(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "unknown"
	}
	switch {
	case parsed.IsLoopback():
		return "loopback"
	case isDocumentationIP(parsed):
		return "documentation"
	case isSharedAddressSpaceIP(parsed):
		return "shared_address_space"
	case isPrivateIP(parsed):
		return "private"
	default:
		return "public_unknown"
	}
}

func isPrivateIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10:
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true
		case v4[0] == 192 && v4[1] == 168:
			return true
		}
		return false
	}
	return ip.IsPrivate()
}

func isDocumentationIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 2:
			return true
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100:
			return true
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113:
			return true
		}
	}
	return false
}

func isSharedAddressSpaceIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	return false
}

// Request/Response types
type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token            string       `json:"token"`
	ExpiresIn        int          `json:"expires_in"`
	RefreshToken     string       `json:"refresh_token,omitempty"`
	RefreshExpiresIn int          `json:"refresh_expires_in,omitempty"`
	User             userResponse `json:"user"`
}

type userResponse struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Avatar      string `json:"avatar"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	TotpEnabled bool   `json:"totp_enabled"`
}

func mapUserResponse(user *models.User) userResponse {
	if user == nil {
		return userResponse{}
	}
	return userResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Name:        user.Name,
		Phone:       user.Phone,
		Avatar:      user.Avatar,
		Role:        user.Role,
		Status:      user.Status,
		TotpEnabled: user.TotpEnabled,
	}
}
