package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	appserver "servify/apps/server/internal/app/server"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// standaloneSeamState 保存全部启动编排 seam，供注入测试结束后还原默认
// （生产实现），避免包级 var 污染后续测试。
type standaloneSeamState struct {
	resolveSchemaMode      func(string) SchemaManagementMode
	runVersionedMigrations func(*gorm.DB) error
	startAppRuntime        func(*App) error
	startAppWorkers        func(*App) error
	shutdownAppGracefully  func(*App, context.Context) error
	shutdownHTTPServer     func(*http.Server, context.Context) error
}

func captureStandaloneSeams() standaloneSeamState {
	return standaloneSeamState{
		resolveSchemaMode:      resolveSchemaMode,
		runVersionedMigrations: runVersionedMigrations,
		startAppRuntime:        startAppRuntime,
		startAppWorkers:        startAppWorkers,
		shutdownAppGracefully:  shutdownAppGracefully,
		shutdownHTTPServer:     shutdownHTTPServer,
	}
}

func (s standaloneSeamState) restore() {
	resolveSchemaMode = s.resolveSchemaMode
	runVersionedMigrations = s.runVersionedMigrations
	startAppRuntime = s.startAppRuntime
	startAppWorkers = s.startAppWorkers
	shutdownAppGracefully = s.shutdownAppGracefully
	shutdownHTTPServer = s.shutdownHTTPServer
}

// withFault 保存 seam → 注入 fault → 测试结束自动还原。
func withFault(t *testing.T, fault string) {
	t.Helper()
	state := captureStandaloneSeams()
	t.Cleanup(state.restore)
	ApplyFault(fault)
}

// standaloneBufferLogger 返回写入缓冲的 logger，供断言编排段日志。
func standaloneBufferLogger(buf *bytes.Buffer) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(buf)
	l.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableQuote: true})
	return l
}

// standaloneHTTPFixture 返回未监听的 http.Server（Shutdown 对未监听 server
// 安全返回 nil）。
func standaloneHTTPFixture() *http.Server {
	return NewHTTPServer(nil, http.NewServeMux(), HTTPServerOptions{})
}

func TestShutdownStandaloneHappyPath(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := standaloneBufferLogger(buf)
	app := &App{Logger: logger}

	err := shutdownStandalone(logger, app, standaloneHTTPFixture())

	require.NoError(t, err)
	logged := buf.String()
	assert.Contains(t, logged, "Server exited")
	assert.NotContains(t, logged, "Failed to")
}

func TestShutdownStandaloneAppShutdownError(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := standaloneBufferLogger(buf)
	app := &App{Logger: logger}

	state := captureStandaloneSeams()
	t.Cleanup(state.restore)
	shutdownAppGracefully = func(*App, context.Context) error {
		return fmt.Errorf("app shutdown boom")
	}

	err := shutdownStandalone(logger, app, standaloneHTTPFixture())

	require.NoError(t, err)
	logged := buf.String()
	assert.Contains(t, logged, "Failed to shutdown cleanly: app shutdown boom")
	assert.Contains(t, logged, "Server exited")
}

func TestShutdownStandaloneHTTPShutdownError(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := standaloneBufferLogger(buf)

	state := captureStandaloneSeams()
	t.Cleanup(state.restore)
	shutdownHTTPServer = func(*http.Server, context.Context) error {
		return fmt.Errorf("shutdown boom")
	}

	err := shutdownStandalone(logger, &App{Logger: logger}, standaloneHTTPFixture())

	require.EqualError(t, err, "Server forced to shutdown: shutdown boom")
	// HTTP 关停失败是收尾硬错误：提前返回，"Server exited" 不再输出。
	assert.NotContains(t, buf.String(), "Server exited")
}

// TestStandaloneSeamDefaults 验证 seam 默认值即生产实现：空 App 的生命周期
// 方法与未监听 server 的 Shutdown 返回 nil，schema 模式解析与导出函数一致。
func TestStandaloneSeamDefaults(t *testing.T) {
	app := &App{}
	srv := standaloneHTTPFixture()
	ctx := context.Background()

	assert.NoError(t, startAppRuntime(app))
	assert.NoError(t, startAppWorkers(app))
	assert.NoError(t, shutdownAppGracefully(app, ctx))
	assert.NoError(t, shutdownHTTPServer(srv, ctx))
	assert.Equal(t, ResolveSchemaMode("sqlite"), resolveSchemaMode("sqlite"))
	assert.Equal(t, ResolveSchemaMode("postgres"), resolveSchemaMode("postgres"))
}

func TestApplyFaultUnknownIsNoOp(t *testing.T) {
	before := captureStandaloneSeams()
	ApplyFault("nonexistent")
	after := captureStandaloneSeams()

	assert.Equal(t,
		fmt.Sprintf("%v", before.resolveSchemaMode("sqlite"))+fmt.Sprint(before.startAppRuntime(&App{})),
		fmt.Sprintf("%v", after.resolveSchemaMode("sqlite"))+fmt.Sprint(after.startAppRuntime(&App{})),
	)
}

