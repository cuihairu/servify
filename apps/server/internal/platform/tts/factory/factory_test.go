package factory

import (
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/tts"
	ttsopenai "servify/apps/server/internal/platform/tts/openai"
)

func TestNewEmptyProviderIsNotConfigured(t *testing.T) {
	cases := []struct {
		name     string
		provider string
	}{
		{name: "empty", provider: ""},
		{name: "spaces only", provider: "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			syn, err := New(config.TTSConfig{Provider: tc.provider})
			if !errors.Is(err, tts.ErrNotConfigured) {
				t.Fatalf("err = %v, want ErrNotConfigured", err)
			}
			if syn != nil {
				t.Fatalf("synthesizer must be nil when unconfigured, got %T", syn)
			}
		})
	}
}

func TestNewUnknownProviderRejected(t *testing.T) {
	cases := []string{"MOCK", "elevenlabs", "azure"}
	for _, provider := range cases {
		t.Run(provider, func(t *testing.T) {
			syn, err := New(config.TTSConfig{Provider: provider})
			if err == nil {
				t.Fatalf("provider %q must be rejected (not wired)", provider)
			}
			if syn != nil {
				t.Fatalf("synthesizer must be nil on error, got %T", syn)
			}
		})
	}
}

func TestNewOpenAIProvider(t *testing.T) {
	cases := []string{"openai", "OpenAI", " OPENAI "}
	for _, provider := range cases {
		t.Run(provider, func(t *testing.T) {
			syn, err := New(config.TTSConfig{
				Provider: provider,
				APIKey:   "k",
				BaseURL:  "https://gw.example/v1",
				Model:    "tts-1",
				Voice:    "alloy",
				Format:   "mp3",
				Timeout:  time.Second,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, ok := syn.(*ttsopenai.Provider); !ok {
				t.Fatalf("provider type = %T, want *ttsopenai.Provider", syn)
			}
		})
	}
}
