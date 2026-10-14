package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	repo   Repository
	config *config.Config
	// 登录风险执行（P2-5 第二刀）：riskIntel 提供登录来源 IP 的网络标签，
	// loginEnforcement 取 ""/"off"/"step_up"/"block"。两者均未注入时登录
	// 流程与既有行为完全一致。
	riskIntel        LoginRiskIntel
	loginEnforcement string
	// refresh token 重放处置档位（P2-5 第三刀）：true 时检测到已轮换旧
	// token 重放即吊销整个会话家族；false 保持"拒绝但不吊销"的既有行为。
	refreshReuseRevocation bool
}

// LoginRiskIntel 抽象登录来源 IP 情报：返回网络标签。handlers 包的
// HTTPSessionIPIntelligence 经适配器注入；nil 表示未配置情报源。
type LoginRiskIntel interface {
	LoginNetworkLabel(ctx context.Context, ip string) string
}

// LoginRiskHighRiskNetworkLabels：情报标签不在该集合内即视为高风险来源。
// 内建启发式分类器只产出这四个标签（public/private/loopback/unknown），
// 接入真实情报源后出现的任何富标签（hosting/proxy/tor/…）都构成风险信号。
var LoginRiskHighRiskNetworkLabels = []string{"public", "private", "loopback", "unknown", ""}

type RegisterInput struct {
	Username string
	Email    string
	Password string
	Name     string
	Phone    string
	Role     string
}

type LoginInput struct {
	Username string
	Password string
}

type AuthSessionMetadata struct {
	DeviceFingerprint string
	UserAgent         string
	ClientIP          string
}

type AuthResult struct {
	Token            string
	ExpiresIn        int
	RefreshToken     string
	RefreshExpiresIn int
	User             *models.User
	SessionID        string
}

func NewService(repo Repository, cfg *config.Config) *AuthService {
	return &AuthService{repo: repo, config: cfg}
}

// WithLoginRiskEnforcement 注入登录风险情报源与执行档位（P2-5 第二刀）。
// enforcement 归一化为小写；非 step_up/block 的取值（含空串）一律视为 off。
func (s *AuthService) WithLoginRiskEnforcement(intel LoginRiskIntel, enforcement string) *AuthService {
	if s == nil {
		return s
	}
	s.riskIntel = intel
	switch strings.ToLower(strings.TrimSpace(enforcement)) {
	case "step_up":
		s.loginEnforcement = "step_up"
	case "block":
		s.loginEnforcement = "block"
	default:
		s.loginEnforcement = ""
	}
	return s
}

// WithRefreshReusePolicy 注入 refresh token 重放处置档位（P2-5 第三刀）。
// 仅 "revoke_family" 开启家族吊销，其余取值（含空串）视为 off——拒绝但
// 不吊销的既有行为。
func (s *AuthService) WithRefreshReusePolicy(policy string) *AuthService {
	if s == nil {
		return s
	}
	s.refreshReuseRevocation = strings.ToLower(strings.TrimSpace(policy)) == "revoke_family"
	return s
}

// loginRiskHighRisk 判定登录来源是否高风险：未注入情报源、来源 IP 为空
// 或标签落在已知安全集合内均不算高风险——默认部署永不拦截。
func (s *AuthService) loginRiskHighRisk(ctx context.Context, ip string) bool {
	if s == nil || s.riskIntel == nil || strings.TrimSpace(ip) == "" {
		return false
	}
	label := strings.ToLower(strings.TrimSpace(s.riskIntel.LoginNetworkLabel(ctx, ip)))
	for _, safe := range LoginRiskHighRiskNetworkLabels {
		if label == safe {
			return false
		}
	}
	return true
}

