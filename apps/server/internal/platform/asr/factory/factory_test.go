package factory

import (
	"errors"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/asr"
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
			rec, err := New(config.ASRConfig{Provider: tc.provider})
			if !errors.Is(err, asr.ErrNotConfigured) {
				t.Fatalf("err = %v, want ErrNotConfigured", err)
			}
			if rec != nil {
				t.Fatalf("recognizer must be nil when unconfigured, got %T", rec)
			}
		})
	}
}

func TestNewUnknownProviderRejected(t *testing.T) {
	cases := []string{"deepgram", "MOCK", "volcano"}
	for _, provider := range cases {
		t.Run(provider, func(t *testing.T) {
			rec, err := New(config.ASRConfig{Provider: provider})
			if err == nil {
				t.Fatalf("provider %q must be rejected (no streaming provider wired yet)", provider)
			}
			if rec != nil {
				t.Fatalf("recognizer must be nil on error, got %T", rec)
			}
		})
	}
}
