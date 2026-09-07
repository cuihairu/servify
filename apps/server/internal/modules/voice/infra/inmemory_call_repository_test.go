package infra

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

func TestInMemoryRepositoryStartCallFallsBackToConnectionID(t *testing.T) {
	repo := NewInMemoryRepository()
	call, err := repo.StartCall(context.Background(), voiceapp.StartCallCommand{
		SessionID:    "s1",
		ConnectionID: "conn-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "conn-1", call.ID)
	assert.Equal(t, "s1", call.SessionID)
	assert.Equal(t, "started", call.Status)
	assert.False(t, call.StartedAt.IsZero())

	_, ok := repo.GetCall("conn-1")
	assert.True(t, ok)
}

func TestInMemoryRepositoryLifecycle(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.StartCall(ctx, voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})
	require.NoError(t, err)

	call, err := repo.AnswerCall(ctx, voiceapp.AnswerCallCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "answered", call.Status)
	require.NotNil(t, call.AnsweredAt)

	call, err = repo.HoldCall(ctx, voiceapp.HoldCallCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "held", call.Status)
	require.NotNil(t, call.HeldAt)

	call, err = repo.ResumeCall(ctx, voiceapp.ResumeCallCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "answered", call.Status)
	require.NotNil(t, call.ResumedAt)

	call, err = repo.EndCall(ctx, voiceapp.EndCallCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "ended", call.Status)
	require.NotNil(t, call.EndedAt)

	call, err = repo.TransferCall(ctx, voiceapp.TransferCallCommand{CallID: "c1", ToAgentID: 8})
	require.NoError(t, err)
	assert.Equal(t, "transferred", call.Status)
	require.NotNil(t, call.TransferToAgent)
	assert.EqualValues(t, 8, *call.TransferToAgent)
}

func TestInMemoryRepositoryMissingCallErrors(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.AnswerCall(ctx, voiceapp.AnswerCallCommand{CallID: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call not found")

	_, err = repo.HoldCall(ctx, voiceapp.HoldCallCommand{CallID: "missing"})
	require.Error(t, err)

	_, err = repo.ResumeCall(ctx, voiceapp.ResumeCallCommand{CallID: "missing"})
	require.Error(t, err)

	_, err = repo.EndCall(ctx, voiceapp.EndCallCommand{CallID: "missing"})
	require.Error(t, err)

	_, err = repo.TransferCall(ctx, voiceapp.TransferCallCommand{CallID: "missing"})
	require.Error(t, err)
}

func TestInMemoryRepositoryGetCall(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.StartCall(ctx, voiceapp.StartCallCommand{CallID: "c1", SessionID: "s1"})
	require.NoError(t, err)

	call, ok := repo.GetCall("c1")
	require.True(t, ok)
	assert.Equal(t, "c1", call.ID)

	// GetCall must return a copy so callers cannot mutate stored state.
	call.Status = "mutated"
	fresh, ok := repo.GetCall("c1")
	require.True(t, ok)
	assert.Equal(t, "started", fresh.Status)

	_, ok = repo.GetCall("missing")
	assert.False(t, ok)
}

func TestInMemoryRecordingRepositoryNotFound(t *testing.T) {
	repo := NewInMemoryRecordingRepository()
	ctx := context.Background()

	err := repo.MarkStopped(ctx, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recording not found")

	_, err = repo.FindByID(ctx, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recording not found")
}

func TestInMemoryRecordingRepositoryFindByIDReturnsCopy(t *testing.T) {
	repo := NewInMemoryRecordingRepository()
	ctx := context.Background()

	require.NoError(t, repo.Save(ctx, voiceapp.RecordingDTO{ID: "rec-1", CallID: "c1", Status: "recording"}))
	found, err := repo.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	assert.Equal(t, "rec-1", found.ID)

	found.Status = "mutated"
	again, err := repo.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	assert.Equal(t, "recording", again.Status)
}

func TestInMemoryTranscriptRepositoryListAll(t *testing.T) {
	repo := NewInMemoryTranscriptRepository()
	ctx := context.Background()

	// Empty repository: the start offset is beyond the collected items.
	items, total, err := repo.ListAll(ctx, 1, 2)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.EqualValues(t, 0, total)

	require.NoError(t, repo.Append(ctx, voiceapp.TranscriptDTO{CallID: "c1", Content: "a"}))
	require.NoError(t, repo.Append(ctx, voiceapp.TranscriptDTO{CallID: "c1", Content: "b"}))
	require.NoError(t, repo.Append(ctx, voiceapp.TranscriptDTO{CallID: "c2", Content: "c"}))

	items, total, err = repo.ListAll(ctx, 1, 2)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.EqualValues(t, 3, total)

	// Last page is shorter than the page size.
	items, total, err = repo.ListAll(ctx, 2, 2)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.EqualValues(t, 3, total)

	// Page beyond the end returns an empty page.
	items, total, err = repo.ListAll(ctx, 5, 2)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.EqualValues(t, 3, total)
}

func TestInMemoryTranscriptRepositoryListByCallIDReturnsCopy(t *testing.T) {
	repo := NewInMemoryTranscriptRepository()
	ctx := context.Background()

	require.NoError(t, repo.Append(ctx, voiceapp.TranscriptDTO{CallID: "c1", Content: "a"}))
	items, err := repo.ListByCallID(ctx, "c1")
	require.NoError(t, err)
	require.Len(t, items, 1)

	items[0].Content = "mutated"
	fresh, err := repo.ListByCallID(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, "a", fresh[0].Content)

	empty, err := repo.ListByCallID(ctx, "other")
	require.NoError(t, err)
	assert.Empty(t, empty)
}
