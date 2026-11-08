package outbox

import (
	"context"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDelivered  = "delivered"
	StatusFailed     = "failed"
	StatusDead       = "dead"
)

// claimStaleAfter 是 processing 行视为崩溃残留、允许重新领取的阈值。
const claimStaleAfter = 60 * time.Second

// GormRepository implements Repository on GORM (postgres and sqlite shared).
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Append(ctx context.Context, row *models.TicketOutbox) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *GormRepository) ClaimDue(ctx context.Context, now time.Time, limit int) ([]models.TicketOutbox, error) {
	var out []models.TicketOutbox
	// 原子领取：候选行先查询，再逐行以条件 UPDATE 抢占（status 仍为待办/
	// 失败且未过期，或为崩溃残留的 processing 且超过阈值），受影响行数=1
	// 才归本实例投递。单库单语句原子，多实例不会同时投递同一行。
	var candidates []models.TicketOutbox
	err := r.db.WithContext(ctx).
		Where(`(
			status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		) OR (
			status = ? AND claimed_at IS NOT NULL AND claimed_at <= ?
		)`,
			[]string{StatusPending, StatusFailed}, now,
			StatusProcessing, now.Add(-claimStaleAfter)).
		Order("id").Limit(limit).Find(&candidates).Error
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		res := r.db.WithContext(ctx).Model(&models.TicketOutbox{}).
			Where(`id = ? AND (
				(status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
				OR (status = ? AND claimed_at IS NOT NULL AND claimed_at <= ?)
			)`, candidate.ID,
				[]string{StatusPending, StatusFailed}, now,
				StatusProcessing, now.Add(-claimStaleAfter)).
			Updates(map[string]interface{}{
				"status":     StatusProcessing,
				"claimed_at": now,
				"updated_at": now,
			})
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 1 {
			candidate.Status = StatusProcessing
			candidate.ClaimedAt = &now
			out = append(out, candidate)
		}
	}
	return out, nil
}

func (r *GormRepository) MarkDelivered(ctx context.Context, id uint, deliveredAt time.Time) error {
	return r.db.WithContext(ctx).Model(&models.TicketOutbox{}).
		Where("id = ? AND status = ?", id, StatusProcessing).
		Updates(map[string]interface{}{
			"status":       StatusDelivered,
			"delivered_at": deliveredAt,
			"updated_at":   deliveredAt,
		}).Error
}

func (r *GormRepository) MarkFailed(ctx context.Context, id uint, attempt int, lastError string, nextAttemptAt *time.Time, dead bool) error {
	status := StatusFailed
	if dead {
		status = StatusDead
	}
	return r.db.WithContext(ctx).Model(&models.TicketOutbox{}).
		Where("id = ? AND status = ?", id, StatusProcessing).
		Updates(map[string]interface{}{
			"status":          status,
			"attempts":        attempt,
			"last_error":      lastError,
			"next_attempt_at": nextAttemptAt,
			"updated_at":      time.Now(),
		}).Error
}
