package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func newAuthServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:auth_service")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserAuthSession{}, &models.UserRecoveryCode{}); err != nil {
		t.Fatalf("migrate models: %v", err)
	}
	return db
}

func testAuthConfig() *config.Config {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.JWT.ExpiresIn = time.Hour
	cfg.JWT.RefreshExpiresIn = 24 * time.Hour
	return cfg
}

func TestAuthServiceLoginAndRefreshRotateSession(t *testing.T) {
	db := newAuthServiceTestDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &models.User{
		ID:           77,
		Username:     "auth-user",
		Email:        "auth-user@example.com",
		Password:     string(hash),
		Status:       "active",
		Role:         "admin",
		TokenVersion: 2,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	svc := NewAuthService(db, testAuthConfig())

	loginResult, err := svc.Login(context.Background(), LoginInput{
		Username: "auth-user",
		Password: "password123",
	}, AuthSessionMetadata{
		DeviceFingerprint: "fp-login-1",
		UserAgent:         "servify-test/1.0",
		ClientIP:          "198.51.100.8",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loginResult.TwoFactorRequired || loginResult.Result == nil {
		t.Fatalf("expected direct login result, got challenge=%v", loginResult.TwoFactorRequired)
	}
	result := loginResult.Result
	if result.SessionID == "" {
		t.Fatalf("expected session id in login result")
	}

	payload1, err := (platformauth.Validator{Secret: testAuthConfig().JWT.Secret}).ValidateToken(result.Token)
	if err != nil {
		t.Fatalf("validate login token: %v", err)
	}
	if got := int(payload1["token_version"].(float64)); got != 2 {
		t.Fatalf("token_version = %d want 2", got)
	}
	if got := payload1["session_id"].(string); got != result.SessionID {
		t.Fatalf("session_id = %q want %q", got, result.SessionID)
	}
	if got := int(payload1["session_token_version"].(float64)); got != 0 {
		t.Fatalf("session_token_version = %d want 0", got)
	}
	if got := payload1["token_use"].(string); got != "access" {
		t.Fatalf("token_use = %q want access", got)
	}
	if got := payload1["jti"].(string); got == "" {
		t.Fatalf("expected access token jti")
	}
	refreshPayload1, err := (platformauth.Validator{Secret: testAuthConfig().JWT.Secret}).ValidateToken(result.RefreshToken)
	if err != nil {
		t.Fatalf("validate login refresh token: %v", err)
	}
	if got := refreshPayload1["token_use"].(string); got != "refresh" {
		t.Fatalf("refresh token_use = %q want refresh", got)
	}
	if got := refreshPayload1["jti"].(string); got == "" {
		t.Fatalf("expected refresh token jti")
	}
	if got := int(refreshPayload1["session_token_version"].(float64)); got != 0 {
		t.Fatalf("refresh session_token_version = %d want 0", got)
	}

	refreshResult, err := svc.RefreshToken(context.Background(), result.RefreshToken, AuthSessionMetadata{
		DeviceFingerprint: "fp-refresh-1",
		UserAgent:         "servify-test/2.0",
		ClientIP:          "198.51.100.9",
	})
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if refreshResult.SessionID != result.SessionID {
		t.Fatalf("refresh session id = %q want %q", refreshResult.SessionID, result.SessionID)
	}

	payload2, err := (platformauth.Validator{Secret: testAuthConfig().JWT.Secret}).ValidateToken(refreshResult.Token)
	if err != nil {
		t.Fatalf("validate refresh token: %v", err)
	}
	if got := int(payload2["session_token_version"].(float64)); got != 1 {
		t.Fatalf("session_token_version = %d want 1", got)
	}
	refreshPayload2, err := (platformauth.Validator{Secret: testAuthConfig().JWT.Secret}).ValidateToken(refreshResult.RefreshToken)
	if err != nil {
		t.Fatalf("validate rotated refresh token: %v", err)
	}
	if got := int(refreshPayload2["session_token_version"].(float64)); got != 1 {
		t.Fatalf("rotated refresh session_token_version = %d want 1", got)
	}

	var session models.UserAuthSession
	if err := db.First(&session, "id = ?", result.SessionID).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.TokenVersion != 1 {
		t.Fatalf("stored session token_version = %d want 1", session.TokenVersion)
	}
	if session.LastRefreshedAt == nil || session.LastRefreshedAt.IsZero() {
		t.Fatalf("expected last_refreshed_at to be set")
	}
	if session.DeviceFingerprint != "fp-refresh-1" {
		t.Fatalf("device_fingerprint = %q want fp-refresh-1", session.DeviceFingerprint)
	}
	if session.UserAgent != "servify-test/2.0" {
		t.Fatalf("user_agent = %q want servify-test/2.0", session.UserAgent)
	}
	if session.ClientIP != "198.51.100.9" {
		t.Fatalf("client_ip = %q want 198.51.100.9", session.ClientIP)
	}
	if session.LastSeenAt == nil || session.LastSeenAt.IsZero() {
		t.Fatalf("expected last_seen_at to be set")
	}

	if _, err := svc.RefreshToken(context.Background(), result.RefreshToken, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalidRefreshToken) {
		t.Fatalf("reusing old refresh token err = %v want %v", err, ErrAuthInvalidRefreshToken)
	}
}

func TestAuthServiceSelfManageSessions(t *testing.T) {
	db := newAuthServiceTestDB(t)
	user := &models.User{
		ID:       88,
		Username: "session-user",
		Email:    "session-user@example.com",
		Password: "ignored",
		Status:   "active",
		Role:     "admin",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Create([]models.UserAuthSession{
		{ID: "sess-current", UserID: 88, Status: "active", TokenVersion: 1, UserAgent: "ua1", ClientIP: "198.51.100.1", LastSeenAt: &now},
		{ID: "sess-other", UserID: 88, Status: "active", TokenVersion: 2, UserAgent: "ua2", ClientIP: "198.51.100.2", LastSeenAt: &now},
	}).Error; err != nil {
		t.Fatalf("seed sessions: %v", err)
	}

	svc := NewAuthService(db, testAuthConfig())

	sessions, err := svc.ListAuthSessions(context.Background(), 88)
	if err != nil {
		t.Fatalf("ListAuthSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d want 2", len(sessions))
	}

	count, err := svc.RevokeOtherSessions(context.Background(), 88, "sess-current")
	if err != nil {
		t.Fatalf("RevokeOtherSessions() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d want 1", count)
	}

	current, err := svc.RevokeCurrentSession(context.Background(), 88, "sess-current")
	if err != nil {
		t.Fatalf("RevokeCurrentSession() error = %v", err)
	}
	if current.Status != "revoked" {
		t.Fatalf("status = %q want revoked", current.Status)
	}

	var rows []models.UserAuthSession
	if err := db.Order("id asc").Find(&rows, "user_id = ?", 88).Error; err != nil {
		t.Fatalf("reload sessions: %v", err)
	}
	if rows[0].Status != "revoked" || rows[1].Status != "revoked" {
		t.Fatalf("expected all sessions revoked: %+v", rows)
	}
}
