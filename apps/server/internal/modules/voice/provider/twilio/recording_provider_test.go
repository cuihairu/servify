package twilio

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

func TestNewRecordingProvider_DefaultsBaseURL(t *testing.T) {
	p := NewRecordingProvider(Config{AccountSID: "sid", AuthToken: "token"})
	require.NotNil(t, p)
	assert.Equal(t, "https://api.twilio.com", p.config.BaseURL)
}

func TestNewRecordingProvider_KeepsCustomBaseURL(t *testing.T) {
	p := NewRecordingProvider(Config{AccountSID: "sid", AuthToken: "token", BaseURL: "http://example.test"})
	assert.Equal(t, "http://example.test", p.config.BaseURL)
}

func TestStartRecording_MissingAccountSID(t *testing.T) {
	p := NewRecordingProvider(Config{AuthToken: "token"})
	recordingID, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{CallID: "call-1"})
	require.Error(t, err)
	assert.Empty(t, recordingID)
	assert.Contains(t, err.Error(), "twilio credentials not configured")
}

func TestStartRecording_MissingAuthToken(t *testing.T) {
	p := NewRecordingProvider(Config{AccountSID: "sid"})
	recordingID, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{CallID: "call-1"})
	require.Error(t, err)
	assert.Empty(t, recordingID)
	assert.Contains(t, err.Error(), "twilio credentials not configured")
}

func TestStartRecording_Success(t *testing.T) {
	p := NewRecordingProvider(Config{AccountSID: "sid", AuthToken: "token"})
	recordingID, err := p.StartRecording(context.Background(), voiceapp.StartRecordingCommand{
		CallID:   "call-9",
		Provider: "twilio",
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(recordingID, "twilio-rec-call-9-"), "unexpected recording id %s", recordingID)
}

func TestStopRecording_MissingCredentials(t *testing.T) {
	p := NewRecordingProvider(Config{})
	err := p.StopRecording(context.Background(), voiceapp.StopRecordingCommand{RecordingID: "rec-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twilio credentials not configured")
}

func TestStopRecording_Success(t *testing.T) {
	p := NewRecordingProvider(Config{AccountSID: "sid", AuthToken: "token"})
	err := p.StopRecording(context.Background(), voiceapp.StopRecordingCommand{RecordingID: "rec-1"})
	require.NoError(t, err)
}
