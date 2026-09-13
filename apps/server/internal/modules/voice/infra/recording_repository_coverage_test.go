package infra

import (
	"testing"

	"servify/apps/server/internal/models"
	voiceapp "servify/apps/server/internal/modules/voice/application"

	"github.com/stretchr/testify/require"
)

// TestGormRecordingRepositoryUpsertCompleted 覆盖新建与更新两条路径。
func TestGormRecordingRepositoryUpsertCompleted(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRecordingRepository(db)
	ctx := t.Context()

	// 回调先于 start 到达：直接建完成态记录
	require.NoError(t, repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{
		ID: "rec-1", CallID: "call-1", Provider: "twilio", StorageURI: "http://example.com/a.mp3",
	}))
	got, err := repo.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	require.Equal(t, "stopped", got.Status)
	require.Equal(t, "http://example.com/a.mp3", got.StorageURI)
	require.Equal(t, "call-1", got.CallID)

	// 已存在：带 URI 的重复回调刷新 URI
	require.NoError(t, repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{
		ID: "rec-1", StorageURI: "http://example.com/b.mp3",
	}))
	got, err = repo.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	require.Equal(t, "stopped", got.Status)
	require.Equal(t, "http://example.com/b.mp3", got.StorageURI)

	// 已存在：不带 URI 的重复回调保留原 URI
	require.NoError(t, repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{ID: "rec-1"}))
	got, err = repo.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	require.Equal(t, "http://example.com/b.mp3", got.StorageURI)
}

// TestGormRecordingRepositoryUpsertCompletedFindError 用删表触发查找错误分支。
func TestGormRecordingRepositoryUpsertCompletedFindError(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.VoiceRecording{}))
	err := NewGormRecordingRepository(db).UpsertCompleted(t.Context(), voiceapp.RecordingDTO{ID: "rec-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "find recording")
}

// TestGormRecordingRepositoryUpsertCompletedWriteErrors 用重命名列制造“查询可读但写入失败”。
func TestGormRecordingRepositoryUpsertCompletedWriteErrors(t *testing.T) {
	ctx := t.Context()

	// 空表 + 列缺失：First 返回 NotFound，Create 失败
	db := setupTestDB(t)
	require.NoError(t, db.Exec("ALTER TABLE voice_recordings RENAME COLUMN storage_uri TO storage_uri_legacy").Error)
	err := NewGormRecordingRepository(db).UpsertCompleted(ctx, voiceapp.RecordingDTO{ID: "rec-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "create completed recording")

	// 已有行 + 列缺失：First 命中，Save 失败
	db = setupTestDB(t)
	repo := NewGormRecordingRepository(db)
	require.NoError(t, repo.Save(ctx, voiceapp.RecordingDTO{ID: "rec-2", CallID: "call-2", Status: "recording"}))
	require.NoError(t, db.Exec("ALTER TABLE voice_recordings RENAME COLUMN storage_uri TO storage_uri_legacy").Error)
	err = repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{ID: "rec-2", StorageURI: "http://example.com/x.mp3"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "save completed recording")
}

// TestGormRepositoryFindByID 覆盖命中与未命中。
func TestGormRepositoryFindByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGormRepository(db)
	ctx := t.Context()

	if _, err := repo.FindByID(ctx, "ghost"); err == nil {
		t.Fatal("unknown call must fail")
	}

	started, err := repo.StartCall(ctx, voiceapp.StartCallCommand{CallID: "call-1", SessionID: "sess-1"})
	require.NoError(t, err)
	got, err := repo.FindByID(ctx, "call-1")
	require.NoError(t, err)
	require.Equal(t, started.ID, got.ID)
	require.Equal(t, "started", got.Status)
}

// TestInMemoryRecordingRepositoryUpsertCompleted 覆盖新建与更新两条路径。
func TestInMemoryRecordingRepositoryUpsertCompleted(t *testing.T) {
	repo := NewInMemoryRecordingRepository()
	ctx := t.Context()

	require.NoError(t, repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{
		ID: "rec-1", CallID: "call-1", StorageURI: "http://example.com/a.mp3",
	}))
	got, err := repo.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	require.Equal(t, "stopped", got.Status)
	require.Equal(t, "http://example.com/a.mp3", got.StorageURI)

	require.NoError(t, repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{ID: "rec-1", StorageURI: "http://example.com/b.mp3"}))
	got, _ = repo.FindByID(ctx, "rec-1")
	require.Equal(t, "http://example.com/b.mp3", got.StorageURI)

	require.NoError(t, repo.UpsertCompleted(ctx, voiceapp.RecordingDTO{ID: "rec-1"}))
	got, _ = repo.FindByID(ctx, "rec-1")
	require.Equal(t, "http://example.com/b.mp3", got.StorageURI, "empty URI must keep the stored value")
}

// TestInMemoryRepositoryFindByID 覆盖命中与未命中。
func TestInMemoryRepositoryFindByID(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := t.Context()

	if _, err := repo.FindByID(ctx, "ghost"); err == nil {
		t.Fatal("unknown call must fail")
	}

	started, err := repo.StartCall(ctx, voiceapp.StartCallCommand{CallID: "call-1"})
	require.NoError(t, err)
	got, err := repo.FindByID(ctx, "call-1")
	require.NoError(t, err)
	require.Equal(t, started.ID, got.ID)
}
