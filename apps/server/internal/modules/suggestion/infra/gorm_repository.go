package infra

import (
	"context"
	"encoding/json"
	"errors"
	"time"

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

// RecordExposure 落一行曝光（P2-0 RQ-5）：Questions 序列化为 JSON 数组存
// questions 列，与 models.SuggestionExposureLog 逐列对齐。
func (r *GormRepository) RecordExposure(ctx context.Context, rec suggestionapp.ExposureRecord) error {
	// []string 的 json.Marshal 恒成功（契约式忽略 err）
	questions, _ := json.Marshal(rec.Questions)
	return r.db.WithContext(ctx).Create(&models.SuggestionExposureLog{
		SessionID: rec.SessionID,
		Kind:      rec.Kind,
		Strategy:  rec.Strategy,
		Questions: string(questions),
	}).Error
}

// FindLatestOpenExposure 取该 session 最近一次未转化曝光；无则返回
// nil, nil。created_at 同刻时按 id 兜底排序，保证"最近"语义稳定。
func (r *GormRepository) FindLatestOpenExposure(ctx context.Context, sessionID string) (*suggestionapp.OpenExposure, error) {
	var row models.SuggestionExposureLog
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND converted_question = ?", sessionID, "").
		Order("created_at DESC, id DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var questions []string
	if err := json.Unmarshal([]byte(row.Questions), &questions); err != nil {
		return nil, err
	}
	return &suggestionapp.OpenExposure{ID: row.ID, Questions: questions}, nil
}

// MarkExposureConverted 标记转化命中；仅命中仍处未转化状态（converted_question
// 保持空串）的行，并发双命中时后到者不覆盖先到者的归因。
func (r *GormRepository) MarkExposureConverted(ctx context.Context, exposureID uint, question string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&models.SuggestionExposureLog{}).
		Where("id = ? AND converted_question = ?", exposureID, "").
		Updates(map[string]interface{}{"converted_question": question, "converted_at": at}).Error
}

// ExposureSummary 曝光/转化聚合（管理面最小口径）：按 kind 分组计数，
// converted_question 非空即转化。
func (r *GormRepository) ExposureSummary(ctx context.Context) (*suggestionapp.ExposureSummary, error) {
	var rows []struct {
		Kind               string
		TotalExposures     int64
		ConvertedExposures int64
	}
	err := r.db.WithContext(ctx).Model(&models.SuggestionExposureLog{}).
		Select("kind, COUNT(*) AS total_exposures, " +
			"SUM(CASE WHEN converted_question <> '' THEN 1 ELSE 0 END) AS converted_exposures").
		Group("kind").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	summary := &suggestionapp.ExposureSummary{ByKind: make([]suggestionapp.ExposureKindSummary, 0, len(rows))}
	for _, row := range rows {
		summary.TotalExposures += row.TotalExposures
		summary.ConvertedExposures += row.ConvertedExposures
		summary.ByKind = append(summary.ByKind, suggestionapp.ExposureKindSummary{
			Kind:               row.Kind,
			TotalExposures:     row.TotalExposures,
			ConvertedExposures: row.ConvertedExposures,
		})
	}
	return summary, nil
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
