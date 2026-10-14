package infra

import (
	"context"
	"errors"
	slapp "servify/apps/server/internal/modules/auth/application"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// TestLoginWithOIDCGuards 覆盖入口的 nil-db 与 subject 校验分支。
func TestLoginWithOIDCGuards(t *testing.T) {
	nilSvc := &slapp.AuthService{}
	if _, err := nilSvc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{}, slapp.AuthSessionMetadata{}); !errors.Is(err, slapp.ErrAuthInvalidDB) {
		t.Fatalf("nil db err = %v, want %v", err, slapp.ErrAuthInvalidDB)
	}

	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), oidcTestConfig(nil))
	if _, err := svc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{
		Email: "a@x.com", EmailVerified: true, Subject: "",
	}, slapp.AuthSessionMetadata{}); !errors.Is(err, slapp.ErrInvalidAuthInput) {
		t.Fatalf("empty subject err = %v, want %v", err, slapp.ErrInvalidAuthInput)
	}
}

// TestLoginWithOIDCProvisionCreateError 覆盖首次登录建号失败透传。
func TestLoginWithOIDCProvisionCreateError(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_user_create", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("boom user create"))
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	_, err := svc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{
		Subject: "sub-1", Email: "new@x.com", EmailVerified: true, Name: "New",
	}, slapp.AuthSessionMetadata{})
	if err == nil || !strings.Contains(err.Error(), "boom user create") {
		t.Fatalf("err = %v, want provision create failure", err)
	}
}

// TestLoginWithOIDCProvisionDuplicateFallback 覆盖并发首登下唯一键冲突回落查询的路径。
func TestLoginWithOIDCProvisionDuplicateFallback(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	existing := &models.User{ID: 71, Username: "ssouser", Email: "dup@x.com", Password: "x", Status: "active", Role: "agent"}
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("seed existing user: %v", err)
	}

	// 模拟竞态：第一次查询假装没找到，插入时撞唯一键，回落查询必须命中已有用户
	queries := 0
	if err := db.Callback().Query().Before("gorm:query").Register("test:first_query_missing", func(tx *gorm.DB) {
		queries++
		if queries == 1 && tx.Statement != nil && tx.Statement.Table == "users" {
			_ = tx.AddError(gorm.ErrRecordNotFound)
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:duplicate_user_create", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "users" {
			// 生产判定按小写子串匹配，用 Postgres 风格文案
			_ = tx.AddError(errors.New("duplicate key value violates unique constraint"))
		}
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}

	result, err := svc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{
		Subject: "sub-dup", Email: "dup@x.com", EmailVerified: true, Name: "Dup",
	}, slapp.AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("LoginWithOIDC() error = %v", err)
	}
	if result.User.ID != existing.ID {
		t.Fatalf("fallback user id = %d, want %d", result.User.ID, existing.ID)
	}
	if result.Token == "" {
		t.Fatal("expected session token for fallback user")
	}
}

// TestLoginWithOIDCProvisionNameFallback 覆盖 Name 为空时回退 email。
func TestLoginWithOIDCProvisionNameFallback(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	result, err := svc.LoginWithOIDC(context.Background(), slapp.OIDCIdentity{
		Subject: "sub-2", Email: "noname@x.com", EmailVerified: true, Name: "   ",
	}, slapp.AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("LoginWithOIDC() error = %v", err)
	}
	if result.User.Name != "noname@x.com" {
		t.Fatalf("name = %q, want email fallback", result.User.Name)
	}
}
