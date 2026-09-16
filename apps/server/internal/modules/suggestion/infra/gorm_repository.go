package infra

import (
	"context"

	"servify/apps/server/internal/models"
	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) FindTicketCandidates(ctx context.Context, tokens []string, candidateMax int) ([]suggestionapp.TicketCandidate, error) {
	q := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).
		Select("id, title, description, status, category, priority, created_at").
		Order("created_at DESC")

	where, args := suggestionapp.BuildLikeWhereTokens([]string{"title", "description"}, tokens, 3)
	if where != "" {
		q = q.Where(where, args...)
	}

	var rows []suggestionapp.TicketCandidate
	if err := q.Limit(candidateMax).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *GormRepository) FindKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]suggestionapp.KnowledgeDocCandidate, error) {
	q := applyScopeFilter(r.db.WithContext(ctx).Model(&models.KnowledgeDoc{}), ctx).
		Select("id, title, content, category, tags").
		Order("created_at DESC")

	where, args := suggestionapp.BuildLikeWhereTokens([]string{"title", "content", "tags"}, tokens, 3)
	if where != "" {
		q = q.Where(where, args...)
	}

	var rows []suggestionapp.KnowledgeDocCandidate
	if err := q.Limit(300).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindPublicKnowledgeDocs 客户侧首屏推荐（P2-0 RQ-1）：仅 is_public
// 文档，updated_at 新近优先。
func (r *GormRepository) FindPublicKnowledgeDocs(ctx context.Context, limit int) ([]suggestionapp.KnowledgeDocCandidate, error) {
	var rows []suggestionapp.KnowledgeDocCandidate
	err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.KnowledgeDoc{}), ctx).
		Select("id, title, category").
		Where("is_public = ?", true).
		Order("updated_at DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// FindPublicKnowledgeDocCandidates 客户侧上下文联想（P2-0 RQ-2）：仅
// is_public 文档的 token 候选集；空 token（客户消息无可用词元）时直接
// 返回空集，不做全表扫描。
func (r *GormRepository) FindPublicKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]suggestionapp.KnowledgeDocCandidate, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	q := applyScopeFilter(r.db.WithContext(ctx).Model(&models.KnowledgeDoc{}), ctx).
		Select("id, title, content, category, tags").
		Where("is_public = ?", true).
		Order("created_at DESC")

	where, args := suggestionapp.BuildLikeWhereTokens([]string{"title", "content", "tags"}, tokens, 3)
	if where != "" {
		q = q.Where(where, args...)
	}

	var rows []suggestionapp.KnowledgeDocCandidate
	if err := q.Limit(300).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}
