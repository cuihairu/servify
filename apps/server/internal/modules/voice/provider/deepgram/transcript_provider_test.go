package deepgram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

func TestNewTranscriptProvider_DefaultsBaseURL(t *testing.T) {
	p := NewTranscriptProvider(Config{APIKey: "key"})
	require.NotNil(t, p)
	assert.Equal(t, "https://api.deepgram.com/v1", p.config.BaseURL)
	assert.Equal(t, "key", p.config.APIKey)
	assert.NotNil(t, p.client)
}

func TestNewTranscriptProvider_KeepsCustomBaseURL(t *testing.T) {
	p := NewTranscriptProvider(Config{APIKey: "key", BaseURL: "http://example.test"})
	assert.Equal(t, "http://example.test", p.config.BaseURL)
}

func TestAppendTranscript_MissingAPIKey(t *testing.T) {
	p := NewTranscriptProvider(Config{})
	err := p.AppendTranscript(context.Background(), voiceapp.AppendTranscriptCommand{
		CallID:   "call-1",
		Content:  "hello",
		Language: "en",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deepgram api key not configured")
}

func TestAppendTranscript_Success(t *testing.T) {
	p := NewTranscriptProvider(Config{APIKey: "key", BaseURL: "http://example.test"})
	err := p.AppendTranscript(context.Background(), voiceapp.AppendTranscriptCommand{
		CallID:    "call-1",
		Content:   "hello",
		Language:  "en",
		Finalized: true,
	})
	require.NoError(t, err)
}

func TestTranscribeAudio_MissingAPIKey(t *testing.T) {
	p := NewTranscriptProvider(Config{})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "deepgram api key not configured")
}

func TestTranscribeAudio_Success(t *testing.T) {
	var gotAuth, gotContentType, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"channels":[{"alternatives":[{"transcript":"hello world"}]}]}}`))
	}))
	defer srv.Close()

	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio-bytes"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", transcript)
	assert.Equal(t, "Token test-key", gotAuth)
	assert.Equal(t, "audio/wav", gotContentType)
	assert.Equal(t, "/listen", gotPath)
	assert.Equal(t, []byte("audio-bytes"), gotBody)
}

func TestTranscribeAudio_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`unauthorized`))
	}))
	defer srv.Close()

	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "deepgram api error")
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestTranscribeAudio_EmptyChannels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":{"channels":[]}}`))
	}))
	defer srv.Close()

	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "no transcript returned")
}

func TestTranscribeAudio_EmptyAlternatives(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":{"channels":[{"alternatives":[]}]}}`))
	}))
	defer srv.Close()

	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "no transcript returned")
}

func TestTranscribeAudio_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "decode response")
}

func TestTranscribeAudio_SendRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "send request")
}

func TestTranscribeAudio_CreateRequestError(t *testing.T) {
	p := NewTranscriptProvider(Config{APIKey: "test-key", BaseURL: "http://bad host"})
	transcript, err := p.TranscribeAudio(context.Background(), "call-1", []byte("audio"))
	require.Error(t, err)
	assert.Empty(t, transcript)
	assert.Contains(t, err.Error(), "create request")
}
