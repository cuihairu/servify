package infra

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	voiceapp "servify/apps/server/internal/modules/voice/application"

	"gorm.io/gorm"
)

// compile-time interface check
var _ voiceapp.Repository = (*GormRepository)(nil)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) StartCall(ctx context.Context, cmd voiceapp.StartCallCommand) (*voiceapp.CallDTO, error) {
	callID := cmd.CallID
	if callID == "" {
		callID = cmd.ConnectionID
	}
	now := time.Now()
	m := models.VoiceCall{
		ID:        callID,
		SessionID: cmd.SessionID,
		Status:    "started",
		StartedAt: now,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("create voice call: %w", err)
	}
	return callModelToDTO(&m), nil
}

func (r *GormRepository) AnswerCall(ctx context.Context, cmd voiceapp.AnswerCallCommand) (*voiceapp.CallDTO, error) {
	var m models.VoiceCall
	if err := r.db.WithContext(ctx).First(&m, "id = ?", cmd.CallID).Error; err != nil {
		return nil, fmt.Errorf("call not found: %w", err)
	}
	now := time.Now()
	m.Status = "answered"
	m.AnsweredAt = &now
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return nil, fmt.Errorf("save answer: %w", err)
	}
	return callModelToDTO(&m), nil
}

func (r *GormRepository) HoldCall(ctx context.Context, cmd voiceapp.HoldCallCommand) (*voiceapp.CallDTO, error) {
	var m models.VoiceCall
	if err := r.db.WithContext(ctx).First(&m, "id = ?", cmd.CallID).Error; err != nil {
		return nil, fmt.Errorf("call not found: %w", err)
	}
	now := time.Now()
	m.Status = "held"
	m.HeldAt = &now
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return nil, fmt.Errorf("save hold: %w", err)
	}
	return callModelToDTO(&m), nil
}

func (r *GormRepository) ResumeCall(ctx context.Context, cmd voiceapp.ResumeCallCommand) (*voiceapp.CallDTO, error) {
	var m models.VoiceCall
	if err := r.db.WithContext(ctx).First(&m, "id = ?", cmd.CallID).Error; err != nil {
		return nil, fmt.Errorf("call not found: %w", err)
	}
	now := time.Now()
	m.Status = "answered"
	m.ResumedAt = &now
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return nil, fmt.Errorf("save resume: %w", err)
	}
	return callModelToDTO(&m), nil
}

func (r *GormRepository) EndCall(ctx context.Context, cmd voiceapp.EndCallCommand) (*voiceapp.CallDTO, error) {
	var m models.VoiceCall
	if err := r.db.WithContext(ctx).First(&m, "id = ?", cmd.CallID).Error; err != nil {
		return nil, fmt.Errorf("call not found: %w", err)
	}
	now := time.Now()
	m.Status = "ended"
	m.EndedAt = &now
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return nil, fmt.Errorf("save end: %w", err)
	}
	return callModelToDTO(&m), nil
}

func (r *GormRepository) TransferCall(ctx context.Context, cmd voiceapp.TransferCallCommand) (*voiceapp.CallDTO, error) {
	var m models.VoiceCall
	if err := r.db.WithContext(ctx).First(&m, "id = ?", cmd.CallID).Error; err != nil {
		return nil, fmt.Errorf("call not found: %w", err)
	}
	m.Status = "transferred"
	m.TransferToAgent = &cmd.ToAgentID
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return nil, fmt.Errorf("save transfer: %w", err)
	}
	return callModelToDTO(&m), nil
}

func (r *GormRepository) GetCall(callID string) (*voiceapp.CallDTO, bool) {
	var m models.VoiceCall
	if err := r.db.First(&m, "id = ?", callID).Error; err != nil {
		return nil, false
	}
	return callModelToDTO(&m), true
}

func callModelToDTO(m *models.VoiceCall) *voiceapp.CallDTO {
	return &voiceapp.CallDTO{
		ID:              m.ID,
		SessionID:       m.SessionID,
		Status:          m.Status,
		StartedAt:       m.StartedAt,
		AnsweredAt:      m.AnsweredAt,
		HeldAt:          m.HeldAt,
		ResumedAt:       m.ResumedAt,
		EndedAt:         m.EndedAt,
		TransferToAgent: m.TransferToAgent,
	}
}

// FindByID 供协调层状态守卫查询;未找到返回错误(与状态变更方法同风格)。
func (r *GormRepository) FindByID(ctx context.Context, callID string) (*voiceapp.CallDTO, error) {
	var m models.VoiceCall
	if err := r.db.WithContext(ctx).First(&m, "id = ?", callID).Error; err != nil {
		return nil, fmt.Errorf("call not found: %w", err)
	}
	return callModelToDTO(&m), nil
}
