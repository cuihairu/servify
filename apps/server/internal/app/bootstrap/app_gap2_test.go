package bootstrap

import (
	"database/sql/driver"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/alicebob/miniredis/v2"
	migrate "github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// ---- BuildApp：InitLogging 的 nil 兜底 ----

// TestBuildAppFallsBackToStandardLogger 覆盖 initLogging 返回 nil logger
// 时的 logrus.StandardLogger 兜底分支（真实实现恒返回非 nil）。
func TestBuildAppFallsBackToStandardLogger(t *testing.T) {
	previous := initLogging
	initLogging = func(*config.Config) (*logrus.Logger, error) { return nil, nil }
	t.Cleanup(func() { initLogging = previous })

	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	if app.Logger != logrus.StandardLogger() {
		t.Fatalf("BuildApp() logger = %v, want logrus.StandardLogger()", app.Logger)
	}
}

// ---- BuildApp：EventBus 失败时关闭已建立的 redis 连接 ----

// TestBuildAppClosesRedisWhenEventBusFails 覆盖 BuildEventBus 失败后的
// redisClient.Close 分支：redis client 已建立（miniredis）、事件总线装配
// 失败只能经 seam 注入。
func TestBuildAppClosesRedisWhenEventBusFails(t *testing.T) {
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

	busErr := errors.New("injected event bus failure")
	previous := buildEventBus
	buildEventBus = func(*config.Config, *logrus.Logger, *redis.Client) (eventbus.Bus, error) {
		return nil, busErr
	}
	t.Cleanup(func() { buildEventBus = previous })

	_, err = BuildApp(cfg)
	if !errors.Is(err, busErr) {
		t.Fatalf("BuildApp() error = %v, want injected event bus failure", err)
	}
}

// ---- expandEnvVarsInConfig：临时文件写失败 ----

// TestExpandEnvVarsInConfigWriteFailure 覆盖 WriteString 失败后的
// os.Remove + 空路径返回分支（真实临时文件创建后立即写入，恒成功；
// 注入已关闭的文件使写入必然失败）。
func TestExpandEnvVarsInConfigWriteFailure(t *testing.T) {
	src := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(src, []byte("port: 8080\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}

	previous := createExpandedConfigFile
	createExpandedConfigFile = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		name := f.Name()
		if err := f.Close(); err != nil {
			t.Fatalf("close stub temp file: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(name) })
		return f, nil
	}
	t.Cleanup(func() { createExpandedConfigFile = previous })

	if got := expandEnvVarsInConfig(src); got != "" {
		t.Fatalf("expandEnvVarsInConfig() = %q, want empty string on write failure", got)
	}
}

// ---- RunMigrations：嵌入源加载与 migrator 构造错误 ----

// TestRunMigrationsSourceLoadError 覆盖 iofs.New 失败分支：嵌入式 FS
// 编译期保证有效，生产不可达，经 seam 注入。
func TestRunMigrationsSourceLoadError(t *testing.T) {
	sourceErr := errors.New("injected source failure")
	previous := newMigrationsSource
	newMigrationsSource = func(fs.FS, string) (source.Driver, error) { return nil, sourceErr }
	t.Cleanup(func() { newMigrationsSource = previous })

	err := RunMigrations(newStubMigrationsDB(t, []driver.Value{int64(8), false}))
	if !errors.Is(err, sourceErr) {
		t.Fatalf("RunMigrations() error = %v, want injected source failure", err)
	}
	if !strings.Contains(err.Error(), "load embedded source") {
		t.Fatalf("RunMigrations() error = %v, want wrapped load-embedded-source error", err)
	}
}

// TestRunMigrationsBuildMigratorError 覆盖 migrate.NewWithInstance 失败
// 分支（构造参数恒非空非默认，生产不可达，经 seam 注入）。
func TestRunMigrationsBuildMigratorError(t *testing.T) {
	migratorErr := errors.New("injected migrator failure")
	previous := newSchemaMigrator
	newSchemaMigrator = func(string, source.Driver, string, database.Driver) (*migrate.Migrate, error) {
		return nil, migratorErr
	}
	t.Cleanup(func() { newSchemaMigrator = previous })

	err := RunMigrations(newStubMigrationsDB(t, []driver.Value{int64(8), false}))
	if !errors.Is(err, migratorErr) {
		t.Fatalf("RunMigrations() error = %v, want injected migrator failure", err)
	}
	if !strings.Contains(err.Error(), "build migrator") {
		t.Fatalf("RunMigrations() error = %v, want wrapped build-migrator error", err)
	}
}

// ---- ObservabilityWarnings：仓库根解析失败 ----

// TestObservabilityWarningsRepoRootUnresolved 覆盖 repoRoot 为空时的
// 提前返回分支（runtime.Caller 恒成功，空根只能经 seam 注入）。
func TestObservabilityWarningsRepoRootUnresolved(t *testing.T) {
	previous := repoRootFromSource
	repoRootFromSource = func() string { return "" }
	t.Cleanup(func() { repoRootFromSource = previous })

	warnings := ObservabilityWarnings(config.GetDefaultConfig(), "   ")
	if len(warnings) == 0 || !strings.Contains(warnings[0], "repository root could not be resolved") {
		t.Fatalf("ObservabilityWarnings() = %v, want unresolved repo root warning", warnings)
	}
	if len(warnings) != 1 {
		t.Fatalf("ObservabilityWarnings() = %v, want early return without asset checks", warnings)
	}
}

// ---- seam 默认路径 ----

// TestExpandEnvVarsInConfigDefaultPathSucceeds 保证 createExpandedConfigFile
// seam 的默认路径行为不变。
func TestExpandEnvVarsInConfigDefaultPathSucceeds(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(src, []byte("port: ${SERVIFY_TEST_PORT}\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	t.Setenv("SERVIFY_TEST_PORT", "9091")

	got := expandEnvVarsInConfig(src)
	if got == "" {
		t.Fatal("expandEnvVarsInConfig() = empty, want expanded temp file path")
	}
	defer os.Remove(got)
	content, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read expanded config: %v", err)
	}
	if string(content) != "port: 9091\n" {
		t.Fatalf("expanded content = %q, want %q", content, "port: 9091\n")
	}
}
