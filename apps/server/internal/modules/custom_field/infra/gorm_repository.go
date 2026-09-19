package infra

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
	customfieldapp "servify/apps/server/internal/modules/custom_field/application"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// applyScopeFilter 按上下文租户/工作区过滤（与模块内其他仓库一致）。
func applyScopeFilter(q *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	return q
}

func (r *GormRepository) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	q := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomField{}), ctx).Where("resource = ?", resource).Order("id ASC")
	if activeOnly {
		q = q.Where("active = ?", true)
	}
	var fields []models.CustomField
	if err := q.Find(&fields).Error; err != nil {
		return nil, err
	}
	return fields, nil
}

func (r *GormRepository) GetScoped(ctx context.Context, id uint) (*models.CustomField, error) {
	var field models.CustomField
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&field, id).Error; err != nil {
		return nil, err
	}
	return &field, nil
}

func (r *GormRepository) Create(ctx context.Context, field *models.CustomField) error {
	return r.db.WithContext(ctx).Create(field).Error
}

func (r *GormRepository) Save(ctx context.Context, field *models.CustomField) error {
	return r.db.WithContext(ctx).Save(field).Error
}

func (r *GormRepository) Delete(ctx context.Context, id uint) error {
	result := applyScopeFilter(r.db.WithContext(ctx), ctx).Delete(&models.CustomField{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return customfieldapp.ErrCustomFieldNotFound
	}
	return nil
}
