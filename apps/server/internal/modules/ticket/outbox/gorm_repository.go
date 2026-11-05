package outbox

import (
	"context"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

const (
	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusDead      = "dead"
)

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
	err := r.db.WithContext(ctx).
		Where("status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)",
			[]string{StatusPending, StatusFailed}, now).
		Order("id").Limit(limit).Find(&out).Error
	return out, err
}

func (r *GormRepository) MarkDelivered(ctx context.Context, id uint, deliveredAt time.Time) error {
	return r.db.WithContext(ctx).Model(&models.TicketOutbox{}).Where("id = ?", id).
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
	return r.db.WithContext(ctx).Model(&models.TicketOutbox{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":          status,
			"attempts":        attempt,
			"last_error":      lastError,
			"next_attempt_at": nextAttemptAt,
			"updated_at":      time.Now(),
		}).Error
}
