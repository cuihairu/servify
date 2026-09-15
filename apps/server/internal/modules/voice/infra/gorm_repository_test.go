package infra

import (
	"testing"
	"time"

	voiceapp "servify/apps/server/internal/modules/voice/application"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&VoiceCall{}, &VoiceRecording{}, &VoiceTranscript{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// ---- Call Repository ----

func TestGormRepositoryStartCall(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	call, err := repo.StartCall(t.Context(), voiceapp.StartCallCommand{
		CallID:    "call-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	if call.ID != "call-1" {
		t.Errorf("ID = %q, want %q", call.ID, "call-1")
	}
	if call.Status != "started" {
		t.Errorf("Status = %q, want %q", call.Status, "started")
	}
}

func TestGormRepositoryAnswerCall(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, _ = repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})
	call, err := repo.AnswerCall(t.Context(), voiceapp.AnswerCallCommand{CallID: "c1"})
	if err != nil {
		t.Fatalf("AnswerCall: %v", err)
	}
	if call.Status != "answered" {
		t.Errorf("Status = %q, want %q", call.Status, "answered")
	}
	if call.AnsweredAt == nil {
		t.Error("AnsweredAt should not be nil")
	}
}

func TestGormRepositoryEndCall(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, _ = repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})
	_, _ = repo.AnswerCall(t.Context(), voiceapp.AnswerCallCommand{CallID: "c1"})
	call, err := repo.EndCall(t.Context(), voiceapp.EndCallCommand{CallID: "c1"})
	if err != nil {
		t.Fatalf("EndCall: %v", err)
	}
	if call.Status != "ended" {
		t.Errorf("Status = %q, want %q", call.Status, "ended")
	}
}

func TestGormRepositoryHoldResume(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, _ = repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})

	call, err := repo.HoldCall(t.Context(), voiceapp.HoldCallCommand{CallID: "c1"})
	if err != nil {
		t.Fatalf("HoldCall: %v", err)
	}
	if call.Status != "held" {
		t.Errorf("Status = %q, want %q", call.Status, "held")
	}

	call, err = repo.ResumeCall(t.Context(), voiceapp.ResumeCallCommand{CallID: "c1"})
	if err != nil {
		t.Fatalf("ResumeCall: %v", err)
	}
	if call.Status != "answered" {
		t.Errorf("Status = %q, want %q", call.Status, "answered")
	}
}

func TestGormRepositoryTransferCall(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, _ = repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})
	call, err := repo.TransferCall(t.Context(), voiceapp.TransferCallCommand{CallID: "c1", ToAgentID: 42})
	if err != nil {
		t.Fatalf("TransferCall: %v", err)
	}
	if call.Status != "transferred" {
		t.Errorf("Status = %q, want %q", call.Status, "transferred")
	}
	if call.TransferToAgent == nil || *call.TransferToAgent != 42 {
		t.Errorf("TransferToAgent = %v, want 42", call.TransferToAgent)
	}
}

func TestGormRepositoryGetCall(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, _ = repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})

	dto, ok := repo.GetCall("c1")
	if !ok {
		t.Fatal("GetCall should find call")
	}
	if dto.ID != "c1" {
		t.Errorf("ID = %q, want %q", dto.ID, "c1")
	}

	_, ok = repo.GetCall("nonexistent")
	if ok {
		t.Error("GetCall should not find nonexistent call")
	}
}

func TestGormRepositoryCallNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, err := repo.AnswerCall(t.Context(), voiceapp.AnswerCallCommand{CallID: "missing"})
	if err == nil {
		t.Error("expected error for missing call")
	}
}

// ---- Recording Repository ----

func TestGormRecordingRepositorySaveAndFind(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRecordingRepository(db)

	dto := voiceapp.RecordingDTO{
		ID:        "rec-1",
		CallID:    "call-1",
		Provider:  "test",
		Status:    "recording",
		StartedAt: time.Now(),
	}
	if err := repo.Save(t.Context(), dto); err != nil {
		t.Fatalf("Save: %v", err)
	}

	found, err := repo.FindByID(t.Context(), "rec-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.ID != "rec-1" {
		t.Errorf("ID = %q, want %q", found.ID, "rec-1")
	}
}

func TestGormRecordingRepositoryMarkStopped(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRecordingRepository(db)

	_ = repo.Save(t.Context(), voiceapp.RecordingDTO{
		ID: "rec-1", CallID: "c1", Status: "recording", StartedAt: time.Now(),
	})

	if err := repo.MarkStopped(t.Context(), "rec-1"); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}

	found, _ := repo.FindByID(t.Context(), "rec-1")
	if found.Status != "stopped" {
		t.Errorf("Status = %q, want %q", found.Status, "stopped")
	}
}

func TestGormRecordingRepositoryMarkStoppedNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRecordingRepository(db)

	err := repo.MarkStopped(t.Context(), "nonexistent")
	if err == nil {
		t.Error("expected error for missing recording")
	}
}

// ---- Transcript Repository ----

func TestGormTranscriptRepositoryAppendAndList(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormTranscriptRepository(db)

	_ = repo.Append(t.Context(), voiceapp.TranscriptDTO{
		CallID: "c1", Content: "hello", Language: "en", Finalized: false,
	})
	_ = repo.Append(t.Context(), voiceapp.TranscriptDTO{
		CallID: "c1", Content: "world", Language: "en", Finalized: true,
	})

	items, err := repo.ListByCallID(t.Context(), "c1")
	if err != nil {
		t.Fatalf("ListByCallID: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if items[0].Content != "hello" {
		t.Errorf("first = %q, want %q", items[0].Content, "hello")
	}
	if items[1].Content != "world" {
		t.Errorf("second = %q, want %q", items[1].Content, "world")
	}
}

func TestGormTranscriptRepositoryListEmpty(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormTranscriptRepository(db)

	items, err := repo.ListByCallID(t.Context(), "nonexistent")
	if err != nil {
		t.Fatalf("ListByCallID: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}
}