// TestApplyFaultVariantsMutateSeams 直验各 fault 变体确实改写了对应 seam
// （子进程注入行为随 seam 本体在包内锚定）。
func TestApplyFaultVariantsMutateSeams(t *testing.T) {
	t.Run("workers", func(t *testing.T) {
		withFault(t, "workers")
		assert.NoError(t, startAppRuntime(&App{}))
		assert.EqualError(t, startAppWorkers(&App{}), "injected worker failure")
	})
	t.Run("shutdown", func(t *testing.T) {
		withFault(t, "shutdown")
		assert.NoError(t, startAppRuntime(&App{}))
		assert.NoError(t, startAppWorkers(&App{}))
		assert.EqualError(t, shutdownAppGracefully(&App{}, context.Background()), "injected shutdown failure")
	})
	t.Run("start-runtime", func(t *testing.T) {
		withFault(t, "start-runtime")
		assert.EqualError(t, startAppRuntime(&App{}), "injected start failure")
	})
	t.Run("shutdown-server", func(t *testing.T) {
		withFault(t, "shutdown-server")
		assert.EqualError(t, shutdownHTTPServer(standaloneHTTPFixture(), context.Background()), "injected server shutdown failure")
	})
	t.Run("versioned", func(t *testing.T) {
		withFault(t, "versioned")
		assert.Equal(t, SchemaModeVersioned, resolveSchemaMode("sqlite"))
		assert.NoError(t, runVersionedMigrations(nil))
	})
	t.Run("versioned-fail", func(t *testing.T) {
		withFault(t, "versioned-fail")
		assert.Equal(t, SchemaModeVersioned, resolveSchemaMode("sqlite"))
		assert.EqualError(t, runVersionedMigrations(nil), "injected migration failure")
	})
}

// TestRunStandaloneEventBusDecorator 覆盖 decorator 非 nil 分支：闭包收到
// BuildApp 产出的事件总线且其返回值被接线（返回原 bus——后续 subscriber
// 会真实消费，不能换空壳实现）。
func TestRunStandaloneEventBusDecorator(t *testing.T) {
	withFault(t, "start-runtime")
	cfg := standaloneRunnableConfig(t, 0)

	var called bool
	var got eventbus.Bus
	err := RunStandalone(cfg, StandaloneOptions{
		EventBusDecorator: func(bus eventbus.Bus, _ *logrus.Logger) eventbus.Bus {
			called = true
			got = bus
			return bus
		},
	})
	// start-runtime 注入让编排快速失败，但 decorator 已在 BuildApp 后执行。
	require.EqualError(t, err, "Failed to start runtime: injected start failure")
	assert.True(t, called)
	assert.NotNil(t, got)
}

// TestRunStandaloneTracingSetupWarns 覆盖 SetupObservability 失败只告警不
// 中断：非法 OTEL 资源环境让 tracing 初始化失败，编排继续推进到被注入
// 失败的 StartRuntime。
func TestRunStandaloneTracingSetupWarns(t *testing.T) {
	withFault(t, "start-runtime")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "not-a-valid-entry")
	cfg := standaloneRunnableConfig(t, 0)
	cfg.Monitoring.Tracing.Enabled = true

	runStandaloneExpectError(t, cfg, StandaloneOptions{},
		"Failed to start runtime: injected start failure")
}

// TestRunStandaloneAutoMigrateRequestedFlag 覆盖 SERVIFY_AUTO_MIGRATE 提示
// 日志分支（sqlite 本就走 AutoMigrate，该 env 只影响提示输出）。
func TestRunStandaloneAutoMigrateRequestedFlag(t *testing.T) {
	withFault(t, "start-runtime")
	t.Setenv("MIGRATIONS_ENABLED", "true")
	t.Setenv("SERVIFY_AUTO_MIGRATE", "true")
	cfg := standaloneRunnableConfig(t, 0)

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to start runtime: injected start failure")
}

// TestRunStandaloneAutoMigrateFailure 覆盖 AutoMigrate 失败分支：预置
// users 视图与建表冲突，GORM AutoMigrate 失败返回 "Failed to migrate
// database"（与 cmd/server 的 TestServerMainMigrateFailure 同手法）。
func TestRunStandaloneAutoMigrateFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "conflict.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE VIEW users AS SELECT 1 AS id").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", dsn)
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.Monitoring.Enabled = false

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to migrate database")
}

// standaloneRunnableConfig 返回 sqlite（env overrides）+ 指定端口的可运行
// 配置：driver 与 DSN 经 ResolveRuntimeOverrides 的环境默认值生效。同进程
// 多次全量装配会重复注册全局 Prometheus collector（initializeObservability），
// 与编排测试无关，关闭 monitoring 规避。
func standaloneRunnableConfig(t *testing.T, port int) *config.Config {
	t.Helper()
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "standalone.db"))
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Monitoring.Enabled = false
	return cfg
}

