package config

import (
	"strings"
	"testing"
)

// TestInsecureDefaults_AIHandoffThreshold 置信门开着就必须给 (0,1] 的阈值：
// 0/负值会让所有首答建议转人工，>1 永不触发——production/staging 在加载期
// 拦下；关闭时不校验（零值合法）。
func TestInsecureDefaults_AIHandoffThreshold(t *testing.T) {
	cases := []struct {
		name       string
		enabled    bool
		threshold  float64
		wantSubstr string
	}{
		{name: "zero threshold blocked", enabled: true, threshold: 0, wantSubstr: "ai.handoff.confidence_threshold must be in (0,1]"},
		{name: "over-one threshold blocked", enabled: true, threshold: 1.5, wantSubstr: "ai.handoff.confidence_threshold must be in (0,1]"},
		{name: "disabled skips check", enabled: false, threshold: 0},
		{name: "valid threshold accepted", enabled: true, threshold: 0.65},
		{name: "boundary one accepted", enabled: true, threshold: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := GetDefaultConfig()
			cfg.Server.Environment = "production"
			cfg.AI.Handoff.Enabled = tc.enabled
			cfg.AI.Handoff.ConfidenceThreshold = tc.threshold
			warnings := InsecureDefaults(cfg)
			if tc.wantSubstr == "" {
				for _, w := range warnings {
					if strings.Contains(w, "handoff") {
						t.Fatalf("unexpected handoff warning: %v", warnings)
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

// TestDefaultConfigHandoffFamily 默认配置开箱即"答不上来才建议转人工"：
// 开启 + 阈值 0.65（零命中 confidence=0.6 < 0.65 ≤ 有命中 ≥0.7）。
func TestDefaultConfigHandoffFamily(t *testing.T) {
	cfg := GetDefaultConfig()
	if !cfg.AI.Handoff.Enabled {
		t.Fatal("handoff gate must default to enabled")
	}
	if cfg.AI.Handoff.ConfidenceThreshold != 0.65 {
		t.Fatalf("default threshold = %v, want 0.65", cfg.AI.Handoff.ConfidenceThreshold)
	}
}
