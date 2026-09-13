package infra

import (
	"context"
	"fmt"
	"sync"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

type InMemoryRecordingRepository struct {
	mu         sync.Mutex
	recordings map[string]voiceapp.RecordingDTO
}

func NewInMemoryRecordingRepository() *InMemoryRecordingRepository {
	return &InMemoryRecordingRepository{recordings: make(map[string]voiceapp.RecordingDTO)}
}

func (r *InMemoryRecordingRepository) Save(ctx context.Context, recording voiceapp.RecordingDTO) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordings[recording.ID] = recording
	return nil
}

func (r *InMemoryRecordingRepository) MarkStopped(ctx context.Context, recordingID string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	recording, ok := r.recordings[recordingID]
	if !ok {
		return fmt.Errorf("recording not found")
	}
	recording.Status = "stopped"
	r.recordings[recordingID] = recording
	return nil
}

func (r *InMemoryRecordingRepository) FindByID(ctx context.Context, recordingID string) (*voiceapp.RecordingDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	recording, ok := r.recordings[recordingID]
	if !ok {
		return nil, fmt.Errorf("recording not found")
	}
	copy := recording
	return &copy, nil
}

var _ voiceapp.RecordingRepository = (*InMemoryRecordingRepository)(nil)

// UpsertCompleted 按录音 ID 落终态:存在则补 status/storage_uri,不存在则建完成态。
func (r *InMemoryRecordingRepository) UpsertCompleted(ctx context.Context, recording voiceapp.RecordingDTO) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.recordings[recording.ID]
	if !ok {
		recording.Status = "stopped"
		r.recordings[recording.ID] = recording
		return nil
	}
	existing.Status = "stopped"
	if recording.StorageURI != "" {
		existing.StorageURI = recording.StorageURI
	}
	r.recordings[recording.ID] = existing
	return nil
}
