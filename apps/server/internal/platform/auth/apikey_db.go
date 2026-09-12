package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// GormAPIKeyResolver 基于 api_keys 表的凭据解析器。
type GormAPIKeyResolver struct {
	DB  *gorm.DB
	Now func() time.Time
}

var _ APIKeyResolver = (*GormAPIKeyResolver)(nil)

func NewGormAPIKeyResolver(db *gorm.DB) *GormAPIKeyResolver {
	return &GormAPIKeyResolver{DB: db, Now: time.Now}
}

// ResolveAPIKey 哈希查找并校验吊销/过期；last_used_at 节流 60s 条件更新。
func (r *GormAPIKeyResolver) ResolveAPIKey(ctx context.Context, plaintext string) (*APIKeyRecord, error) {
	plaintext = strings.TrimSpace(plaintext)
	if r.DB == nil || !strings.HasPrefix(plaintext, APIKeyPrefix) {
		return nil, ErrAPIKeyInvalid
	}
	var row models.APIKey
	err := r.DB.WithContext(ctx).Where("key_hash = ?", APIKeyHash(plaintext)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAPIKeyInvalid
	}
	if err != nil {
		return nil, err
	}
	now := r.Now()
	if row.RevokedAt != nil {
		return nil, ErrAPIKeyInvalid
	}
	if row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
		return nil, ErrAPIKeyInvalid
	}
	r.touchLastUsed(ctx, row.ID, now)
	return &APIKeyRecord{
		ID:          row.ID,
		TenantID:    row.TenantID,
		WorkspaceID: row.WorkspaceID,
		Scopes:      APIKeyScopesOrDefault(splitScopes(row.Scopes)),
		ExpiresAt:   row.ExpiresAt,
		RevokedAt:   row.RevokedAt,
	}, nil
}

// touchLastUsed 条件更新（仅当上次记录早于 60s 前），避免高频请求写放大。
func (r *GormAPIKeyResolver) touchLastUsed(ctx context.Context, id uint, now time.Time) {
	_ = r.DB.WithContext(ctx).Model(&models.APIKey{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", id, now.Add(-60*time.Second)).
		UpdateColumn("last_used_at", now).Error
}

func splitScopes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
