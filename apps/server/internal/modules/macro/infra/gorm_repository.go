package infra

// GormRepository 承接宏的 SQL：列表/增删改查按租户/工作区过滤，
// 工单读取同样走 scope（ApplyToTicket 的跨工作区拒绝语义）。

import (
	"context"
	"errors"

	macroapp "servify/apps/server/internal/modules/macro/application"
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
// macro 迁移后自备一份）。
func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

func (r *GormRepository) List(ctx context.Context) ([]models.Macro, error) {
	var macros []models.Macro
	// Sort by most recently updated first; use ID as deterministic tie-breaker for same timestamp
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).Order("updated_at DESC, id DESC").Find(&macros).Error; err != nil {
		return nil, err
	}
	return macros, nil
}

func (r *GormRepository) Create(ctx context.Context, macro *models.Macro) error {
	return r.db.WithContext(ctx).Create(macro).Error
}

func (r *GormRepository) GetScoped(ctx context.Context, id uint) (*models.Macro, error) {
	var macro models.Macro
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&macro, id).Error; err != nil {
		return nil, err
	}
	return &macro, nil
}

func (r *GormRepository) Save(ctx context.Context, macro *models.Macro) error {
	return r.db.WithContext(ctx).Save(macro).Error
}

func (r *GormRepository) Delete(ctx context.Context, id uint) error {
	result := applyScopeFilter(r.db.WithContext(ctx), ctx).Delete(&models.Macro{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return macroapp.ErrMacroNotFound
	}
	return nil
}

func (r *GormRepository) GetTicket(ctx context.Context, id uint) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&ticket, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, macroapp.ErrTicketNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

func (r *GormRepository) CreateComment(ctx context.Context, comment *models.TicketComment) error {
	return r.db.WithContext(ctx).Create(comment).Error
}
