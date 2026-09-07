package mock

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

func TestRecordingProvider_StartRecording_AssignsSequentialIDs(t *testing.T) {
	p := NewRecordingProvider()
	cmd := voiceapp.StartRecordingCommand{CallID: "call-1", Provider: "mock"}

	id1, err := p.StartRecording(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, "rec-1", id1)

	id2, err := p.StartRecording(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, "rec-2", id2)

	p.mu.Lock()
	stored, ok := p.recordings["rec-1"]
	count := len(p.recordings)
	p.mu.Unlock()
	assert.True(t, ok)
	assert.Equal(t, cmd, stored)
	assert.Equal(t, 2, count)
}

func TestRecordingProvider_StartRecording_Error(t *testing.T) {
	p := NewRecordingProvider()
	p.StartErr = errors.New("start failed")

	id, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{CallID: "call-1"})
	require.Error(t, err)
	assert.Equal(t, "start failed", err.Error())
	assert.Empty(t, id)
	assert.Empty(t, p.recordings)
	assert.Equal(t, 0, p.nextSequence)
}

func TestRecordingProvider_StopRecording_RemovesEntry(t *testing.T) {
	p := NewRecordingProvider()
	id, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{CallID: "call-1"})
	require.NoError(t, err)

	require.NoError(t, p.StopRecording(context.Background(), voiceapp.StopRecordingCommand{RecordingID: id}))

	p.mu.Lock()
	_, exists := p.recordings[id]
	p.mu.Unlock()
	assert.False(t, exists)
}

func TestRecordingProvider_StopRecording_UnknownID(t *testing.T) {
	p := NewRecordingProvider()
	require.NoError(t, p.StopRecording(context.Background(), voiceapp.StopRecordingCommand{RecordingID: "rec-missing"}))
}

func TestRecordingProvider_StopRecording_Error(t *testing.T) {
	p := NewRecordingProvider()
	p.StopErr = errors.New("stop failed")

	err := p.StopRecording(context.Background(), voiceapp.StopRecordingCommand{RecordingID: "rec-1"})
	require.Error(t, err)
	assert.Equal(t, "stop failed", err.Error())
}

func TestRecordingProvider_ConcurrentStartsProduceUniqueIDs(t *testing.T) {
	p := NewRecordingProvider()
	const n = 20
	ids := make(chan string, n)

	for i := 0; i < n; i++ {
		go func() {
			id, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{CallID: "call-1"})
			assert.NoError(t, err)
			ids <- id
		}()
	}

	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := <-ids
		assert.False(t, seen[id], "duplicate recording id %s", id)
		seen[id] = true
	}

	p.mu.Lock()
	count := len(p.recordings)
	p.mu.Unlock()
	assert.Equal(t, n, count)
}
