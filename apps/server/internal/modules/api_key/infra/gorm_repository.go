package infra

// GormRepository 承接 API Key 的 SQL：列表按租户/工作区过滤，
// 明文密钥不落库（仅 prefix + hash）。

import (
	"context"
	"errors"
	"time"

	apikeyapp "servify/apps/server/internal/modules/api_key/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// applyScopeFilter 按上下文租户/工作区过滤（与 services 侧共享语义，
// api_key 迁移后自备一份）。
func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

func (r *GormRepository) List(ctx context.Context) ([]models.APIKey, error) {
	var keys []models.APIKey
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).Order("created_at DESC, id DESC").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

func (r *GormRepository) Create(ctx context.Context, row *models.APIKey) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *GormRepository) Get(ctx context.Context, id uint) (*models.APIKey, error) {
	var row models.APIKey
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apikeyapp.ErrAPIKeyNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (r *GormRepository) Revoke(ctx context.Context, id uint, revokedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&models.APIKey{}).Where("id = ?", id).
		Updates(map[string]interface{}{"revoked_at": revokedAt, "updated_at": revokedAt}).Error
}

func (r *GormRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.APIKey{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apikeyapp.ErrAPIKeyNotFound
	}
	return nil
}
