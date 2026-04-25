package bootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
)

func TestLoadConfigFindsRepoRootConfigFromNestedDir(t *testing.T) {
	root := t.TempDir()
	serverDir := filepath.Join(root, "apps", "server")
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	configPath := filepath.Join(root, "config.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 19090\nweknora:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(cwd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(serverDir); err != nil {
		t.Fatalf("chdir nested dir: %v", err)
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Server.Port != 19090 {
		t.Fatalf("server port = %d, want 19090", cfg.Server.Port)
	}
	if !cfg.WeKnora.Enabled {
		t.Fatal("expected weknora.enabled to be loaded from repo-root config")
	}
}

func TestResolveRuntimeOverridesUsesFlagsAndEnv(t *testing.T) {
	t.Setenv("DB_DRIVER", "")
	t.Setenv("DB_DSN", "")
	t.Setenv("DB_HOST", "env-db")
	t.Setenv("SERVIFY_PORT", "19091")

	cfg := config.GetDefaultConfig()
	cfg.Database.Host = "cfg-db"
	cfg.Database.Port = 5432
	cfg.Database.User = "cfg-user"
	cfg.Database.Password = "cfg-pass"
	cfg.Database.Name = "cfg-name"
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080

	overrides, err := ResolveRuntimeOverrides(cfg, []string{
		"--db-user", "flag-user",
		"--db-name", "flag-name",
		"--host", "127.0.0.1",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ResolveRuntimeOverrides() error = %v", err)
	}

	for _, want := range []string{
		"host=env-db",
		"user=flag-user",
		"password=cfg-pass",
		"dbname=flag-name",
		"port=5432",
		"sslmode=disable",
		"TimeZone=UTC",
	} {
		if !strings.Contains(overrides.Database.DSN, want) {
			t.Fatalf("dsn missing %q: %s", want, overrides.Database.DSN)
		}
	}
	if overrides.HTTP.Host != "127.0.0.1" {
		t.Fatalf("http host = %q", overrides.HTTP.Host)
	}
	if overrides.HTTP.Port != 19091 {
		t.Fatalf("http port = %d", overrides.HTTP.Port)
	}
}

func TestResolveRuntimeOverridesRejectsInvalidFlag(t *testing.T) {
	_, err := ResolveRuntimeOverrides(config.GetDefaultConfig(), []string{"--not-a-real-flag"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid flag error")
	}
}

func TestResolveRuntimeOverridesSupportsSQLiteDriver(t *testing.T) {
	t.Setenv("DB_DRIVER", "")
	t.Setenv("DB_DSN", "")
	cfg := config.GetDefaultConfig()

	overrides, err := ResolveRuntimeOverrides(cfg, []string{
		"--db-driver", "sqlite",
		"--dsn", "file:test-release-check.sqlite?cache=shared",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ResolveRuntimeOverrides() error = %v", err)
	}

	if overrides.Database.Driver != "sqlite" {
		t.Fatalf("database driver = %q, want sqlite", overrides.Database.Driver)
	}
	if overrides.Database.DSN != "file:test-release-check.sqlite?cache=shared" {
		t.Fatalf("database dsn = %q", overrides.Database.DSN)
	}
}

func TestLoadConfigAppliesOpenAIBaseURLEnvOverride(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yml")
	if err := os.WriteFile(configPath, []byte("ai:\n  openai:\n    base_url: https://api.openai.com/v1\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:15002/v1")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.AI.OpenAI.BaseURL != "http://127.0.0.1:15002/v1" {
		t.Fatalf("openai base url = %q, want env override", cfg.AI.OpenAI.BaseURL)
	}
}
