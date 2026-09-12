package infra

import (
	"context"
	"errors"
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

func (r *GormRepository) ListReviewsForRetry(ctx context.Context, maxAttempts int, now time.Time, limit int) ([]models.QualityReview, error) {
	var out []models.QualityReview
	err := r.db.WithContext(ctx).
		Where("status = ? AND attempt_count < ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", "failed", maxAttempts, now).
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

func (r *GormRepository) GetReviewBySession(ctx context.Context, sessionID string) (*models.QualityReview, error) {
	var review models.QualityReview
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

func (r *GormRepository) InsertReviewIfAbsent(ctx context.Context, review *models.QualityReview) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "session_id"}}, DoNothing: true}).
		Create(review)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *GormRepository) MarkReviewScored(ctx context.Context, sessionID string, allowedFrom []string, fields application.ScoredFields) (bool, error) {
	res := r.db.WithContext(ctx).Model(&models.QualityReview{}).
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
	res := r.db.WithContext(ctx).Model(&models.QualityReview{}).
		Where("session_id = ? AND status IN ?", sessionID, allowedFrom).
		Updates(map[string]any{
			"status":        "failed",
			"attempt_count": attempts,
			"next_retry_at": nextRetry,
			"last_error":    lastErr,
		})
	return res.RowsAffected > 0, res.Error
}
