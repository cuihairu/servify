package mock

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

func TestTranscriptProvider_AppendAccumulates(t *testing.T) {
	p := NewTranscriptProvider()
	require.Empty(t, p.Transcripts)

	cmd1 := voiceapp.AppendTranscriptCommand{CallID: "call-1", Content: "first", Language: "en", Finalized: true}
	cmd2 := voiceapp.AppendTranscriptCommand{CallID: "call-1", Content: "second", Language: "en"}

	require.NoError(t, p.AppendTranscript(context.Background(), cmd1))
	require.NoError(t, p.AppendTranscript(context.Background(), cmd2))
	assert.Equal(t, []voiceapp.AppendTranscriptCommand{cmd1, cmd2}, p.Transcripts)
}

func TestTranscriptProvider_AppendError(t *testing.T) {
	p := NewTranscriptProvider()
	p.AppendErr = errors.New("append failed")

	err := p.AppendTranscript(context.Background(), voiceapp.AppendTranscriptCommand{CallID: "call-1"})
	require.Error(t, err)
	assert.Equal(t, "append failed", err.Error())
	assert.Empty(t, p.Transcripts)
}