func (s *AuthService) Register(ctx context.Context, req RegisterInput, meta AuthSessionMetadata) (*AuthResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return nil, ErrInvalidAuthInput
	}

	var count int64
	count = s.repo.CountUsersByUsernameOrEmail(ctx, req.Username, req.Email)
	if count > 0 {
		return nil, ErrAuthUserAlreadyExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	role := req.Role
	if role == "" {
		role = "customer"
	}
	if role == "admin" && s.repo.CountUsers(ctx) > 0 {
		role = "customer"
	}

	user := &models.User{
		Username: req.Username,
		Email:    req.Email,
		Password: string(hash),
		Name:     req.Name,
		Phone:    req.Phone,
		Role:     role,
		Status:   "active",
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return nil, ErrAuthUserAlreadyExists
		}
		return nil, err
	}

	session, err := s.createAuthSession(ctx, user.ID, meta)
	if err != nil {
		return nil, err
	}
	return s.buildAuthResult(ctx, user, session)
}

// Login 验证密码后分岔：未启用两步验证（或 kill-switch 关闭）的用户直接拿
// 到会话 token；已启用的用户只拿到短命挑战 token，须走 VerifyTwoFactorLogin
// 完成第二因子后才换取正式会话。
func (s *AuthService) Login(ctx context.Context, req LoginInput, meta AuthSessionMetadata) (*LoginOutcome, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	user, err := s.repo.GetUserByUsernameOrEmail(ctx, req.Username)
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return nil, ErrAuthInvalidCredentials
		}
		return nil, err
	}
	if user.Status != "active" {
		return nil, ErrAuthUserDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, ErrAuthInvalidCredentials
	}
	// 登录风险执行（P2-5 第二刀）：凭据验证通过后、发放会话前评估来源风险。
	//   block：高风险来源直接拒绝；
	//   step_up：高风险来源强制第二因子——已绑定 TOTP 的用户进入挑战步
	//   （即使 2FA kill-switch 关闭），未绑定的用户无第二因子可用，拒绝。
	if s.loginRiskHighRisk(ctx, meta.ClientIP) {
		switch s.loginEnforcement {
		case "block":
			return nil, ErrLoginBlockedByRisk
		case "step_up":
			if !user.TotpEnabled {
				return nil, ErrLoginBlockedByRisk
			}
			if !s.twoFactorChallengeEnabled(s.config, user) {
				token, expiresIn, err := s.createChallengeToken(user, meta)
				if err != nil {
					return nil, err
				}
				return &LoginOutcome{TwoFactorRequired: true, ChallengeToken: token, ExpiresIn: expiresIn}, nil
			}
		}
	}
	if s.twoFactorChallengeEnabled(s.config, user) {
		token, expiresIn, err := s.createChallengeToken(user, meta)
		if err != nil {
			return nil, err
		}
		return &LoginOutcome{TwoFactorRequired: true, ChallengeToken: token, ExpiresIn: expiresIn}, nil
	}
	session, err := s.createAuthSession(ctx, user.ID, meta)
	if err != nil {
		return nil, err
	}
	result, err := s.buildAuthResult(ctx, user, session)
	if err != nil {
		return nil, err
	}
	return &LoginOutcome{Result: result}, nil
}

func (s *AuthService) GetCurrentUser(ctx context.Context, userID uint) (*models.User, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) ListAuthSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	if userID == 0 {
		return nil, fmt.Errorf("user_id required")
	}

	return s.repo.ListAuthSessionsByUser(ctx, userID)
}

