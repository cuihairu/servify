package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// OIDCIdentity carries the verified IdP claims the local login decides on.
type OIDCIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Roles         []string // raw IdP role/group values (pre-mapping)
}

// LoginWithOIDC signs a local session for a verified external identity.
//
// Matching is by normalized email and requires email_verified. Existing users
// keep their local role (IdP changes never escalate local permissions);
// unknown identities are rejected unless auto-provisioning is enabled, in
// which case the local role comes from role mapping with a safe default.
// Provisioned accounts get an unguessable unusable password so password login
// stays impossible. Banned users are always rejected. The result reuses the
// standard session pipeline, so refresh rotation and revocation policies
// apply unchanged.
func (s *AuthService) LoginWithOIDC(ctx context.Context, identity OIDCIdentity, meta AuthSessionMetadata) (*AuthResult, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	oidcCfg := s.config.OIDC

	email := strings.ToLower(strings.TrimSpace(identity.Email))
	if email == "" || !identity.EmailVerified {
		return nil, ErrOIDCUnverifiedEmail
	}
	if identity.Subject == "" {
		return nil, ErrInvalidAuthInput
	}
	if !oidcEmailAllowed(oidcCfg.AllowedDomains, email) {
		return nil, ErrOIDCDomainNotAllowed
	}

	var user models.User
	err := s.db.WithContext(ctx).Where("LOWER(email) = ?", email).First(&user).Error
	switch {
	case err == nil:
		if strings.TrimSpace(user.Status) == "banned" {
			return nil, ErrAuthUserDisabled
		}
		// 本地角色不随 IdP 变更，防权限漂移。
	case errors.Is(err, gorm.ErrRecordNotFound):
		if !oidcCfg.AutoProvision {
			return nil, ErrOIDCAutoProvisionDisabled
		}
		created, createErr := s.provisionOIDCUser(ctx, identity, oidcCfg.DefaultRole)
		if createErr != nil {
			return nil, createErr
		}
		user = *created
	default:
		return nil, err
	}

	session, err := s.createAuthSession(ctx, user.ID, meta)
	if err != nil {
		return nil, err
	}
	return s.buildAuthResult(ctx, &user, session)
}

// provisionOIDCUser creates the local account for a first-time IdP identity.
func (s *AuthService) provisionOIDCUser(ctx context.Context, identity OIDCIdentity, defaultRole string) (*models.User, error) {
	password, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(identity.Name)
	if name == "" {
		name = strings.TrimSpace(identity.Email)
	}
	user := &models.User{
		Username: oidcUsername(identity),
		Email:    strings.TrimSpace(identity.Email),
		Password: password, // 随机值未登记任何口令；密码登录天然失败，只能走 SSO
		Name:     name,
		Role:     mapOIDCRole(identity.Roles, s.config.OIDC.RoleMapping, defaultRole),
		Status:   "active",
	}
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			// 并发首次登录等场景：唯一键冲突时回落到查询既有用户。
			var existing models.User
			if lookupErr := s.db.WithContext(ctx).
				Where("LOWER(email) = ?", strings.ToLower(strings.TrimSpace(identity.Email))).
				First(&existing).Error; lookupErr == nil {
				return &existing, nil
			}
		}
		return nil, err
	}
	return user, nil
}

// oidcUsername derives a unique username from the verified email (unique key
// on users.username would collide for local parts shared across domains).
func oidcUsername(identity OIDCIdentity) string {
	email := strings.ToLower(strings.TrimSpace(identity.Email))
	parts := strings.SplitN(email, "@", 2)
	local := "sso"
	if len(parts) == 2 && parts[0] != "" {
		local = parts[0]
	}
	suffix, err := randomHex(4)
	if err != nil {
		return local
	}
	return fmt.Sprintf("%s_%s", local, suffix)
}

// mapOIDCRole maps raw IdP role/group values onto local roles. Only known
// local roles result; anything else falls back to defaultRole.
func mapOIDCRole(idpRoles []string, mapping map[string]string, defaultRole string) string {
	if defaultRole != "admin" && defaultRole != "agent" {
		defaultRole = "agent"
	}
	for _, raw := range idpRoles {
		value := strings.ToLower(strings.TrimSpace(raw))
		for local, idpValue := range mapping {
			if strings.EqualFold(strings.TrimSpace(idpValue), value) && (local == "admin" || local == "agent") {
				return local
			}
		}
	}
	return defaultRole
}

// oidcEmailAllowed enforces the optional allowed_domains restriction.
func oidcEmailAllowed(allowed []string, email string) bool {
	if len(allowed) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, d := range allowed {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
