package infra

import (
	"context"
	"errors"
	"fmt"

	voiceapp "servify/apps/server/internal/modules/voice/application"

	"gorm.io/gorm"
)

// compile-time interface check
var _ voiceapp.RecordingRepository = (*GormRecordingRepository)(nil)

type GormRecordingRepository struct {
	db *gorm.DB
}

func NewGormRecordingRepository(db *gorm.DB) *GormRecordingRepository {
	return &GormRecordingRepository{db: db}
}

func (r *GormRecordingRepository) Save(ctx context.Context, recording voiceapp.RecordingDTO) error {
	m := VoiceRecording{
		ID:         recording.ID,
		CallID:     recording.CallID,
		Provider:   recording.Provider,
		Status:     recording.Status,
		StorageURI: recording.StorageURI,
		StartedAt:  recording.StartedAt,
	}
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return fmt.Errorf("save recording: %w", err)
	}
	return nil
}

func (r *GormRecordingRepository) MarkStopped(ctx context.Context, recordingID string) error {
	result := r.db.WithContext(ctx).
		Model(&VoiceRecording{}).
		Where("id = ?", recordingID).
		Update("status", "stopped")
	if result.Error != nil {
		return fmt.Errorf("mark recording stopped: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("recording not found")
	}
	return nil
}

func (r *GormRecordingRepository) FindByID(ctx context.Context, recordingID string) (*voiceapp.RecordingDTO, error) {
	var m VoiceRecording
	if err := r.db.WithContext(ctx).First(&m, "id = ?", recordingID).Error; err != nil {
		return nil, fmt.Errorf("recording not found: %w", err)
	}
	dto := voiceapp.RecordingDTO{
		ID:         m.ID,
		CallID:     m.CallID,
		Provider:   m.Provider,
		Status:     m.Status,
		StorageURI: m.StorageURI,
		StartedAt:  m.StartedAt,
	}
	return &dto, nil
}

// UpsertCompleted 按录音 ID 落终态:存在则补 status/storage_uri(保留原
// started_at),不存在则直接建完成态记录。同 ID 重复回调天然幂等。
func (r *GormRecordingRepository) UpsertCompleted(ctx context.Context, recording voiceapp.RecordingDTO) error {
	var m VoiceRecording
	err := r.db.WithContext(ctx).First(&m, "id = ?", recording.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m = VoiceRecording{
			ID:         recording.ID,
			CallID:     recording.CallID,
			Provider:   recording.Provider,
			Status:     "stopped",
			StorageURI: recording.StorageURI,
			StartedAt:  recording.StartedAt,
		}
		if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
			return fmt.Errorf("create completed recording: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find recording: %w", err)
	}
	m.Status = "stopped"
	if recording.StorageURI != "" {
		m.StorageURI = recording.StorageURI
	}
	if err := r.db.WithContext(ctx).Save(&m).Error; err != nil {
		return fmt.Errorf("save completed recording: %w", err)
	}
	return nil
}
