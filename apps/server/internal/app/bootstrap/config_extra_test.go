package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/glebarez/sqlite"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

func writeBootstrapConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfigMissingExplicitFileFails(t *testing.T) {
	t.Cleanup(viper.Reset)
	missing := filepath.Join(t.TempDir(), "missing.yml")
	if _, err := LoadConfig(missing); err == nil {
		t.Fatal("expected LoadConfig to fail for a missing explicit config file")
	}
}

func TestLoadConfigMalformedFileFails(t *testing.T) {
	t.Cleanup(viper.Reset)
	path := writeBootstrapConfig(t, "{{{{ not yaml")
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected LoadConfig to fail for malformed yaml")
	}
}

func TestLoadConfigExpandsEnvVars(t *testing.T) {
	t.Cleanup(viper.Reset)
	t.Setenv("BOOTSTRAP_TEST_JWT_SECRET", "expanded-secret-value")

	path := writeBootstrapConfig(t, strings.Join([]string{
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"event_bus:",
		"  provider: inmemory",
		"jwt:",
		"  secret: ${BOOTSTRAP_TEST_JWT_SECRET}",
		"",
	}, "\n"))

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.JWT.Secret != "expanded-secret-value" {
		t.Fatalf("jwt secret = %q, want expanded value", cfg.JWT.Secret)
	}
}

func TestLoadConfigDefaultsToSearchPaths(t *testing.T) {
	t.Cleanup(viper.Reset)
	// No config.yml next to the package sources, so the search falls through
	// and the loader tolerates the missing-file result.
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected default config")
	}
}

func TestExpandEnvVarsInConfigBranches(t *testing.T) {
	if got := expandEnvVarsInConfig(filepath.Join(t.TempDir(), "nope.yml")); got != "" {
		t.Fatalf("missing file = %q, want empty", got)
	}

	t.Setenv("BOOTSTRAP_EXPAND_VAR", "hello")
	path := writeBootstrapConfig(t, "key: ${BOOTSTRAP_EXPAND_VAR}\n")
	expanded := expandEnvVarsInConfig(path)
	if expanded == "" {
		t.Fatal("expected expanded temp config path")
	}
	t.Cleanup(func() { _ = os.Remove(expanded) })

	body, err := os.ReadFile(expanded)
	if err != nil {
		t.Fatalf("read expanded: %v", err)
	}
	if !strings.Contains(string(body), "hello") {
		t.Fatalf("expanded content = %q", string(body))
	}
}

func TestApplyConfigEnvOverrides(t *testing.T) {
	cfg := config.GetDefaultConfig()

	t.Setenv("OPENAI_API_KEY", "k1")
	t.Setenv("OPENAI_BASE_URL", "http://openai.example")
	t.Setenv("OPENAI_MODEL", "m1")
	t.Setenv("DIFY_ENABLED", "true")
	t.Setenv("DIFY_BASE_URL", "http://dify.example")
	t.Setenv("DIFY_API_KEY", "k2")
	t.Setenv("DIFY_DATASET_ID", "ds1")
	t.Setenv("WEKNORA_ENABLED", "TRUE")
	t.Setenv("WEKNORA_BASE_URL", "http://weknora.example")
	t.Setenv("WEKNORA_API_KEY", "k3")
	t.Setenv("WEKNORA_TENANT_ID", "tenant-9")
	t.Setenv("WEKNORA_KB_ID", "kb-9")

	applyConfigEnvOverrides(cfg)

	if cfg.AI.OpenAI.APIKey != "k1" || cfg.AI.OpenAI.BaseURL != "http://openai.example" || cfg.AI.OpenAI.Model != "m1" {
		t.Fatalf("openai overrides not applied: %+v", cfg.AI.OpenAI)
	}
	if !cfg.Dify.Enabled || cfg.Dify.BaseURL != "http://dify.example" || cfg.Dify.APIKey != "k2" || cfg.Dify.DatasetID != "ds1" {
		t.Fatalf("dify overrides not applied: %+v", cfg.Dify)
	}
	if !cfg.WeKnora.Enabled || cfg.WeKnora.BaseURL != "http://weknora.example" || cfg.WeKnora.APIKey != "k3" ||
		cfg.WeKnora.TenantID != "tenant-9" || cfg.WeKnora.KnowledgeBaseID != "kb-9" {
		t.Fatalf("weknora overrides not applied: %+v", cfg.WeKnora)
	}

	applyConfigEnvOverrides(nil)

	t.Setenv("DIFY_ENABLED", "false")
	cfg2 := config.GetDefaultConfig()
	cfg2.Dify.Enabled = true
	applyConfigEnvOverrides(cfg2)
	if cfg2.Dify.Enabled {
		t.Fatal("DIFY_ENABLED=false should disable dify")
	}
}

