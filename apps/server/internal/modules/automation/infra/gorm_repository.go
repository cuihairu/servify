package infra

import (
	"context"
	"encoding/json"
	"fmt"
	automationdomain "servify/apps/server/internal/modules/automation/domain"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) ListTriggers(ctx context.Context) ([]automationdomain.AutomationTrigger, error) {
	var triggers []automationdomain.AutomationTrigger
	if err := r.db.WithContext(ctx).Order("id DESC").Find(&triggers).Error; err != nil {
		return nil, err
	}
	return triggers, nil
}

func (r *GormRepository) ListActiveTriggersByEvent(ctx context.Context, event string) ([]automationdomain.AutomationTrigger, error) {
	var triggers []automationdomain.AutomationTrigger
	if err := r.db.WithContext(ctx).Where("event = ? AND active = true", event).Order("id ASC").Find(&triggers).Error; err != nil {
		return nil, err
	}
	return triggers, nil
}

func (r *GormRepository) CreateTrigger(ctx context.Context, req automationapp.TriggerRequest) (*automationdomain.AutomationTrigger, error) {
	condJSON, err := json.Marshal(req.Conditions)
	if err != nil {
		return nil, fmt.Errorf("invalid conditions: %w", err)
	}
	actJSON, err := json.Marshal(req.Actions)
	if err != nil {
		return nil, fmt.Errorf("invalid actions: %w", err)
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	trigger := &automationdomain.AutomationTrigger{
		Name:       req.Name,
		Event:      req.Event,
		Conditions: string(condJSON),
		Actions:    string(actJSON),
		Active:     active,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(trigger).Error; err != nil {
		return nil, err
	}
	return trigger, nil
}

func (r *GormRepository) DeleteTrigger(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&automationdomain.AutomationTrigger{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("trigger not found")
	}
	return nil
}

func (r *GormRepository) ListRuns(ctx context.Context, query automationapp.RunListQuery) ([]automationdomain.AutomationRun, int64, error) {
	offset := (query.Page - 1) * query.PageSize
	q := r.db.WithContext(ctx).Model(&automationdomain.AutomationRun{}).Preload("Trigger")
	if query.Status != "" {
		q = q.Where("status = ?", query.Status)
	}
	if query.TriggerID != 0 {
		q = q.Where("trigger_id = ?", query.TriggerID)
	}
	if query.TicketID != 0 {
		q = q.Where("ticket_id = ?", query.TicketID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var runs []automationdomain.AutomationRun
	if err := q.Order("id DESC").Limit(query.PageSize).Offset(offset).Find(&runs).Error; err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

func (r *GormRepository) RecordRun(ctx context.Context, triggerID uint, ticketID uint, status, message string) error {
	return r.db.WithContext(ctx).Create(&automationdomain.AutomationRun{
		TriggerID: triggerID,
		TicketID:  ticketID,
		Status:    status,
		Message:   message,
		CreatedAt: time.Now(),
	}).Error
}

func (r *GormRepository) GetTicket(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := r.db.WithContext(ctx).First(&ticket, ticketID).Error; err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (r *GormRepository) UpdateTicketPriority(ctx context.Context, ticketID uint, priority string) error {
	return r.db.WithContext(ctx).Model(&models.Ticket{}).Where("id = ?", ticketID).Update("priority", priority).Error
}

func (r *GormRepository) UpdateTicketTags(ctx context.Context, ticketID uint, tags string) error {
	return r.db.WithContext(ctx).Model(&models.Ticket{}).Where("id = ?", ticketID).Update("tags", tags).Error
}

func (r *GormRepository) CreateTicketComment(ctx context.Context, ticketID uint, content string) error {
	return r.db.WithContext(ctx).Create(&models.TicketComment{
		TicketID:  ticketID,
		UserID:    0,
		Content:   content,
		Type:      "system",
		CreatedAt: time.Now(),
	}).Error
}

func (r *GormRepository) CreateTimer(ctx context.Context, timer *automationdomain.AutomationTimer) error {
	return r.db.WithContext(ctx).Create(timer).Error
}

func (r *GormRepository) ClaimDueTimers(ctx context.Context, now time.Time, limit int) ([]automationdomain.AutomationTimer, error) {
	var timers []automationdomain.AutomationTimer
	if err := r.db.WithContext(ctx).
		Where("status = ? AND due_at <= ?", automationapp.TimerStatusPending, now).
		Order("due_at, id").
		Limit(limit).
		Find(&timers).Error; err != nil {
		return nil, err
	}
	return timers, nil
}

func (r *GormRepository) CompleteTimer(ctx context.Context, id uint, now time.Time) bool {
	result := r.db.WithContext(ctx).Model(&automationdomain.AutomationTimer{}).
		Where("id = ? AND status = ?", id, automationapp.TimerStatusPending).
		Updates(map[string]interface{}{"status": automationapp.TimerStatusDone, "executed_at": now})
	return result.Error == nil && result.RowsAffected > 0
}

func (r *GormRepository) UpdateTimerLastError(ctx context.Context, id uint, message string) error {
	return r.db.WithContext(ctx).Model(&automationdomain.AutomationTimer{}).
		Where("id = ?", id).
		Update("last_error", message).Error
}
