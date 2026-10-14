package infra

import (
	"context"
	"errors"
	slapp "servify/apps/server/internal/modules/auth/application"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"gorm.io/gorm"
)

// TestLoginWithOIDCLookupError 覆盖首查非 NotFound 错误的透传分支。
func TestLoginWithOIDCLookupError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), oidcTestConfig(nil))
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_oidc_lookup", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("boom oidc lookup"))
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	_, err := svc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{
		Subject: "sub-lookup", Email: "lookup@x.com", EmailVerified: true,
	}, slapp.AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "boom oidc lookup") {
		t.Fatalf("err = %v, want lookup failure", err)
	}
}

// TestLoginWithOIDCSessionCreateError 覆盖建号成功但会话创建失败。
func TestLoginWithOIDCSessionCreateError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))
	if err := db.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}

	_, err := svc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{
		Subject: "sub-sess", Email: "sess@x.com", EmailVerified: true,
	}, slapp.AuthSessionMetadata{})
	if err == nil {
		t.Fatal("expected session create failure")
	}
}