// runStandaloneExpectError 进程内直调 RunStandalone 并断言错误前缀文案
// （编排根在各阶段失败时以原 Fatalf 文案返回，不触达阻塞段）。
func runStandaloneExpectError(t *testing.T, cfg *config.Config, opts StandaloneOptions, wantContains ...string) {
	t.Helper()
	err := RunStandalone(cfg, opts)
	require.Error(t, err)
	for _, want := range wantContains {
		assert.Contains(t, err.Error(), want)
	}
}

func TestRunStandaloneParseFailure(t *testing.T) {
	runStandaloneExpectError(t, config.GetDefaultConfig(),
		StandaloneOptions{StartupArgs: []string{"-port=not-a-number"}},
		"Failed to parse startup options")
}

func TestRunStandaloneBuildAppFailure(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Embedding.Provider = "tei"
	cfg.Embedding.TEI.BaseURL = ""

	runStandaloneExpectError(t, cfg, StandaloneOptions{}, "Failed to build app")
}

func TestRunStandaloneDatabaseFailure(t *testing.T) {
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", filepath.Join("/dev/null", "not", "a", "db.sqlite"))

	runStandaloneExpectError(t, config.GetDefaultConfig(), StandaloneOptions{}, "Failed to connect to database")
}

// TestRunStandaloneVersionedMigrationBranch 借 seam 把 sqlite 场景推入
// postgres-only 的 versioned 分支并注入 StartRuntime 失败：验证 case 日志与
// "Failed to start runtime" 返回。
func TestRunStandaloneVersionedMigrationBranch(t *testing.T) {
	withFault(t, "versioned")
	cfg := standaloneRunnableConfig(t, 0)

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to start runtime: injected runtime failure")
}

func TestRunStandaloneVersionedMigrationFailure(t *testing.T) {
	withFault(t, "versioned-fail")
	cfg := standaloneRunnableConfig(t, 0)

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to run database migrations: injected migration failure")
}

func TestRunStandaloneSkipMigrationBranch(t *testing.T) {
	withFault(t, "start-runtime")
	t.Setenv("MIGRATIONS_ENABLED", "false")
	cfg := standaloneRunnableConfig(t, 0)

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to start runtime: injected start failure")
}

func TestRunStandaloneAutoMigrateBranch(t *testing.T) {
	withFault(t, "start-runtime")
	t.Setenv("MIGRATIONS_ENABLED", "true")
	cfg := standaloneRunnableConfig(t, 0)

	// sqlite 默认走 AutoMigrate：真实建表成功后推进到被注入失败的
	// StartRuntime，同时证明 auto 分支执行。
	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to start runtime: injected start failure")

	db, err := gorm.Open(sqlite.Open(os.Getenv("DB_DSN")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = 'users'").Scan(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestRunStandaloneBuildRuntimeFailure(t *testing.T) {
	cfg := standaloneRunnableConfig(t, 0)
	cfg.Fallback.Enabled = false
	cfg.WeKnora.Enabled = true
	cfg.WeKnora.BaseURL = "http://127.0.0.1:1"

	runStandaloneExpectError(t, cfg, StandaloneOptions{}, "Failed to build runtime")
}

func TestRunStandaloneWorkersFailure(t *testing.T) {
	withFault(t, "workers")
	cfg := standaloneRunnableConfig(t, 0)

	runStandaloneExpectError(t, cfg, StandaloneOptions{
		RegisterWorkers: func(*App, *config.Config, *gorm.DB, *appserver.Runtime) {},
	}, "Failed to start workers: injected worker failure")
}

// TestRunStandaloneHappyPath 完整生命周期：sqlite 起服 → /health 200 →
// SIGTERM → RunStandalone 返回 nil 且日志含关停段三件。
func TestRunStandaloneHappyPath(t *testing.T) {
	port := standaloneFreePort(t)
	cfg := standaloneRunnableConfig(t, port)

	errCh := make(chan error, 1)
	go func() { errCh <- RunStandalone(cfg, StandaloneOptions{}) }()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 200*time.Millisecond, "standalone server did not become healthy")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(60 * time.Second):
		t.Fatal("RunStandalone did not return after SIGTERM")
	}
}

// TestRunStandaloneHTTPShutdownFailure 覆盖尾段的 srv.Shutdown 失败分支：
// seam 注入后 RunStandalone 以 "Server forced to shutdown" 返回。
func TestRunStandaloneHTTPShutdownFailure(t *testing.T) {
	withFault(t, "shutdown-server")
	port := standaloneFreePort(t)
	cfg := standaloneRunnableConfig(t, port)

	errCh := make(chan error, 1)
	go func() { errCh <- RunStandalone(cfg, StandaloneOptions{}) }()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 200*time.Millisecond, "standalone server did not become healthy")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "Server forced to shutdown: injected server shutdown failure")
	case <-time.After(60 * time.Second):
		t.Fatal("RunStandalone did not return after SIGTERM")
	}
}

func standaloneFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}
