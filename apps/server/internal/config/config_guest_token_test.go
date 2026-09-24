package config

import (
	"strings"
	"testing"
)

// TestGuestTokenInsecureDefaults 锚定访客 token（§10 #2 / D6）gate 语义：
// required + 默认 jwt.secret 组合才告警（同信任域自签自验，默认 secret
// 下访客 token 可伪造）；required 关闭或 secret 已替换均保持安静。
func TestGuestTokenInsecureDefaults(t *testing.T) {
	base := func() *Config {
		cfg := GetDefaultConfig()
		cfg.JWT.Secret = "a-very-long-production-secret-value"
		return cfg
	}

	t.Run("required with default jwt secret warns", func(t *testing.T) {
		cfg := base()
		cfg.JWT.Secret = "dev-secret-key-change-in-production" // InsecureJWTSecrets 白名单内的默认值
		cfg.Security.GuestToken.Required = true
		found := false
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "security.guest_token.required is true but jwt.secret") {
				found = true
			}
		}
		if !found {
			t.Fatal("expected forgeable-guest-token warning")
		}
	})

	t.Run("required with custom secret is quiet", func(t *testing.T) {
		cfg := base()
		cfg.Security.GuestToken.Required = true
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "guest_token") {
				t.Fatalf("unexpected guest_token warning: %q", w)
			}
		}
	})

	t.Run("disabled is quiet", func(t *testing.T) {
		cfg := base()
		cfg.JWT.Secret = "dev-secret-key-change-in-production"
		for _, w := range InsecureDefaults(cfg) {
			if strings.Contains(w, "guest_token") {
				t.Fatalf("unexpected guest_token warning: %q", w)
			}
		}
	})

	t.Run("production zero tolerance", func(t *testing.T) {
		cfg := base()
		cfg.Server.Environment = "production"
		cfg.JWT.Secret = "dev-secret-key-change-in-production"
		cfg.Security.GuestToken.Required = true
		cfg.Database.Password = "prod-db-password"
		cfg.EventBus.Provider = "redis"
		if res := Validate(cfg); res.Valid {
			t.Fatalf("expected production startup rejection, warnings = %v", res.Warnings)
		}
	})
}

func TestGetDefaultConfigGuestToken(t *testing.T) {
	got := GetDefaultConfig().Security.GuestToken
	if got.Required {
		t.Fatal("guest_token must default to disabled")
	}
	if got.TTL != 24*60*60*1e9 { // 24h
		t.Fatalf("guest_token ttl = %v, want 24h", got.TTL)
	}
}
