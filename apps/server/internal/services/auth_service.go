package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
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
	"gorm.io/gorm"
)

type AuthService struct {
	db     *gorm.DB
	config *config.Config
}

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

func NewAuthService(db *gorm.DB, cfg *config.Config) *AuthService {
	return &AuthService{db: db, config: cfg}
}

func (s *AuthService) Register(ctx context.Context, req RegisterInput, meta AuthSessionMetadata) (*AuthResult, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return nil, ErrInvalidAuthInput
	}

	var count int64
	s.db.WithContext(ctx).Model(&models.User{}).Where("username = ? OR email = ?", req.Username, req.Email).Count(&count)
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
	if role == "admin" {
		var total int64
		s.db.WithContext(ctx).Model(&models.User{}).Count(&total)
		if total > 0 {
			role = "customer"
		}
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
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
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
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var user models.User
	if err := s.db.WithContext(ctx).Where("username = ? OR email = ?", req.Username, req.Username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
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
	if s.twoFactorChallengeEnabled(s.config, &user) {
		token, expiresIn, err := s.createChallengeToken(&user, meta)
		if err != nil {
			return nil, err
		}
		return &LoginOutcome{TwoFactorRequired: true, ChallengeToken: token, ExpiresIn: expiresIn}, nil
	}
	session, err := s.createAuthSession(ctx, user.ID, meta)
	if err != nil {
		return nil, err
	}
	result, err := s.buildAuthResult(ctx, &user, session)
	if err != nil {
		return nil, err
	}
	return &LoginOutcome{Result: result}, nil
}

func (s *AuthService) GetCurrentUser(ctx context.Context, userID uint) (*models.User, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *AuthService) ListAuthSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if userID == 0 {
		return nil, fmt.Errorf("user_id required")
	}

	var sessions []models.UserAuthSession
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at desc, created_at desc").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *AuthService) RevokeCurrentSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if userID == 0 {
		return nil, fmt.Errorf("user_id required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Updates(map[string]any{
			"status":        "revoked",
			"revoked_at":    now,
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("session not found")
	}

	var session models.UserAuthSession
	if err := s.db.WithContext(ctx).First(&session, "id = ? AND user_id = ?", sessionID, userID).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *AuthService) RevokeOtherSessions(ctx context.Context, userID uint, currentSessionID string) (int, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	if userID == 0 {
		return 0, fmt.Errorf("user_id required")
	}
	if strings.TrimSpace(currentSessionID) == "" {
		return 0, fmt.Errorf("current session_id required")
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("user_id = ? AND id <> ? AND status = ? AND revoked_at IS NULL", userID, currentSessionID, "active").
		Updates(map[string]any{
			"status":        "revoked",
			"revoked_at":    now,
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string, meta AuthSessionMetadata) (*AuthResult, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
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
	token, err := createHS256JWT(map[string]interface{}{
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
	refreshToken, err := createHS256JWT(map[string]interface{}{
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
	_ = s.db.WithContext(ctx).Model(user).Update("last_login", now).Error
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
	if s.db == nil {
		return nil, gorm.ErrInvalidDB
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
	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AuthService) rotateRefreshSession(ctx context.Context, userID uint, sessionID string, expectedVersion int, meta AuthSessionMetadata) (*models.UserAuthSession, error) {
	var session models.UserAuthSession
	if err := s.db.WithContext(ctx).First(&session, "id = ? AND user_id = ?", sessionID, userID).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(session.Status) != "active" || session.RevokedAt != nil {
		return nil, ErrAuthInvalidRefreshToken
	}
	if session.TokenVersion != expectedVersion {
		return nil, ErrAuthInvalidRefreshToken
	}

	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":             "active",
		"token_version":      gorm.Expr("COALESCE(token_version, 0) + 1"),
		"device_fingerprint": normalizeAuthSessionField(meta.DeviceFingerprint, 128),
		"user_agent":         normalizeAuthSessionField(meta.UserAgent, 512),
		"client_ip":          normalizeAuthSessionField(meta.ClientIP, 128),
		"last_seen_at":       now,
		"last_refreshed_at":  now,
		"revoked_at":         nil,
	}
	if err := s.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).First(&session, "id = ? AND user_id = ?", sessionID, userID).Error; err != nil {
		return nil, err
	}
	return &session, nil
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
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("auth_%d", time.Now().UnixNano())
	}
	return "auth_" + hex.EncodeToString(buf[:])
}

func newAuthTokenID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
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
