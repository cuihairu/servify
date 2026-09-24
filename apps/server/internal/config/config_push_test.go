package config

import (
	"strings"
	"testing"
)

// TestPushFCMCredentialsValid 覆盖服务账号 JSON 的五分支。
func TestPushFCMCredentialsValid(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{"empty", "  ", "credentials_json is empty"},
		{"not json", "{oops", "not valid JSON"},
		{"missing client_email", `{"private_key":"k"}`, "missing client_email"},
		{"missing private_key", `{"client_email":"a@b"}`, "missing private_key"},
		{"valid", `{"client_email":"a@b","private_key":"k"}`, ""},
	}
	for _, tc := range cases {
		if got := pushFCMCredentialsValid(tc.raw); !strings.Contains(got, tc.wantSub) {
			t.Errorf("%s: got %q, want substring %q", tc.name, got, tc.wantSub)
		}
	}
}

func TestInsecureDefaultsPushWarnings(t *testing.T) {
	base := func() *Config {
		cfg := GetDefaultConfig()
		cfg.Server.Environment = "production"
		cfg.JWT.Secret = "a-very-long-production-secret-value"
		cfg.Database.Password = "prod-db-password"
		cfg.EventBus.Provider = "redis"
		cfg.Email.Enabled = false
		return cfg
	}

	t.Run("disabled is quiet", func(t *testing.T) {
		cfg := base()
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "push.") {
				t.Fatalf("unexpected push warning: %q", w)
			}
		}
	})

	t.Run("enabled without credentials warns", func(t *testing.T) {
		cfg := base()
		cfg.Push.Enabled = true
		found := false
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "push.fcm/push.apns credentials are both missing") {
				found = true
			}
		}
		if !found {
			t.Fatal("expected missing-credentials warning")
		}
	})

	t.Run("partial apns warns", func(t *testing.T) {
		cfg := base()
		cfg.Push.Enabled = true
		cfg.Push.APNs.PrivateKey = "p8-content"
		// fcm 缺、apns 有私钥但缺 key_id/team_id/bundle_id
		found := false
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "push.apns is partially configured") {
				found = true
			}
		}
		if !found {
			t.Fatal("expected partial-apns warning")
		}
	})
}

func TestGetDefaultConfigPushDisabled(t *testing.T) {
	if GetDefaultConfig().Push.Enabled {
		t.Fatal("push must default to disabled")
	}
}
