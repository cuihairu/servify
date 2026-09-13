package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"gorm.io/gorm"
)

// TestLoginWithOIDCLookupError 覆盖首查非 NotFound 错误的透传分支。
func TestLoginWithOIDCLookupError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, oidcTestConfig(nil))
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_oidc_lookup", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("boom oidc lookup"))
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	_, err := svc.LoginWithOIDC(context.Background(), OIDCIdentity{
		Subject: "sub-lookup", Email: "lookup@x.com", EmailVerified: true,
	}, AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "boom oidc lookup") {
		t.Fatalf("err = %v, want lookup failure", err)
	}
}

// TestLoginWithOIDCSessionCreateError 覆盖建号成功但会话创建失败。
func TestLoginWithOIDCSessionCreateError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))
	if err := db.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}

	_, err := svc.LoginWithOIDC(context.Background(), OIDCIdentity{
		Subject: "sub-sess", Email: "sess@x.com", EmailVerified: true,
	}, AuthSessionMetadata{})
	if err == nil {
		t.Fatal("expected session create failure")
	}
}

// TestOIDCHelpersFallbacks 覆盖角色映射与域白名单的兜底分支。
func TestOIDCHelpersFallbacks(t *testing.T) {
	// 非 admin/agent 的默认角色一律归一化为 agent
	if got := mapOIDCRole(nil, nil, "owner"); got != "agent" {
		t.Fatalf("mapOIDCRole(default=owner) = %q, want agent", got)
	}
	// 域白名单开启时，无 @ 的邮箱直接拒绝
	if oidcEmailAllowed([]string{"example.com"}, "no-at-sign") {
		t.Fatal("email without @ must be rejected under domain allowlist")
	}
	// 空白名单放行所有邮箱（对照）
	if !oidcEmailAllowed(nil, "any@x.com") {
		t.Fatal("empty allowlist must allow any email")
	}
}

// TestValidateChallengeTokenZeroUser 覆盖挑战 token 中 user_id 为 0 的拒绝分支。
func TestValidateChallengeTokenZeroUser(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewAuthService(db, twoFactorTestConfig(true))

	token, err := createHS256JWT(map[string]interface{}{
		"exp":       time.Now().Add(time.Minute).Unix(),
		"token_use": challengeTokenUse,
		"user_id":   0,
	}, testAuthConfig().JWT.Secret)
	if err != nil {
		t.Fatalf("craft challenge token: %v", err)
	}
	if _, err := svc.validateChallengeToken(token, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalid2FAChallenge) {
		t.Fatalf("zero-user challenge err = %v, want %v", err, ErrAuthInvalid2FAChallenge)
	}
}
