package factory

import (
	"errors"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/tts"
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
	cases := []string{"openai", "MOCK", "elevenlabs"}
	for _, provider := range cases {
		t.Run(provider, func(t *testing.T) {
			syn, err := New(config.TTSConfig{Provider: provider})
			if err == nil {
				t.Fatalf("provider %q must be rejected (no TTS provider wired yet)", provider)
			}
			if syn != nil {
				t.Fatalf("synthesizer must be nil on error, got %T", syn)
			}
		})
	}
}
