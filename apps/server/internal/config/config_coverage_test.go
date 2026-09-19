package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	cfg.EventBus.Provider = "redis" // P3-3：production 不允许默认 in-memory 事件总线

	result := Validate(cfg)
	if !result.Valid {
		t.Fatalf("Validate() expected valid, got warnings %v", result.Warnings)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}
}

func TestInsecureDefaultsNilConfig(t *testing.T) {
	warnings := InsecureDefaults(nil)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "config is nil") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestLoadInvalidForProductionInsecureDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("server.environment", "production")

	if _, err := Load(); err == nil {
		t.Fatal("expected production insecure defaults to fail Load()")
	}
	if _, _, err := LoadWithResult(); err == nil {
		t.Fatal("expected production insecure defaults to fail LoadWithResult()")
	}
}

func TestExpandEnvPlaceholdersEdgeValues(t *testing.T) {
	var nilPtr *Config
	expandEnvPlaceholders(reflect.ValueOf(nilPtr))

	var nilIface interface{}
	expandEnvPlaceholders(reflect.ValueOf(nilIface))

	iface := interface{}("no-set") // interface holding a string stored in a non-settable value
	expandEnvPlaceholders(reflect.ValueOf(iface))

	slice := []string{"plain"}
	expandEnvPlaceholders(reflect.ValueOf(slice))
	if slice[0] != "plain" {
		t.Fatalf("slice = %v", slice)
	}

	m := map[string]interface{}{"k": "v"}
	expandEnvPlaceholders(reflect.ValueOf(m))
	if m["k"] != "v" {
		t.Fatalf("map = %v", m)
	}
}

func TestInitLoggerVariants(t *testing.T) {
	cfg := GetDefaultConfig()

	cfg.Log = LogConfig{Level: "bogus", Format: "text", Output: "unknown"}
	if err := InitLogger(cfg); err != nil {
		t.Fatalf("InitLogger(unknown output) error = %v", err)
	}

	cfg.Log = LogConfig{Level: "debug", Format: "json", Output: "file", FilePath: filepath.Join(t.TempDir(), "logs", "app.log")}
	if err := InitLogger(cfg); err != nil {
		t.Fatalf("InitLogger(file) error = %v", err)
	}

	cfg.Log = LogConfig{Level: "warn", Format: "text", Output: "both", FilePath: filepath.Join(t.TempDir(), "both", "app.log")}
	if err := InitLogger(cfg); err != nil {
		t.Fatalf("InitLogger(both) error = %v", err)
	}

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg.Log = LogConfig{Level: "info", Format: "json", Output: "file", FilePath: filepath.Join(blocker, "sub", "app.log")}
	if err := InitLogger(cfg); err == nil {
		t.Fatal("expected mkdir failure under a file path")
	}
	cfg.Log.Output = "both"
	cfg.Log.FilePath = filepath.Join(blocker, "sub", "app.log")
	if err := InitLogger(cfg); err == nil {
		t.Fatal("expected mkdir failure under a file path (both)")
	}

	cfg.Log = LogConfig{Level: "info", Format: "json", Output: "stdout"}
	if err := InitLogger(cfg); err != nil {
		t.Fatalf("InitLogger(stdout) error = %v", err)
	}
}
