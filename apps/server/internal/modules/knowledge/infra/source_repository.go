package infra

import (
	"context"
	"strings"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	"servify/apps/server/internal/modules/knowledge/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// GormSourceRepository 来源登记仓储（B3-1a §8.1）：与文档仓储同款 scope
// 过滤（tenant/workspace 经 platformauth 上下文推导）。
type GormSourceRepository struct {
	db *gorm.DB
}

func NewGormSourceRepository(db *gorm.DB) *GormSourceRepository {
	return &GormSourceRepository{db: db}
}

func (r *GormSourceRepository) Create(ctx context.Context, source *domain.Source) error {
	if source == nil {
		return gorm.ErrInvalidValue
	}
	model := &domain.KnowledgeSource{
		TenantID:    platformauth.TenantIDFromContext(ctx),
		WorkspaceID: platformauth.WorkspaceIDFromContext(ctx),
		Name:        source.Name,
		Type:        source.Type,
		Description: source.Description,
		CreatedAt:   source.CreatedAt,
		UpdatedAt:   source.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	source.ID = model.ID
	return nil
}

func (r *GormSourceRepository) Update(ctx context.Context, source *domain.Source) error {
	if source == nil {
		return gorm.ErrInvalidValue
	}
	result := applyKnowledgeScope(r.db.WithContext(ctx).Model(&domain.KnowledgeSource{}), ctx).Where("id = ?", source.ID).Updates(map[string]interface{}{
		"name":        source.Name,
		"type":        source.Type,
		"description": source.Description,
		"updated_at":  source.UpdatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormSourceRepository) Delete(ctx context.Context, id uint) error {
	result := applyKnowledgeScope(r.db.WithContext(ctx), ctx).Delete(&domain.KnowledgeSource{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormSourceRepository) Get(ctx context.Context, id uint) (*domain.Source, error) {
	var model domain.KnowledgeSource
	if err := applyKnowledgeScope(r.db.WithContext(ctx), ctx).First(&model, id).Error; err != nil {
		return nil, err
	}
	return sourceFromModel(model), nil
}

func (r *GormSourceRepository) List(ctx context.Context, filter knowledgeapp.ListSourcesFilter) ([]domain.Source, error) {
	q := applyKnowledgeScope(r.db.WithContext(ctx).Model(&domain.KnowledgeSource{}), ctx)
	if t := strings.TrimSpace(filter.Type); t != "" {
		q = q.Where("type = ?", t)
	}
	var rows []domain.KnowledgeSource
	if err := q.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Source, 0, len(rows))
	for _, row := range rows {
		out = append(out, *sourceFromModel(row))
	}
	return out, nil
}

// CountDocuments 引用该来源的文档数（删除守卫）。
func (r *GormSourceRepository) CountDocuments(ctx context.Context, sourceID uint) (int64, error) {
	var count int64
	err := applyKnowledgeScope(r.db.WithContext(ctx).Model(&domain.KnowledgeDoc{}), ctx).
		Where("source_id = ?", sourceID).
		Count(&count).Error
	return count, err
}

func sourceFromModel(model domain.KnowledgeSource) *domain.Source {
	return &domain.Source{
		ID:          model.ID,
		Name:        model.Name,
		Type:        model.Type,
		Description: model.Description,
		CreatedAt:   model.CreatedAt,
		UpdatedAt:   model.UpdatedAt,
	}
}
