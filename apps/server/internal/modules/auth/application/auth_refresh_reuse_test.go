package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// newRefreshReuseService：建库 + seed 用户 + 按档位注入 refresh reuse 政策。
func newRefreshReuseService(t *testing.T, policy string) *AuthService {
	t.Helper()
	db := newAuthServiceTestDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &models.User{
		ID:       95,
		Username: "reuse-user",
		Email:    "reuse-user@example.com",
		Password: string(hash),
		Status:   "active",
		Role:     "customer",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc := NewService(db, testAuthConfig())
	svc.WithRefreshReusePolicy(policy)
	return svc
}

func refreshReuseLogin(t *testing.T, svc *AuthService) (firstRefresh, rotatedRefresh string, sessionID string) {
	t.Helper()
	outcome, err := svc.Login(context.Background(), LoginInput{Username: "reuse-user", Password: "password123"}, AuthSessionMetadata{
		DeviceFingerprint: "fp-reuse",
		UserAgent:         "reuse-test/1.0",
		ClientIP:          "198.51.100.20",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if outcome.TwoFactorRequired || outcome.Result == nil {
		t.Fatalf("expected direct login, got %+v", outcome)
	}
	first, rotated := outcome.Result.RefreshToken, ""
	// 第一次刷新完成轮换：first 变成"已轮换旧 token"，rotated 是家族最新。
	refreshed, err := svc.RefreshToken(context.Background(), first, AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("first RefreshToken: %v", err)
	}
	rotated = refreshed.RefreshToken
	return first, rotated, outcome.Result.SessionID
}

func loadReuseSession(t *testing.T, svc *AuthService, sessionID string) models.UserAuthSession {
	t.Helper()
	var session models.UserAuthSession
	if err := svc.db.First(&session, "id = ?", sessionID).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	return session
}

// TestWithRefreshReusePolicyNormalization：档位归一化——仅 revoke_family
// 开启，其余取值（含空串/未知值）一律 off；nil receiver 原样返回。
func TestWithRefreshReusePolicyNormalization(t *testing.T) {
	for raw, want := range map[string]bool{
		"revoke_family":   true,
		" REVOKE_FAMILY ": true,
		"":                false,
		"off":             false,
		"block":           false,
		"bogus":           false,
	} {
		svc := NewService(newAuthServiceTestDB(t), testAuthConfig())
		svc.WithRefreshReusePolicy(raw)
		if svc.refreshReuseRevocation != want {
			t.Fatalf("policy %q revocation = %v, want %v", raw, svc.refreshReuseRevocation, want)
		}
	}

	var nilSvc *AuthService
	if got := nilSvc.WithRefreshReusePolicy("revoke_family"); got != nil {
		t.Fatal("nil receiver must return nil")
	}
}

// TestRefreshReuseOffKeepsSessionActive：off 档位下旧 token 重放被拒绝，
// 但会话保持 active——既有行为不变，合法客户端的最新 token 可继续刷新。
func TestRefreshReuseOffKeepsSessionActive(t *testing.T) {
	svc := newRefreshReuseService(t, "off")
	first, rotated, sessionID := refreshReuseLogin(t, svc)

	_, err := svc.RefreshToken(context.Background(), first, AuthSessionMetadata{})
	require.ErrorIs(t, err, ErrAuthInvalidRefreshToken)

	session := loadReuseSession(t, svc, sessionID)
	if session.Status != "active" || session.RevokedAt != nil {
		t.Fatalf("session must stay active under off, got %+v", session)
	}

	if _, err := svc.RefreshToken(context.Background(), rotated, AuthSessionMetadata{}); err != nil {
		t.Fatalf("latest token must keep refreshing under off, got %v", err)
	}
}

// TestRefreshReuseRevocationRevokesFamily：revoke_family 档位下旧 token
// 重放被拒绝且整个会话（家族）被吊销。
func TestRefreshReuseRevocationRevokesFamily(t *testing.T) {
	svc := newRefreshReuseService(t, "revoke_family")
	first, _, sessionID := refreshReuseLogin(t, svc)

	_, err := svc.RefreshToken(context.Background(), first, AuthSessionMetadata{})
	require.ErrorIs(t, err, ErrAuthInvalidRefreshToken)

	session := loadReuseSession(t, svc, sessionID)
	if session.Status != "revoked" || session.RevokedAt == nil {
		t.Fatalf("family must be revoked on reuse, got %+v", session)
	}
}

// TestRefreshReuseRevocationKillsLatestToken：家族吊销后，连合法用户手里
// 的最新 refresh token 也被拒（status=revoked），迫使重新登录。
// refreshReuseLogin 返回时家族已轮换到 v1：first=v0（已轮换）、rotated=v1（最新）。
func TestRefreshReuseRevocationKillsLatestToken(t *testing.T) {
	svc := newRefreshReuseService(t, "revoke_family")
	first, rotated, _ := refreshReuseLogin(t, svc)

	// 重放已轮换的旧 token → 触发家族吊销。
	if _, err := svc.RefreshToken(context.Background(), first, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalidRefreshToken) {
		t.Fatalf("replay must be rejected, got %v", err)
	}
	// 家族最新的 token 一并失效。
	if _, err := svc.RefreshToken(context.Background(), rotated, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalidRefreshToken) {
		t.Fatalf("latest token must be dead after family revocation, got %v", err)
	}
}

// TestRefreshReuseNotTriggeredByFutureVersion：token 声明的版本大于库内
// 版本（异常/伪造 token）只被拒绝，不触发家族吊销。
func TestRefreshReuseNotTriggeredByFutureVersion(t *testing.T) {
	svc := newRefreshReuseService(t, "revoke_family")
	_, _, sessionID := refreshReuseLogin(t, svc)

	cfg := testAuthConfig()
	forged, err := hookCreateHS256JWT(map[string]interface{}{
		"iat":                   time.Now().Unix(),
		"sub":                   95,
		"jti":                   "forged-jti",
		"user_id":               95,
		"roles":                 []string{"customer"},
		"exp":                   time.Now().Add(time.Hour).Unix(),
		"token_use":             "refresh",
		"token_version":         0,
		"session_id":            sessionID,
		"session_token_version": 99,
	}, cfg.JWT.Secret)
	if err != nil {
		t.Fatalf("forge token: %v", err)
	}

	if _, err := svc.RefreshToken(context.Background(), forged, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalidRefreshToken) {
		t.Fatalf("future-version token must be rejected, got %v", err)
	}
	session := loadReuseSession(t, svc, sessionID)
	if session.Status != "active" || session.RevokedAt != nil {
		t.Fatalf("future-version mismatch must not revoke, got %+v", session)
	}
}

// TestRefreshReuseRevocationDBError：家族吊销的 UPDATE 失败时 fail-closed
// ——重放仍被拒绝（错误在 RefreshToken 边界统一折叠为 invalid refresh
// token，不向调用方泄漏内部失败），绝不静默放过。经 sqlite 触发器构造
// "读成功、写失败"。
func TestRefreshReuseRevocationDBError(t *testing.T) {
	svc := newRefreshReuseService(t, "revoke_family")
	first, _, _ := refreshReuseLogin(t, svc)

	if err := svc.db.Exec(`CREATE TRIGGER block_revoke BEFORE UPDATE ON user_auth_sessions
WHEN NEW.status = 'revoked' BEGIN SELECT RAISE(ABORT, 'no-revoke'); END;`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := svc.RefreshToken(context.Background(), first, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalidRefreshToken) {
		t.Fatalf("replay must stay rejected when revocation fails, got %v", err)
	}
}
