package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

func NewUserStateTokenPolicy(db *gorm.DB) TokenPolicy {
	if db == nil {
		return nil
	}

	return func(payload map[string]interface{}, claims Claims, now time.Time) error {
		if !claims.HasUserID || claims.UserID == 0 {
			return nil
		}

		var user models.User
		if err := db.WithContext(ContextWithScope(context.Background(), claims.TenantID, claims.WorkspaceID)).
			Select("id", "status", "token_valid_after", "token_version").
			First(&user, claims.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("token user no longer exists")
			}
			return errors.New("failed to evaluate token state")
		}

		status := strings.ToLower(strings.TrimSpace(user.Status))
		if status != "" && status != "active" {
			return errors.New("token user is not active")
		}

		if user.TokenValidAfter != nil && !user.TokenValidAfter.IsZero() {
			if err := RejectIssuedBefore(user.TokenValidAfter.Unix())(payload, claims, now); err != nil {
				return err
			}
		}
		if user.TokenVersion > 0 {
			if err := RequireMinimumTokenVersion(int64(user.TokenVersion))(payload, claims, now); err != nil {
				return err
			}
		}

		if claims.SessionID == "" {
			return nil
		}

		var session models.UserAuthSession
		if err := db.WithContext(ContextWithScope(context.Background(), claims.TenantID, claims.WorkspaceID)).
			Select("id", "user_id", "status", "token_version", "revoked_at").
			First(&session, "id = ? AND user_id = ?", claims.SessionID, claims.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("token session no longer exists")
			}
			return errors.New("failed to evaluate token session state")
		}
		if strings.ToLower(strings.TrimSpace(session.Status)) != "active" || session.RevokedAt != nil {
			return errors.New("token session is not active")
		}
		if session.TokenVersion > 0 {
			version, ok := int64Claim(payload, "session_token_version", "stv")
			if !ok {
				return errors.New("token missing session_token_version required by session policy")
			}
			if version < int64(session.TokenVersion) {
				return errors.New("token has been revoked by session policy")
			}
		}

		return nil
	}
}

func NewRevokedTokenPolicy(db *gorm.DB) TokenPolicy {
	if db == nil {
		return nil
	}

	return func(payload map[string]interface{}, claims Claims, now time.Time) error {
		if claims.TokenID == "" {
			return nil
		}

		var revoked models.RevokedToken
		err := db.WithContext(ContextWithScope(context.Background(), claims.TenantID, claims.WorkspaceID)).
			Select("id", "jti", "expires_at", "revoked_at").
			Where("jti = ?", claims.TokenID).
			First(&revoked).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return errors.New("failed to evaluate revoked token state")
		}
		if revoked.ExpiresAt != nil && !revoked.ExpiresAt.IsZero() && !revoked.ExpiresAt.After(now) {
			return nil
		}
		return errors.New("token has been explicitly revoked")
	}
}
