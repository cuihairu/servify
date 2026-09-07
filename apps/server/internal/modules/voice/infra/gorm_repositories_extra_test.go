package infra

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"servify/apps/server/internal/models"
	voiceapp "servify/apps/server/internal/modules/voice/application"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupSingleConnectionDB opens an in-memory sqlite database restricted to a
// single connection so that PRAGMA statements affect every query.
func setupSingleConnectionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.VoiceCall{}, &models.VoiceRecording{}, &models.VoiceTranscript{}))
	return db
}

// setQueryOnly marks the database connection read-only; reads keep working
// while writes fail, which exercises the write-error branches.
func setQueryOnly(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec("PRAGMA query_only = true").Error)
}

func TestGormRepositoryStartCallFallsBackToConnectionID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	call, err := repo.StartCall(t.Context(), voiceapp.StartCallCommand{
		SessionID:    "s1",
		ConnectionID: "conn-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "conn-1", call.ID)
	assert.Equal(t, "s1", call.SessionID)

	found, ok := repo.GetCall("conn-1")
	require.True(t, ok)
	assert.Equal(t, "conn-1", found.ID)
}

func TestGormRepositoryStartCallCreateError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	setQueryOnly(t, db)
	repo := NewGormRepository(db)

	_, err := repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create voice call")
}

func TestGormRepositoryCallMutationsMissingCall(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)

	_, err := repo.HoldCall(t.Context(), voiceapp.HoldCallCommand{CallID: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call not found")

	_, err = repo.ResumeCall(t.Context(), voiceapp.ResumeCallCommand{CallID: "missing"})
	require.Error(t, err)

	_, err = repo.EndCall(t.Context(), voiceapp.EndCallCommand{CallID: "missing"})
	require.Error(t, err)

	_, err = repo.TransferCall(t.Context(), voiceapp.TransferCallCommand{CallID: "missing"})
	require.Error(t, err)
}

func TestGormRepositoryCallSaveErrors(t *testing.T) {
	db := setupSingleConnectionDB(t)
	repo := NewGormRepository(db)

	_, err := repo.StartCall(t.Context(), voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})
	require.NoError(t, err)

	setQueryOnly(t, db)

	_, err = repo.AnswerCall(t.Context(), voiceapp.AnswerCallCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save answer")

	_, err = repo.HoldCall(t.Context(), voiceapp.HoldCallCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save hold")

	_, err = repo.ResumeCall(t.Context(), voiceapp.ResumeCallCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save resume")

	_, err = repo.EndCall(t.Context(), voiceapp.EndCallCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save end")

	_, err = repo.TransferCall(t.Context(), voiceapp.TransferCallCommand{CallID: "c1", ToAgentID: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save transfer")
}

func TestGormRecordingRepositorySaveError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	setQueryOnly(t, db)
	repo := NewGormRecordingRepository(db)

	err := repo.Save(t.Context(), voiceapp.RecordingDTO{ID: "rec-1", CallID: "c1", Status: "recording", StartedAt: time.Now()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save recording")
}

func TestGormRecordingRepositoryMarkStoppedUpdateError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	repo := NewGormRecordingRepository(db)

	require.NoError(t, repo.Save(t.Context(), voiceapp.RecordingDTO{ID: "rec-1", CallID: "c1", Status: "recording", StartedAt: time.Now()}))
	setQueryOnly(t, db)

	err := repo.MarkStopped(t.Context(), "rec-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mark recording stopped")
}

func TestGormRecordingRepositoryFindByIDMissing(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRecordingRepository(db)

	_, err := repo.FindByID(t.Context(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recording not found")
}

func TestGormTranscriptRepositoryAppendError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	setQueryOnly(t, db)
	repo := NewGormTranscriptRepository(db)

	err := repo.Append(t.Context(), voiceapp.TranscriptDTO{CallID: "c1", Content: "hello"})
	require.Error(t, err)
}

func TestGormTranscriptRepositoryListAll(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormTranscriptRepository(db)

	appendTranscript := func(callID, content string) {
		require.NoError(t, repo.Append(t.Context(), voiceapp.TranscriptDTO{CallID: callID, Content: content, Language: "en"}))
		time.Sleep(10 * time.Millisecond)
	}
	appendTranscript("c1", "first")
	appendTranscript("c1", "second")
	appendTranscript("c2", "third")

	items, total, err := repo.ListAll(t.Context(), 1, 2)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.EqualValues(t, 3, total)

	items, total, err = repo.ListAll(t.Context(), 2, 2)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.EqualValues(t, 3, total)

	items, total, err = repo.ListAll(t.Context(), 3, 2)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.EqualValues(t, 3, total)
}

func TestGormTranscriptRepositoryListAllCountError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.VoiceTranscript{}))
	repo := NewGormTranscriptRepository(db)

	_, _, err := repo.ListAll(t.Context(), 1, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "voice_transcripts")
}

func TestGormTranscriptRepositoryListAllQueryError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	// The table still exists so Count succeeds, but ordering on the dropped
	// column makes the paged query fail.
	require.NoError(t, db.Migrator().DropColumn(&models.VoiceTranscript{}, "created_at"))
	repo := NewGormTranscriptRepository(db)

	_, _, err := repo.ListAll(t.Context(), 1, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "created_at")
}

func TestGormTranscriptRepositoryListByCallIDQueryError(t *testing.T) {
	db := setupSingleConnectionDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.VoiceTranscript{}))
	repo := NewGormTranscriptRepository(db)

	_, err := repo.ListByCallID(t.Context(), "c1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "voice_transcripts")
}
