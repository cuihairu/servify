package infra

// app_integration 模块基础设施层：应用市场集成 GORM 仓储（自 services/app_integration_service.go 迁入）。

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"

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

func (r *GormRepository) CountBySlug(ctx context.Context, slug string) (int64, error) {
	var exists int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.AppIntegration{}), ctx).Where("slug = ?", slug).Count(&exists).Error; err != nil {
		return 0, err
	}
	return exists, nil
}

// integrationQuery 组装 scope 与列表过滤条件。
func integrationQuery(ctx context.Context, db *gorm.DB, req *appintegrationapp.AppIntegrationListRequest) *gorm.DB {
	query := applyScopeFilter(db.WithContext(ctx).Model(&models.AppIntegration{}), ctx)
	if req.Category != "" {
		query = query.Where("category = ?", req.Category)
	}
	if req.Search != "" {
		term := "%" + req.Search + "%"
		query = query.Where("LOWER(name) LIKE LOWER(?) OR LOWER(vendor) LIKE LOWER(?) OR LOWER(summary) LIKE LOWER(?)", term, term, term)
	}
	if len(req.Status) == 1 {
		if req.Status[0] == "enabled" {
			query = query.Where("enabled = ?", true)
		} else if req.Status[0] == "disabled" {
			query = query.Where("enabled = ?", false)
		}
	}
	return query
}

func (r *GormRepository) CountIntegrations(ctx context.Context, req *appintegrationapp.AppIntegrationListRequest) (int64, error) {
	var total int64
	if err := integrationQuery(ctx, r.db, req).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *GormRepository) ListIntegrations(ctx context.Context, req *appintegrationapp.AppIntegrationListRequest, offset, limit int) ([]models.AppIntegration, error) {
	var list []models.AppIntegration
	q := integrationQuery(ctx, r.db, req)
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Order("created_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *GormRepository) GetIntegration(ctx context.Context, id uint) (*models.AppIntegration, error) {
	var model models.AppIntegration
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, appintegrationapp.ErrIntegrationNotFound
		}
		return nil, err
	}
	return &model, nil
}

func (r *GormRepository) CreateIntegration(ctx context.Context, model *models.AppIntegration) error {
	return r.db.WithContext(ctx).Create(model).Error
}

func (r *GormRepository) SaveIntegration(ctx context.Context, model *models.AppIntegration) error {
	return r.db.WithContext(ctx).Save(model).Error
}

func (r *GormRepository) DeleteIntegration(ctx context.Context, id uint) error {
	result := applyScopeFilter(r.db.WithContext(ctx), ctx).Delete(&models.AppIntegration{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return appintegrationapp.ErrIntegrationNotFound
	}
	return nil
}