func (s *AuthService) RevokeCurrentSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	if userID == 0 {
		return nil, fmt.Errorf("user_id required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}

	rows, err := s.repo.RevokeAuthSession(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, fmt.Errorf("session not found")
	}

	session, err := s.repo.GetAuthSession(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AuthService) RevokeOtherSessions(ctx context.Context, userID uint, currentSessionID string) (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrAuthInvalidDB
	}
	if userID == 0 {
		return 0, fmt.Errorf("user_id required")
	}
	if strings.TrimSpace(currentSessionID) == "" {
		return 0, fmt.Errorf("current session_id required")
	}

	rows, err := s.repo.RevokeOtherAuthSessions(ctx, userID, currentSessionID)
	if err != nil {
		return 0, err
	}
	return int(rows), nil
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string, meta AuthSessionMetadata) (*AuthResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, ErrAuthInvalidRefreshToken
	}

	payload, err := (platformauth.Validator{Secret: s.config.JWT.Secret}).ValidateToken(refreshToken)
	if err != nil {
		return nil, ErrAuthInvalidRefreshToken
	}
	if tokenUse, _ := payload["token_use"].(string); strings.TrimSpace(tokenUse) != "refresh" {
		return nil, ErrAuthInvalidRefreshToken
	}

	userID, ok := authNumericClaim(payload, "user_id", "sub")
	if !ok || userID == 0 {
		return nil, ErrAuthInvalidRefreshToken
	}
	sessionID, _ := authStringClaim(payload, "session_id", "sid")
	if sessionID == "" {
		return nil, ErrAuthInvalidRefreshToken
	}
	sessionVersion, ok := authNumericClaim(payload, "session_token_version", "stv")
	if !ok {
		return nil, ErrAuthInvalidRefreshToken
	}
	tokenVersion, _ := authNumericClaim(payload, "token_version")
	issuedAt, _ := authNumericClaim(payload, "iat")

	user, err := s.GetCurrentUser(ctx, uint(userID))
	if err != nil {
		return nil, ErrAuthInvalidRefreshToken
	}
	if user.Status != "active" {
		return nil, ErrAuthUserDisabled
	}
	if user.TokenVersion > int(tokenVersion) {
		return nil, ErrAuthInvalidRefreshToken
	}
	if user.TokenValidAfter != nil && !user.TokenValidAfter.IsZero() && issuedAt < user.TokenValidAfter.Unix() {
		return nil, ErrAuthInvalidRefreshToken
	}

	session, err := s.rotateRefreshSession(ctx, uint(userID), sessionID, int(sessionVersion), meta)
	if err != nil {
		return nil, ErrAuthInvalidRefreshToken
	}
	return s.buildAuthResult(ctx, user, session)
}

func (s *AuthService) buildAuthResult(ctx context.Context, user *models.User, session *models.UserAuthSession) (*AuthResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	if session == nil {
		return nil, fmt.Errorf("auth session is required")
	}

	now := time.Now()
	token, err := HookCreateHS256JWT(map[string]interface{}{
		"iat":                   now.Unix(),
		"sub":                   user.ID,
		"jti":                   newAuthTokenID(),
		"user_id":               user.ID,
		"roles":                 []string{user.Role},
		"exp":                   now.Add(s.config.JWT.ExpiresIn).Unix(),
		"token_use":             "access",
		"token_version":         user.TokenVersion,
		"session_id":            session.ID,
		"session_token_version": session.TokenVersion,
	}, s.config.JWT.Secret)
	if err != nil {
		return nil, err
	}
	refreshExpiresIn := s.refreshExpiresIn()
	refreshToken, err := HookCreateHS256JWT(map[string]interface{}{
		"iat":                   now.Unix(),
		"sub":                   user.ID,
		"jti":                   newAuthTokenID(),
		"user_id":               user.ID,
		"roles":                 []string{user.Role},
		"exp":                   now.Add(refreshExpiresIn).Unix(),
		"token_use":             "refresh",
		"token_version":         user.TokenVersion,
		"session_id":            session.ID,
		"session_token_version": session.TokenVersion,
	}, s.config.JWT.Secret)
	if err != nil {
		return nil, err
	}
	_ = s.repo.UpdateUserLastLogin(ctx, user, now)
	return &AuthResult{
		Token:            token,
		ExpiresIn:        int(s.config.JWT.ExpiresIn.Seconds()),
		RefreshToken:     refreshToken,
		RefreshExpiresIn: int(refreshExpiresIn.Seconds()),
		User:             user,
		SessionID:        session.ID,
	}, nil
}

