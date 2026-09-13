package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"

	"gorm.io/gorm"
)

// GormRepository 基于 GORM 的 webhook 仓储（postgres 与 sqlite 通用）。
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	var out []models.WebhookEndpoint
	err := r.db.WithContext(ctx).Order("id").Find(&out).Error
	return out, err
}

func (r *GormRepository) GetEndpoint(ctx context.Context, id uint) (*models.WebhookEndpoint, error) {
	var ep models.WebhookEndpoint
	if err := r.db.WithContext(ctx).First(&ep, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrNotFound
		}
		return nil, err
	}
	return &ep, nil
}

func (r *GormRepository) CreateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	return r.db.WithContext(ctx).Create(ep).Error
}

func (r *GormRepository) UpdateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	return r.db.WithContext(ctx).Save(ep).Error
}

func (r *GormRepository) DeleteEndpoint(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&models.WebhookEndpoint{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return application.ErrNotFound
	}
	return nil
}

func (r *GormRepository) CreateDelivery(ctx context.Context, d *models.WebhookDelivery) error {
	return r.db.WithContext(ctx).Create(d).Error
}

func (r *GormRepository) ListDeliveries(ctx context.Context, query application.DeliveryListQuery) ([]models.WebhookDelivery, int64, error) {
	db := r.db.WithContext(ctx).Model(&models.WebhookDelivery{})
	if query.EndpointID != 0 {
		db = db.Where("endpoint_id = ?", query.EndpointID)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []models.WebhookDelivery
	offset := (query.Page - 1) * query.PageSize
	err := db.Order("id DESC").Offset(offset).Limit(query.PageSize).Find(&out).Error
	return out, total, err
}

func (r *GormRepository) GetDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	var d models.WebhookDelivery
	if err := r.db.WithContext(ctx).First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

// ResetDeliveryForRedeliver 状态回 pending、attempt 归零并清空重试时间。
func (r *GormRepository) ResetDeliveryForRedeliver(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Model(&models.WebhookDelivery{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        models.WebhookDeliveryStatusPending,
		"attempt":       0,
		"last_error":    "",
		"next_retry_at": nil,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return application.ErrNotFound
	}
	return nil
}

func (r *GormRepository) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]models.WebhookDelivery, error) {
	var out []models.WebhookDelivery
	err := r.db.WithContext(ctx).
		Where("status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", models.WebhookDeliveryStatusPending, now).
		Order("id").Limit(limit).Find(&out).Error
	return out, err
}

func (r *GormRepository) MarkDeliverySuccess(ctx context.Context, id uint, httpStatus int, durationMs int64, deliveredAt time.Time) error {
	res := r.db.WithContext(ctx).Model(&models.WebhookDelivery{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        models.WebhookDeliveryStatusSuccess,
		"attempt":       gorm.Expr("attempt + 1"),
		"http_status":   httpStatus,
		"duration_ms":   durationMs,
		"last_error":    "",
		"next_retry_at": nil,
		"delivered_at":  deliveredAt,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("delivery %d not found", id)
	}
	return nil
}

func (r *GormRepository) MarkDeliveryFailure(ctx context.Context, id uint, attempt int, httpStatus int, durationMs int64, lastError string, dead bool, nextRetryAt *time.Time) error {
	status := models.WebhookDeliveryStatusPending
	if dead {
		status = models.WebhookDeliveryStatusDead
	}
	res := r.db.WithContext(ctx).Model(&models.WebhookDelivery{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        status,
		"attempt":       attempt,
		"http_status":   httpStatus,
		"duration_ms":   durationMs,
		"last_error":    lastError,
		"next_retry_at": nextRetryAt,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("delivery %d not found", id)
	}
	return nil
}

func (r *GormRepository) GetTicketSnapshot(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := r.db.WithContext(ctx).First(&ticket, ticketID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

func (r *GormRepository) GetSessionSnapshot(ctx context.Context, sessionID string) (*models.Session, error) {
	var session models.Session
	if err := r.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (r *GormRepository) GetCallSnapshot(ctx context.Context, callID string) (*models.VoiceCall, error) {
	var call models.VoiceCall
	if err := r.db.WithContext(ctx).First(&call, "id = ?", callID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrNotFound
		}
		return nil, err
	}
	return &call, nil
}

var _ application.Repository = (*GormRepository)(nil)
