package config

// webrtc_turn_test.go 覆盖 TURN 时间限凭据配置的零容忍门禁与默认值
// （docs/TURN_DEPLOYMENT.md 第 5 节配置面口径）。

import (
	"strings"
	"testing"
	"time"
)

func webrtcTurnWarnings(t *testing.T, mutate func(*Config)) []string {
	t.Helper()
	cfg := GetDefaultConfig()
	mutate(cfg)
	return InsecureDefaults(cfg)
}

func TestInsecureDefaultsWebRTCTurnDisabledQuiet(t *testing.T) {
	// 默认（URL 为空）不产生任何 webrtc.turn 告警。
	for _, w := range webrtcTurnWarnings(t, func(*Config) {}) {
		if strings.Contains(w, "webrtc.turn") {
			t.Fatalf("disabled TURN must not warn, got %q", w)
		}
	}
}

func TestInsecureDefaultsWebRTCTurnEnabledRequiresComplete(t *testing.T) {
	enable := func(cfg *Config) {
		cfg.WebRTC.TURN.URL = "turn:turn.example.com:3478"
		cfg.WebRTC.TURN.Realm = "servify.example.com"
		cfg.WebRTC.TURN.StaticAuthSecret = "shared-secret"
		cfg.WebRTC.TURN.TTL = 5 * time.Minute
	}

	// 完整配置无 webrtc.turn 告警（默认配置自带的 jwt/db/eventbus 告警除外）。
	for _, w := range webrtcTurnWarnings(t, enable) {
		if strings.Contains(w, "webrtc.turn") {
			t.Fatalf("complete TURN config must not warn, got %q", w)
		}
	}

	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			"missing realm",
			func(c *Config) { c.WebRTC.TURN.Realm = "" },
			"webrtc.turn.url is set but webrtc.turn.realm is empty",
		},
		{
			"missing secret",
			func(c *Config) { c.WebRTC.TURN.StaticAuthSecret = "" },
			"webrtc.turn.url is set but webrtc.turn.static_auth_secret is empty",
		},
		{
			"non-positive ttl",
			func(c *Config) { c.WebRTC.TURN.TTL = 0 },
			"webrtc.turn.url is set but webrtc.turn.ttl is not positive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, w := range webrtcTurnWarnings(t, func(c *Config) {
				enable(c)
				tc.mutate(c)
			}) {
				if strings.Contains(w, "webrtc.turn") {
					got = append(got, w)
				}
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("warnings = %v, want exactly [%s]", got, tc.want)
			}
		})
	}
}

func TestGetDefaultConfigWebRTCTurnTTL(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.WebRTC.TURN.URL != "" {
		t.Fatalf("TURN must default to disabled, got url %q", cfg.WebRTC.TURN.URL)
	}
	if cfg.WebRTC.TURN.TTL != 5*time.Minute {
		t.Fatalf("TURN TTL default = %v, want 5m", cfg.WebRTC.TURN.TTL)
	}
}
