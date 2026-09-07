package disabled

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

func TestRecordingProvider_StartRecording_Disabled(t *testing.T) {
	p := NewRecordingProvider()
	recordingID, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{CallID: "call-1"})
	require.Error(t, err)
	assert.Empty(t, recordingID)

	var providerErr *voiceapp.ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, voiceapp.ProviderErrorUnavailable, providerErr.Code)
	assert.Equal(t, "voice recording provider is disabled", providerErr.Message)
	assert.False(t, providerErr.Retryable)
}

func TestRecordingProvider_StopRecording_Disabled(t *testing.T) {
	p := NewRecordingProvider()
	err := p.StopRecording(context.Background(), voiceapp.StopRecordingCommand{RecordingID: "rec-1"})
	require.Error(t, err)

	var providerErr *voiceapp.ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, voiceapp.ProviderErrorUnavailable, providerErr.Code)
	assert.Equal(t, "voice recording provider is disabled", providerErr.Message)
	assert.False(t, providerErr.Retryable)
}

func TestTranscriptProvider_AppendTranscript_Disabled(t *testing.T) {
	p := NewTranscriptProvider()
	err := p.AppendTranscript(context.Background(), voiceapp.AppendTranscriptCommand{CallID: "call-1", Content: "text"})
	require.Error(t, err)

	var providerErr *voiceapp.ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, voiceapp.ProviderErrorUnavailable, providerErr.Code)
	assert.Equal(t, "voice transcript provider is disabled", providerErr.Message)
	assert.False(t, providerErr.Retryable)
}
