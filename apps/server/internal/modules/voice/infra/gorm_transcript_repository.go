package infra

import (
	"context"

	voiceapp "servify/apps/server/internal/modules/voice/application"

	"gorm.io/gorm"
)

// compile-time interface check
var _ voiceapp.TranscriptRepository = (*GormTranscriptRepository)(nil)

type GormTranscriptRepository struct {
	db *gorm.DB
}

func NewGormTranscriptRepository(db *gorm.DB) *GormTranscriptRepository {
	return &GormTranscriptRepository{db: db}
}

func (r *GormTranscriptRepository) Append(ctx context.Context, transcript voiceapp.TranscriptDTO) error {
	m := VoiceTranscript{
		CallID:    transcript.CallID,
		Content:   transcript.Content,
		Language:  transcript.Language,
		Finalized: transcript.Finalized,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return err
	}
	return nil
}

func (r *GormTranscriptRepository) ListByCallID(ctx context.Context, callID string) ([]voiceapp.TranscriptDTO, error) {
	var records []VoiceTranscript
	if err := r.db.WithContext(ctx).
		Where("call_id = ?", callID).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]voiceapp.TranscriptDTO, len(records))
	for i, r := range records {
		out[i] = voiceapp.TranscriptDTO{
			CallID:     r.CallID,
			Content:    r.Content,
			Language:   r.Language,
			Finalized:  r.Finalized,
			AppendedAt: r.CreatedAt,
		}
	}
	return out, nil
}

func (r *GormTranscriptRepository) ListAll(ctx context.Context, page, pageSize int) ([]voiceapp.TranscriptDTO, int64, error) {
	var records []VoiceTranscript
	var total int64

	if err := r.db.WithContext(ctx).Model(&VoiceTranscript{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}

	out := make([]voiceapp.TranscriptDTO, len(records))
	for i, r := range records {
		out[i] = voiceapp.TranscriptDTO{
			CallID:     r.CallID,
			Content:    r.Content,
			Language:   r.Language,
			Finalized:  r.Finalized,
			AppendedAt: r.CreatedAt,
		}
	}
	return out, total, nil
}
