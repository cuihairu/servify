package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"golang.org/x/crypto/bcrypt"
)

func oidcTestConfig(mutate func(*config.OIDCConfig)) *config.Config {
	cfg := testAuthConfig()
	cfg.OIDC.Enabled = true
	cfg.OIDC.AutoProvision = false
	cfg.OIDC.DefaultRole = "agent"
	cfg.OIDC.RoleMapping = map[string]string{"admin": "idp-admins"}
	if mutate != nil {
		mutate(&cfg.OIDC)
	}
	return cfg
}

func oidcIdentity(email string, verified bool, roles ...string) OIDCIdentity {
	return OIDCIdentity{
		Subject:       "idp-sub-" + email,
		Email:         email,
		EmailVerified: verified,
		Name:          "SSO User",
		Roles:         roles,
	}
}

func TestLoginWithOIDCRequiresVerifiedEmail(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, oidcTestConfig(nil))

	if _, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("a@example.com", false), AuthSessionMetadata{}); err != ErrOIDCUnverifiedEmail {
		t.Fatalf("unverified email: got %v, want ErrOIDCUnverifiedEmail", err)
	}
	if _, err := svc.LoginWithOIDC(context.Background(), OIDCIdentity{Subject: "s", EmailVerified: true}, AuthSessionMetadata{}); err != ErrOIDCUnverifiedEmail {
		t.Fatalf("missing email: got %v, want ErrOIDCUnverifiedEmail", err)
	}
}

func TestLoginWithOIDCRejectsUnknownWithoutAutoProvision(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, oidcTestConfig(nil))

	_, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("new@example.com", true), AuthSessionMetadata{})
	if err != ErrOIDCAutoProvisionDisabled {
		t.Fatalf("got %v, want ErrOIDCAutoProvisionDisabled", err)
	}
}

func TestLoginWithOIDCProvisionsAndReusesAccount(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	first, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("provision@example.com", true, "idp-admins"), AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if first.User.Role != "admin" {
		t.Fatalf("provisioned role = %q, want admin (role mapping)", first.User.Role)
	}
	if first.Token == "" || first.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens")
	}

	// 同一 email 二次登录必须复用账号，而不是再建一个。
	second, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("provision@example.com", true), AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.User.ID != first.User.ID {
		t.Fatalf("second login created a new user: %d vs %d", second.User.ID, first.User.ID)
	}
	var count int64
	db.Model(&models.User{}).Where("email = ?", "provision@example.com").Count(&count)
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}

	// 已存在用户的本地角色不因 IdP 角色变化而升级。
	if second.User.Role != "admin" {
		t.Fatalf("role drifted to %q", second.User.Role)
	}
}

func TestLoginWithOIDCProvisionedPasswordIsUnusable(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	if _, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("nopass@example.com", true), AuthSessionMetadata{}); err != nil {
		t.Fatalf("provision login: %v", err)
	}
	var user models.User
	if err := db.Where("email = ?", "nopass@example.com").First(&user).Error; err != nil {
		t.Fatalf("load provisioned user: %v", err)
	}
	// 存储的是随机值（非 bcrypt 哈希格式）：任何密码登录都必然失败。
	if strings.HasPrefix(user.Password, "$2") {
		t.Fatal("provisioned password should be a random value, not a real hash")
	}
	if _, err := svc.Login(context.Background(), LoginInput{Username: user.Username, Password: user.Password}, AuthSessionMetadata{}); err == nil {
		t.Fatal("password login with stored secret must fail")
	}
}

func TestLoginWithOIDCDefaultRole(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	result, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("agent-role@example.com", true, "unknown-group"), AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.User.Role != "agent" {
		t.Fatalf("role = %q, want default agent", result.User.Role)
	}
}

func TestLoginWithOIDCDomainAllowlist(t *testing.T) {
	db := newAuthServiceTestDB(t)
	svc := NewService(db, oidcTestConfig(func(c *config.OIDCConfig) {
		c.AutoProvision = true
		c.AllowedDomains = []string{"corp.example.com"}
	}))

	if _, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("outsider@example.com", true), AuthSessionMetadata{}); err != ErrOIDCDomainNotAllowed {
		t.Fatalf("got %v, want ErrOIDCDomainNotAllowed", err)
	}
	if _, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("insider@CORP.example.com", true), AuthSessionMetadata{}); err != nil {
		t.Fatalf("allowed domain rejected: %v", err)
	}
}

func TestLoginWithOIDCBannedUserRejected(t *testing.T) {
	db := newAuthServiceTestDB(t)
	if err := db.Create(&models.User{
		Username: "banned-sso",
		Email:    "banned@example.com",
		Password: "x",
		Role:     "admin",
		Status:   "banned",
	}).Error; err != nil {
		t.Fatalf("seed banned user: %v", err)
	}
	svc := NewService(db, oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	if _, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("banned@example.com", true), AuthSessionMetadata{}); err != ErrAuthUserDisabled {
		t.Fatalf("got %v, want ErrAuthUserDisabled", err)
	}
}

func TestLoginWithOIDCIgnoresIdPRoleForExistingUser(t *testing.T) {
	db := newAuthServiceTestDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.Create(&models.User{
		Username: "plain-customer",
		Email:    "plain@example.com",
		Password: string(hash),
		Role:     "customer",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc := NewService(db, oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true }))

	result, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("plain@example.com", true, "idp-admins"), AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.User.Role != "customer" {
		t.Fatalf("existing user role escalated to %q", result.User.Role)
	}
}

func TestLoginWithOIDCSessionRefreshable(t *testing.T) {
	db := newAuthServiceTestDB(t)
	cfg := oidcTestConfig(func(c *config.OIDCConfig) { c.AutoProvision = true })
	cfg.JWT.RefreshExpiresIn = time.Hour
	svc := NewService(db, cfg)

	result, err := svc.LoginWithOIDC(context.Background(), oidcIdentity("refresh@example.com", true), AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	rotated, err := svc.RefreshToken(context.Background(), result.RefreshToken, AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.Token == "" || rotated.SessionID == "" {
		t.Fatal("expected rotated session tokens")
	}
}
