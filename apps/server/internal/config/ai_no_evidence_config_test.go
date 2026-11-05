package config

import (
	"strings"
	"testing"
)

// TestInsecureDefaults_AINoEvidencePolicy covers action whitelist, threshold
// bounds, and defaults.
func TestInsecureDefaults_AINoEvidencePolicy(t *testing.T) {
	cases := []struct {
		name       string
		cfg        func(cfg *Config)
		wantSubstr string
	}{
		{
			name: "default accepted",
			cfg:  func(cfg *Config) {},
		},
		{
			name: "clarify accepted",
			cfg:  func(cfg *Config) { cfg.AI.NoEvidence.ZeroHitAction = "clarify" },
		},
		{
			name: "refuse accepted",
			cfg:  func(cfg *Config) { cfg.AI.NoEvidence.ZeroHitAction = "refuse" },
		},
		{
			name: "handoff accepted",
			cfg:  func(cfg *Config) { cfg.AI.NoEvidence.ZeroHitAction = "handoff" },
		},
		{
			name:       "invalid zero-hit blocked",
			cfg:        func(cfg *Config) { cfg.AI.NoEvidence.ZeroHitAction = "bogus" },
			wantSubstr: "ai.no_evidence.zero_hit_action must be 'direct', 'clarify', 'refuse' or 'handoff'",
		},
		{
			name:       "invalid low-confidence blocked",
			cfg:        func(cfg *Config) { cfg.AI.NoEvidence.LowConfidenceAction = "bogus" },
			wantSubstr: "ai.no_evidence.low_confidence_action must be 'direct', 'clarify', 'refuse' or 'handoff'",
		},
		{
			name:       "negative threshold blocked",
			cfg:        func(cfg *Config) { cfg.AI.NoEvidence.ConfidenceThreshold = -0.1 },
			wantSubstr: "ai.no_evidence.confidence_threshold must be in [0,1]",
		},
		{
			name:       "over-one threshold blocked",
			cfg:        func(cfg *Config) { cfg.AI.NoEvidence.ConfidenceThreshold = 1.5 },
			wantSubstr: "ai.no_evidence.confidence_threshold must be in [0,1]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := GetDefaultConfig()
			cfg.Server.Environment = "production"
			tc.cfg(cfg)
			warnings := InsecureDefaults(cfg)
			if tc.wantSubstr == "" {
				for _, w := range warnings {
					if strings.Contains(w, "no_evidence") {
						t.Fatalf("unexpected no_evidence warning: %v", warnings)
					}
				}
				return
			}
			found := false
			for _, w := range warnings {
				if strings.Contains(w, tc.wantSubstr) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected warning containing %q, got %v", tc.wantSubstr, warnings)
			}
		})
	}
}

// TestDefaultConfigNoEvidenceFamily covers default values.
func TestDefaultConfigNoEvidenceFamily(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.AI.NoEvidence.ZeroHitAction != "direct" {
		t.Fatalf("zero_hit_action = %q, want direct", cfg.AI.NoEvidence.ZeroHitAction)
	}
	if cfg.AI.NoEvidence.LowConfidenceAction != "direct" {
		t.Fatalf("low_confidence_action = %q, want direct", cfg.AI.NoEvidence.LowConfidenceAction)
	}
	if cfg.AI.NoEvidence.ConfidenceThreshold != 0 {
		t.Fatalf("confidence_threshold = %v, want 0 (disabled)", cfg.AI.NoEvidence.ConfidenceThreshold)
	}
}