func TestResolveRuntimeOverridesMatrix(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Database.Host = "db-host"
	cfg.Database.Port = 6543
	cfg.Server.Host = "srv-host"
	cfg.Server.Port = 7070

	t.Run("flags override config", func(t *testing.T) {
		overrides, err := ResolveRuntimeOverrides(cfg, []string{
			"-db-driver=postgres", "-db-host=flag-host", "-db-port=6599", "-db-user=flag-user",
			"-db-pass=flag-pass", "-db-name=flag-name", "-db-sslmode=require", "-db-timezone=Asia/Shanghai",
			"-dsn=postgres://explicit/dsn", "-host=0.0.0.0", "-port=9000",
		}, nil)
		if err != nil {
			t.Fatalf("ResolveRuntimeOverrides() error = %v", err)
		}
		db := overrides.Database
		if db.DSN != "postgres://explicit/dsn" || db.Host != "flag-host" || db.Port != "6599" ||
			db.User != "flag-user" || db.Password != "flag-pass" || db.Name != "flag-name" ||
			db.SSLMode != "require" || db.TimeZone != "Asia/Shanghai" {
			t.Fatalf("database overrides = %+v", db)
		}
		if overrides.HTTP.Host != "0.0.0.0" || overrides.HTTP.Port != 9000 {
			t.Fatalf("http overrides = %+v", overrides.HTTP)
		}
	})

	t.Run("postgres dsn composed when flag dsn empty", func(t *testing.T) {
		overrides, err := ResolveRuntimeOverrides(cfg, []string{"-db-driver=postgres"}, nil)
		if err != nil {
			t.Fatalf("ResolveRuntimeOverrides() error = %v", err)
		}
		if !strings.Contains(overrides.Database.DSN, "host=db-host") ||
			!strings.Contains(overrides.Database.DSN, "port=6543") ||
			!strings.Contains(overrides.Database.DSN, "sslmode=disable") {
			t.Fatalf("composed dsn = %q", overrides.Database.DSN)
		}
		if overrides.HTTP.Port != 7070 {
			t.Fatalf("http port fallback = %d", overrides.HTTP.Port)
		}
	})

	t.Run("sqlite driver skips postgres dsn", func(t *testing.T) {
		t.Setenv("DB_DRIVER", "sqlite")
		t.Setenv("DB_DSN", "/tmp/servify-test.db")
		overrides, err := ResolveRuntimeOverrides(nil, nil, nil)
		if err != nil {
			t.Fatalf("ResolveRuntimeOverrides() error = %v", err)
		}
		if overrides.Database.Driver != "sqlite" || overrides.Database.DSN != "/tmp/servify-test.db" {
			t.Fatalf("sqlite defaults = %+v", overrides.Database)
		}
	})

	t.Run("env port fallback", func(t *testing.T) {
		t.Setenv("SERVIFY_PORT", "7777")
		overrides, err := ResolveRuntimeOverrides(cfg, nil, nil)
		if err != nil {
			t.Fatalf("ResolveRuntimeOverrides() error = %v", err)
		}
		if overrides.HTTP.Port != 7777 {
			t.Fatalf("env port = %d", overrides.HTTP.Port)
		}
	})

	t.Run("invalid flag fails", func(t *testing.T) {
		if _, err := ResolveRuntimeOverrides(cfg, []string{"-port=not-a-number"}, nil); err == nil {
			t.Fatal("expected flag parse error")
		}
	})
}

func TestMigrationModelsCatalog(t *testing.T) {
	models := MigrationModels()
	if len(models) < 30 {
		t.Fatalf("expected canonical migration catalog, got %d models", len(models))
	}
	for _, m := range models {
		if m == nil {
			t.Fatal("migration catalog contains nil models")
		}
	}
}

func TestAutoMigrateAndCreateIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migrate.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	if !db.Migrator().HasTable("users") {
		t.Fatal("expected users table after migration")
	}
	if err := CreateIndexes(db); err != nil {
		t.Fatalf("CreateIndexes() error = %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("raw db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if err := CreateIndexes(db); err == nil {
		t.Fatal("expected CreateIndexes to fail on a closed database")
	}
	if err := AutoMigrate(db); err == nil {
		t.Fatal("expected AutoMigrate to fail on a closed database")
	}
}

func TestAutoMigrateFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "conflict.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("CREATE VIEW users AS SELECT 1 AS id").Error; err != nil {
		t.Fatalf("seed view: %v", err)
	}
	if err := AutoMigrate(db); err == nil {
		t.Fatal("expected AutoMigrate to fail when the users table cannot be created")
	}
}

