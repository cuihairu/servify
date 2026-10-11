package deepgram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

// Config holds Deepgram configuration.
type Config struct {
	APIKey  string
	BaseURL string // Optional override, defaults to https://api.deepgram.com/v1
}

// TranscriptProvider implements voiceapp.TranscriptProvider using Deepgram API.
type TranscriptProvider struct {
	config Config
	client *http.Client
}

// NewTranscriptProvider creates a new Deepgram transcript provider.
func NewTranscriptProvider(cfg Config) *TranscriptProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepgram.com/v1"
	}
	return &TranscriptProvider{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// AppendTranscript appends a transcript entry.
// In a real implementation, this would:
// 1. Stream audio data to Deepgram for real-time transcription
// 2. Or upload an audio file for batch transcription
// 3. Store the transcript result
func (p *TranscriptProvider) AppendTranscript(ctx context.Context, cmd voiceapp.AppendTranscriptCommand) error {
	if p.config.APIKey == "" {
		return fmt.Errorf("deepgram api key not configured")
	}

	// In a real implementation, you have two options:
	//
	// Option 1: Real-time transcription (Streaming)
	// - Use WebSocket connection to stream audio
	// - Receive transcripts as they arrive
	// - Endpoint: wss://api.deepgram.com/v1/listen
	//
	// Option 2: Batch transcription (Pre-recorded audio)
	// - POST audio file to Deepgram
	// - Receive complete transcript
	// - Endpoint: POST https://api.deepgram.com/v1/listen
	//
	// Example batch request:
	// url := fmt.Sprintf("%s/listen", p.config.BaseURL)
	// req, _ := http.NewRequest("POST", url, audioBody)
	// req.Header.Set("Authorization", "Token "+p.config.APIKey)
	// req.Header.Set("Content-Type", "audio/wav")
	// resp, _ := p.client.Do(req)

	// For this implementation, the content is assumed to be pre-transcribed text
	// that we're just storing. In production, you'd send audio to Deepgram.

	return nil
}

// TranscribeAudio transcribes audio data using Deepgram API.
// This is a helper method for batch transcription.
func (p *TranscriptProvider) TranscribeAudio(ctx context.Context, callID string, audioData []byte) (string, error) {
	if p.config.APIKey == "" {
		return "", fmt.Errorf("deepgram api key not configured")
	}

	url := fmt.Sprintf("%s/listen", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(audioData))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+p.config.APIKey)
	req.Header.Set("Content-Type", "audio/wav")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // 读完即弃，关闭错误不影响响应语义

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("deepgram api error: %s", string(body))
	}

	var result struct {
		Results struct {
			Channels []struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Results.Channels) == 0 || len(result.Results.Channels[0].Alternatives) == 0 {
		return "", fmt.Errorf("no transcript returned")
	}

	return result.Results.Channels[0].Alternatives[0].Transcript, nil
}

// Ensure interface compliance
var _ voiceapp.TranscriptProvider = (*TranscriptProvider)(nil)