func (s *AuthService) createAuthSession(ctx context.Context, userID uint, meta AuthSessionMetadata) (*models.UserAuthSession, error) {
	if s.repo == nil {
		return nil, ErrAuthInvalidDB
	}
	now := time.Now().UTC()
	session := &models.UserAuthSession{
		ID:                newAuthSessionID(),
		UserID:            userID,
		Status:            "active",
		TokenVersion:      0,
		DeviceFingerprint: normalizeAuthSessionField(meta.DeviceFingerprint, 128),
		UserAgent:         normalizeAuthSessionField(meta.UserAgent, 512),
		ClientIP:          normalizeAuthSessionField(meta.ClientIP, 128),
		LastSeenAt:        &now,
	}
	if err := s.repo.CreateAuthSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AuthService) rotateRefreshSession(ctx context.Context, userID uint, sessionID string, expectedVersion int, meta AuthSessionMetadata) (*models.UserAuthSession, error) {
	session, err := s.repo.GetAuthSession(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(session.Status) != "active" || session.RevokedAt != nil {
		return nil, ErrAuthInvalidRefreshToken
	}
	if session.TokenVersion != expectedVersion {
		// refresh token 重放（reuse，P2-5 第三刀）：token 声明的版本小于库内
		// 当前版本，说明它已被轮换过——正常客户端不会持有，属会话泄漏信号。
		// revoke_family 档位下吊销整个家族（该 session 的所有轮换代——包括
		// 重放者与合法用户手里的"最新"token——一并失效，迫使重新登录）。
		// 对外统一 401，不回显是否吊销、不回显版本差异。
		if s.refreshReuseRevocation && expectedVersion < session.TokenVersion {
			if err := s.repo.RevokeActiveAuthSession(ctx, sessionID, userID); err != nil {
				return nil, err
			}
		}
		return nil, ErrAuthInvalidRefreshToken
	}

	if err := s.repo.RotateAuthSession(ctx, sessionID, userID, AuthSessionRotation{
		DeviceFingerprint: normalizeAuthSessionField(meta.DeviceFingerprint, 128),
		UserAgent:         normalizeAuthSessionField(meta.UserAgent, 512),
		ClientIP:          normalizeAuthSessionField(meta.ClientIP, 128),
		Now:               time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	return s.repo.GetAuthSession(ctx, sessionID, userID)
}

func (s *AuthService) refreshExpiresIn() time.Duration {
	if s != nil && s.config != nil && s.config.JWT.RefreshExpiresIn > 0 {
		return s.config.JWT.RefreshExpiresIn
	}
	return 7 * 24 * time.Hour
}

func authNumericClaim(payload map[string]interface{}, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int64(typed), true
		case json.Number:
			v, err := typed.Int64()
			if err == nil {
				return v, true
			}
		}
	}
	return 0, false
}

func authStringClaim(payload map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(string); ok {
			typed = strings.TrimSpace(typed)
			if typed != "" {
				return typed, true
			}
		}
	}
	return "", false
}

func normalizeAuthSessionField(value string, max int) string {
	value = strings.TrimSpace(value)
	if max > 0 && len(value) > max {
		return value[:max]
	}
	return value
}

func newAuthSessionID() string {
	var buf [16]byte
	if _, err := HookRandRead(buf[:]); err != nil {
		return fmt.Sprintf("auth_%d", time.Now().UnixNano())
	}
	return "auth_" + hex.EncodeToString(buf[:])
}

func newAuthTokenID() string {
	var buf [16]byte
	if _, err := HookRandRead(buf[:]); err != nil {
		return fmt.Sprintf("jti_%d", time.Now().UnixNano())
	}
	return "jti_" + hex.EncodeToString(buf[:])
}

func createHS256JWT(payload map[string]interface{}, secret string) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

	h := enc(headerJSON)
	p := enc(payloadJSON)
	signing := h + "." + p

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	sig := mac.Sum(nil)
	return signing + "." + enc(sig), nil
}
