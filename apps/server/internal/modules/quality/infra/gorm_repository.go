package infra

import (
	"context"
	"errors"
	qualitydomain "servify/apps/server/internal/modules/quality/domain"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/quality/application"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormRepository 基于 GORM 的质检仓储（postgres 与 sqlite 通用）。
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) ListReviewCandidates(ctx context.Context, lookback time.Time, limit int) ([]models.Session, error) {
	var out []models.Session
	err := r.db.WithContext(ctx).
		Where("status = ? AND ended_at IS NOT NULL AND ended_at >= ?", "ended", lookback).
		Where("NOT EXISTS (SELECT 1 FROM quality_reviews q WHERE q.session_id = sessions.id)").
		Order("ended_at ASC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// ListReviewsForRetry 扫 pending（含 rescore 重置与 worker 中断滞留）与 failed（退避到期）。
func (r *GormRepository) ListReviewsForRetry(ctx context.Context, maxAttempts int, now time.Time, limit int) ([]qualitydomain.QualityReview, error) {
	var out []qualitydomain.QualityReview
	err := r.db.WithContext(ctx).
		Where("status IN ? AND attempt_count < ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
			[]string{application.StatusPending, application.StatusFailed}, maxAttempts, now).
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

func (r *GormRepository) GetReviewBySession(ctx context.Context, sessionID string) (*qualitydomain.QualityReview, error) {
	var review qualitydomain.QualityReview
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&review).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrNotFound
		}
		return nil, err
	}
	return &review, nil
}

func (r *GormRepository) ListMessages(ctx context.Context, sessionID string) ([]models.Message, error) {
	var out []models.Message
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&out).Error
	return out, err
}

func (r *GormRepository) InsertReviewIfAbsent(ctx context.Context, review *qualitydomain.QualityReview) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "session_id"}}, DoNothing: true}).
		Create(review)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *GormRepository) MarkReviewScored(ctx context.Context, sessionID string, allowedFrom []string, fields application.ScoredFields) (bool, error) {
	res := r.db.WithContext(ctx).Model(&qualitydomain.QualityReview{}).
		Where("session_id = ? AND status IN ?", sessionID, allowedFrom).
		Updates(map[string]any{
			"status":          "scored",
			"dimensions_json": fields.DimensionsJSON,
			"llm_total_score": fields.TotalScore,
			"llm_summary":     fields.Summary,
			"llm_provider":    fields.Provider,
			"llm_model":       fields.Model,
			"scored_at":       fields.ScoredAt,
			"next_retry_at":   nil,
			"last_error":      "",
		})
	return res.RowsAffected > 0, res.Error
}

func (r *GormRepository) MarkReviewFailed(ctx context.Context, sessionID string, allowedFrom []string, attempts int, nextRetry time.Time, lastErr string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&qualitydomain.QualityReview{}).
		Where("session_id = ? AND status IN ?", sessionID, allowedFrom).
		Updates(map[string]any{
			"status":        "failed",
			"attempt_count": attempts,
			"next_retry_at": nextRetry,
			"last_error":    lastErr,
		})
	return res.RowsAffected > 0, res.Error
}

func (r *GormRepository) ListReviews(ctx context.Context, query application.ReviewListQuery) ([]qualitydomain.QualityReview, int64, error) {
	tx := r.db.WithContext(ctx).Model(&qualitydomain.QualityReview{})
	if query.Status != "" {
		tx = tx.Where("status = ?", query.Status)
	}
	if query.AgentID != nil {
		tx = tx.Where("agent_id = ?", *query.AgentID)
	}
	if query.CustomerID != nil {
		tx = tx.Where("customer_id = ?", *query.CustomerID)
	}
	if query.HasViolations != nil {
		if *query.HasViolations {
			tx = tx.Where("violation_count > 0")
		} else {
			tx = tx.Where("violation_count = 0")
		}
	}
	if query.Severity != "" {
		tx = tx.Where("max_severity = ?", query.Severity)
	}
	if query.MinScore != nil {
		tx = tx.Where("llm_total_score >= ?", *query.MinScore)
	}
	if query.MaxScore != nil {
		tx = tx.Where("llm_total_score <= ?", *query.MaxScore)
	}
	if query.From != nil {
		tx = tx.Where("created_at >= ?", *query.From)
	}
	if query.To != nil {
		tx = tx.Where("created_at <= ?", *query.To)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := query.Page, query.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	var out []qualitydomain.QualityReview
	err := tx.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&out).Error
	return out, total, err
}

func (r *GormRepository) ConfirmReview(ctx context.Context, sessionID string, cmd application.ConfirmCommand) (bool, error) {
	updates := map[string]any{
		"status":        "confirmed",
		"manual_result": cmd.ManualResult,
		"review_note":   cmd.ReviewNote,
		"reviewed_by":   cmd.ReviewedBy,
		"reviewed_at":   time.Now(),
		"next_retry_at": nil,
		"last_error":    "",
	}
	if cmd.ManualScore != nil {
		updates["manual_score"] = *cmd.ManualScore
	}
	res := r.db.WithContext(ctx).Model(&qualitydomain.QualityReview{}).
		Where("session_id = ? AND status = ?", sessionID, application.StatusScored).
		Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *GormRepository) RescheduleReview(ctx context.Context, sessionID string, force bool) (bool, error) {
	tx := r.db.WithContext(ctx).Model(&qualitydomain.QualityReview{}).
		Where("session_id = ?", sessionID)
	if !force {
		tx = tx.Where("status <> ?", application.StatusConfirmed)
	}
	res := tx.Updates(map[string]any{
		"status":        "pending",
		"trigger":       "rescore",
		"attempt_count": 0,
		"next_retry_at": nil,
		"last_error":    "",
	})
	return res.RowsAffected > 0, res.Error
}
