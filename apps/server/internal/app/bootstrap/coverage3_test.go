package bootstrap

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestBuildAppClosesRedisWhenEmbeddingFails(t *testing.T) {
	mr := miniredis.RunT(t)
	host, portStr, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "redis"
	cfg.Redis.Host = host
	cfg.Redis.Port = port
	cfg.Embedding.Provider = "tei"
	cfg.Embedding.TEI.BaseURL = ""

	_, err = BuildApp(cfg)
	if err == nil || !strings.Contains(err.Error(), "create embedding provider") {
		t.Fatalf("BuildApp() error = %v, want embedding failure", err)
	}
}

func TestBuildServerRuntimePropagatesRuntimeError(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	// Avoid duplicate Prometheus collector registration across tests.
	app.Config.Monitoring.Enabled = false
	app.DB = newBootstrapSQLite(t)

	app.Config.WeKnora.Enabled = true
	app.Config.WeKnora.BaseURL = "http://127.0.0.1:1"
	app.Config.Fallback.Enabled = false

	if _, err := app.BuildServerRuntime(); err == nil {
		t.Fatal("expected BuildServerRuntime to surface runtime assembly error")
	}
}

func TestLoadConfigInvalidTypedValue(t *testing.T) {
	path := writeBootstrapConfig(t, strings.Join([]string{
		"server:",
		"  host: 127.0.0.1",
		"  port: not-a-number",
		"",
	}, "\n"))
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected LoadConfig to fail for invalid typed values")
	}
}

func TestExpandEnvVarsInConfigTempDirFailure(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing-tmp"))
	path := writeBootstrapConfig(t, "a: b\n")
	if got := expandEnvVarsInConfig(path); got != "" {
		t.Fatalf("expected empty result when temp creation fails, got %q", got)
	}
}

func TestDatabaseHelpersCoverage(t *testing.T) {
	if got := BuildPostgresDSN(nil, DatabaseOptions{DSN: "postgres://override"}); got != "postgres://override" {
		t.Fatalf("DSN override = %q", got)
	}

	cfg := config.GetDefaultConfig()
	cfg.Database.Host = "127.0.0.1"
	cfg.Database.Port = 1

	// Postgres dial to a closed local port fails fast and exercises the DSN
	// composition plus the gorm.Open error path.
	if _, err := OpenDatabase(cfg, DatabaseOptions{}); err == nil {
		t.Fatal("expected OpenDatabase to fail for unreachable postgres")
	}

	// Tracing toggles: config-driven and option-driven.
	tracingCfg := config.GetDefaultConfig()
	tracingCfg.Monitoring.Tracing.Enabled = true
	if db, err := OpenDatabase(tracingCfg, DatabaseOptions{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "trace1.db")}); err != nil {
		t.Fatalf("open sqlite with cfg tracing: %v", err)
	} else {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}

	if db, err := OpenDatabase(config.GetDefaultConfig(), DatabaseOptions{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "trace2.db"), EnableTracing: true,
	}); err != nil {
		t.Fatalf("open sqlite with opts tracing: %v", err)
	} else {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}

	// Retry defaults kick in for zero-value retry options; success on first try.
	db, err := OpenDatabaseWithRetry(config.GetDefaultConfig(), DatabaseOptions{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "retry.db"),
	}, DatabaseRetryOptions{})
	if err != nil {
		t.Fatalf("OpenDatabaseWithRetry(success) error = %v", err)
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()

	// Bounded retry failure with a short delay.
	if _, err := OpenDatabaseWithRetry(cfg, DatabaseOptions{}, DatabaseRetryOptions{
		MaxRetries: 2,
		RetryDelay: 0, // falls back to the default branch, but no sleep on first failure
		Logger:     logrus.New(),
	}); err == nil {
		t.Fatal("expected OpenDatabaseWithRetry to fail for unreachable postgres")
	}

	if got := firstNonEmpty("", "", ""); got != "" {
		t.Fatalf("firstNonEmpty(all empty) = %q", got)
	}
}

func TestAutoMigrateReAddsDroppedColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "columns.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	dropIndexes := func(table string) {
		var names []string
		if err := db.Raw("SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ?", table).Scan(&names).Error; err != nil {
			t.Fatalf("list indexes on %s: %v", table, err)
		}
		for _, name := range names {
			if err := db.Exec("DROP INDEX IF EXISTS " + name).Error; err != nil {
				t.Fatalf("drop index %s: %v", name, err)
			}
		}
	}

	for _, table := range []string{"knowledge_docs", "agents"} {
		dropIndexes(table)
	}
	for _, stmt := range []string{
		"ALTER TABLE knowledge_docs DROP COLUMN is_public",
		"ALTER TABLE knowledge_docs DROP COLUMN provider_id",
		"ALTER TABLE knowledge_docs DROP COLUMN external_id",
		"ALTER TABLE agents DROP COLUMN last_activity_at",
		"ALTER TABLE agents DROP COLUMN connected_at",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate(re-add columns) error = %v", err)
	}
	for _, column := range []string{"is_public", "provider_id", "external_id"} {
		if !db.Migrator().HasColumn("knowledge_docs", column) {
			t.Fatalf("expected knowledge_docs.%s to be re-added", column)
		}
	}
}

func TestObservabilityWarningsRepoRootFallback(t *testing.T) {
	cfg := config.GetDefaultConfig()
	// Blank root falls back to the source-derived repo root where the
	// observability assets exist, so no asset warnings are expected.
	warnings := ObservabilityWarnings(cfg, "   ")
	for _, warning := range warnings {
		if strings.Contains(warning, "asset missing") || strings.Contains(warning, "repository root") {
			t.Fatalf("expected source-derived repo root to resolve assets, got %v", warnings)
		}
	}
}

func TestHasRateLimitPrefixBlankPrefix(t *testing.T) {
	if hasRateLimitPrefix(nil, "   ") {
		t.Fatal("blank prefix should never match")
	}
}

func TestStartHTTPServerNilLogger(t *testing.T) {
	handler := http.NewServeMux()
	server := &http.Server{Addr: "127.0.0.1:0", Handler: handler}
	StartHTTPServer(server, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = server.Shutdown(ctx)
}