func TestAutoMigrateEnabledValues(t *testing.T) {
	cases := map[string]bool{
		"":      true,
		"1":     true,
		"true":  true,
		"YES":   true,
		" On ":  true,
		"0":     false,
		"false": false,
		"No":    false,
		"off":   false,
		"bogus": true,
	}
	for value, want := range cases {
		t.Setenv("SERVIFY_AUTO_MIGRATE", value)
		if got := AutoMigrateEnabled(); got != want {
			t.Fatalf("AutoMigrateEnabled(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestObservabilityWarningsMatrix(t *testing.T) {
	if warnings := ObservabilityWarnings(nil, ""); len(warnings) != 1 || !strings.Contains(warnings[0], "config is nil") {
		t.Fatalf("nil config warnings = %v", warnings)
	}

	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Enabled = false
	warnings := ObservabilityWarnings(cfg, t.TempDir())
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "monitoring is disabled") {
		t.Fatalf("expected disabled monitoring warning, got %v", warnings)
	}

	cfg = config.GetDefaultConfig()
	cfg.Monitoring.MetricsPath = "metrics-relative"
	warnings = ObservabilityWarnings(cfg, t.TempDir())
	if !strings.Contains(strings.Join(warnings, "\n"), "must start with /") {
		t.Fatalf("expected metrics path warning, got %v", warnings)
	}

	cfg = config.GetDefaultConfig()
	cfg.Monitoring.MetricsPath = ""
	warnings = ObservabilityWarnings(cfg, t.TempDir())
	if !strings.Contains(strings.Join(warnings, "\n"), "metrics_path is empty") {
		t.Fatalf("expected empty metrics path warning, got %v", warnings)
	}

	cfg = config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.Endpoint = " "
	cfg.Monitoring.Tracing.SampleRatio = 2
	cfg.Monitoring.Tracing.ServiceName = ""
	warnings = ObservabilityWarnings(cfg, t.TempDir())
	joined = strings.Join(warnings, "\n")
	for _, want := range []string{"endpoint is empty", "sample_ratio", "service_name is empty"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in tracing warnings, got %v", want, warnings)
		}
	}

	cfg = config.GetDefaultConfig()
	warnings = ObservabilityWarnings(cfg, filepath.Join(t.TempDir(), "missing-root"))
	if len(warnings) == 0 {
		t.Fatal("expected asset warnings for missing repo root")
	}
}

func TestRepoRootFromSource(t *testing.T) {
	if root := repoRootFromSource(); root == "" {
		t.Fatal("expected repo root to resolve from source location")
	}
}

func TestSetupObservabilityBranches(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}

	if err := SetupObservability(context.Background(), config.GetDefaultConfig(), app); err != nil {
		t.Fatalf("SetupObservability(disabled) error = %v", err)
	}
	if len(app.ShutdownHooks) != 1 {
		t.Fatalf("expected tracing shutdown hook, got %d hooks", len(app.ShutdownHooks))
	}
	if err := app.RunShutdownHooks(); err != nil {
		t.Fatalf("RunShutdownHooks() error = %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.ServiceName = "bootstrap-test"
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "not-a-valid-entry")
	if err := SetupObservability(context.Background(), cfg, app); err == nil {
		t.Fatal("expected SetupObservability to fail for invalid resource attributes")
	}

	if err := SetupObservability(context.Background(), config.GetDefaultConfig(), nil); err != nil {
		t.Fatalf("SetupObservability(disabled, nil app) error = %v", err)
	}
}

func TestSecurityWarningsNilAndRateLimitBranches(t *testing.T) {
	if warnings := SecurityWarnings(nil); len(warnings) != 1 {
		t.Fatalf("nil config warnings = %v", warnings)
	}

	cfg := config.GetDefaultConfig()
	cfg.Security.RateLimiting.Enabled = true
	cfg.Security.RateLimiting.Paths = []config.PathRateLimitConfig{
		{Prefix: "  ", Enabled: true},        // blank prefix ignored
		{Prefix: "/public/", Enabled: false}, // disabled path ignored
	}
	warnings := SecurityWarnings(cfg)
	if !strings.Contains(strings.Join(warnings, "\n"), "/public/") {
		t.Fatalf("expected missing rate limit warning for /public/, got %v", warnings)
	}

	cfg.Security.RateLimiting.Paths = append(cfg.Security.RateLimiting.Paths,
		config.PathRateLimitConfig{Prefix: " /public/ ", Enabled: true},
	)
	if warnings := SecurityWarnings(cfg); strings.Contains(strings.Join(warnings, "\n"), "for /public/ (") {
		t.Fatalf("trimmed enabled prefix should satisfy /public/ requirement: %v", warnings)
	}
}
