package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestHealthChecksConfig_KnowledgeProviderEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  HealthChecksConfig
		want bool
	}{
		{"both disabled", HealthChecksConfig{}, false},
		{"knowledge_provider only", HealthChecksConfig{KnowledgeProvider: true}, true},
		{"weknora only", HealthChecksConfig{WeKnora: true}, true},
		{"both enabled", HealthChecksConfig{KnowledgeProvider: true, WeKnora: true}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.KnowledgeProviderEnabled(); got != tc.want {
			t.Fatalf("%s: KnowledgeProviderEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLoadWithResult_Defaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, result, err := LoadWithResult()
	if err != nil {
		t.Fatalf("LoadWithResult() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadWithResult() returned nil config")
	}
	if !result.Valid {
		t.Fatalf("LoadWithResult() result = %+v, want valid", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected insecure-default warnings for default config")
	}
}

func TestLoadWithResult_ProductionInvalid(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("server.environment", "production")

	cfg, result, err := LoadWithResult()
	if err == nil {
		t.Fatal("LoadWithResult() expected error for insecure production config")
	}
	if cfg == nil {
		t.Fatal("LoadWithResult() should still return the config on validation failure")
	}
	if result.Valid {
		t.Fatal("expected result.Valid = false for production with insecure defaults")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for production with insecure defaults")
	}
}

func TestValidate_DevelopmentAllowsWarnings(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Server.Environment = "development"

	result := Validate(cfg)
	if !result.Valid {
		t.Fatalf("Validate() in development should stay valid, got %+v", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for insecure defaults in development")
	}
}

func TestValidate_SecureConfigIsValid(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Server.Environment = "production"
	cfg.JWT.Secret = "a-very-long-production-secret-value"
	cfg.WeKnora.APIKey = "wk-prod-key"
	cfg.Database.Password = "prod-db-password"

	result := Validate(cfg)
	if !result.Valid {
		t.Fatalf("Validate() expected valid, got warnings %v", result.Warnings)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}
}
