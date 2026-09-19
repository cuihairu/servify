package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	appserver "servify/apps/server/internal/app/server"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/eventbus"

	gormlogger "gorm.io/gorm/logger"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// 启动编排 seam：默认全部指向生产实现，生产行为不变。RunStandalone 在
// WaitForShutdownSignal 阻塞、错误分支在真实依赖下难以全部触达，子进程测试
// 经 ApplyFault（SERVIFY_SERVER_FAULT / CLI_RUN_VARIANT 驱动）注入失败以
// 覆盖这些防御性分支（见 cmd/server/server_seams_test.go 与 cmd/cli 测试）。
var (
	resolveSchemaMode      = ResolveSchemaMode
	runVersionedMigrations = RunMigrations
	startAppRuntime        = func(app *App) error { return app.StartRuntime() }
	startAppWorkers        = func(app *App) error { return app.StartWorkers() }
	shutdownAppGracefully  = func(app *App, ctx context.Context) error { return app.Shutdown(ctx) }
	shutdownHTTPServer     = func(srv *http.Server, ctx context.Context) error { return srv.Shutdown(ctx) }
)

// ApplyFault 在子进程内按 fault 值注入 seam 失败/分支。仅供子进程测试使用；
// 生产进程不调用，seam 保持默认（生产实现）。
func ApplyFault(fault string) {
	switch fault {
	case "versioned":
		// versioned 分支 + RunMigrations 成功 + StartRuntime 失败：快速结束，
		// 不真正监听端口。
		resolveSchemaMode = func(string) SchemaManagementMode { return SchemaModeVersioned }
		runVersionedMigrations = func(*gorm.DB) error { return nil }
		startAppRuntime = func(*App) error { return fmt.Errorf("injected runtime failure") }
	case "versioned-fail":
		resolveSchemaMode = func(string) SchemaManagementMode { return SchemaModeVersioned }
		runVersionedMigrations = func(*gorm.DB) error { return fmt.Errorf("injected migration failure") }
	case "workers":
		startAppRuntime = func(*App) error { return nil }
		startAppWorkers = func(*App) error { return fmt.Errorf("injected worker failure") }
	case "shutdown":
		startAppRuntime = func(*App) error { return nil }
		startAppWorkers = func(*App) error { return nil }
		shutdownAppGracefully = func(*App, context.Context) error { return fmt.Errorf("injected shutdown failure") }
	case "start-runtime":
		startAppRuntime = func(*App) error { return fmt.Errorf("injected start failure") }
	case "shutdown-server":
		shutdownHTTPServer = func(*http.Server, context.Context) error { return fmt.Errorf("injected server shutdown failure") }
	}
}

// StandaloneOptions 描述一次独立进程启动编排的入口差异点：cmd/server 与
// cmd/cli run 共享同一编排根（RunStandalone），仅以下字段不同。
type StandaloneOptions struct {
	// StartupArgs/StartupOutput 传给 ResolveRuntimeOverrides；cmd/server 传
	// os.Args[1:]/os.Stdout，CLI 传 nil, nil 只吃环境变量与配置默认值。
	StartupArgs   []string
	StartupOutput io.Writer
	// Migrate 为 true 时执行 schema 管理分支（versioned/automigrate/skip）；
	// CLI 留 false（期望预迁移的库）。
	Migrate bool
	// RegisterWorkers 非 nil 时在 runtime 启动后注册默认后台 workers；
	// cmd/server 传 appworker.RegisterDefaultWorkers 包装（bootstrap 不能
	// 反向 import app/worker），CLI 留 false（轻量本地运行定位）。
	RegisterWorkers func(app *App, cfg *config.Config, db *gorm.DB, rt *appserver.Runtime)
	// EventBusDecorator 非空时对 BuildApp 产出的事件总线做观测包装
	// （bootstrap 不能 import async：async→bootstrap for Worker，故由入口
	// 注入 async.WireDefaultObservableBus）。
	EventBusDecorator func(bus eventbus.Bus, logger *logrus.Logger) eventbus.Bus
}

// RunStandalone 是独立进程的启动编排根：overrides 解析 → BuildApp → 事件总线
// 观测接线 → 观测初始化 → 数据库（重试）→ schema 管理 → runtime 装配与启动 →
// 可选 workers → HTTP server → 等待信号 → 优雅关停。每个阶段的错误以原
// Fatalf 文案前缀返回，由入口统一 Fatal 退出（退出码与文案不变）。
func RunStandalone(cfg *config.Config, opts StandaloneOptions) error {
	overrides, err := ResolveRuntimeOverrides(cfg, opts.StartupArgs, opts.StartupOutput)
	if err != nil {
		return fmt.Errorf("Failed to parse startup options: %w", err)
	}
	app, err := BuildApp(cfg)
	if err != nil {
		return fmt.Errorf("Failed to build app: %w", err)
	}
	logger := app.Logger

	if opts.EventBusDecorator != nil {
		app.EventBus = opts.EventBusDecorator(app.EventBus, logger)
	}

	if err := SetupObservability(context.Background(), cfg, app); err != nil {
		logger.Warnf("init tracing: %v", err)
	}

	dbOpts := overrides.Database
	dbOpts.LogLevel = gormlogger.Info
	dbOpts.EnableTracing = cfg.Monitoring.Tracing.Enabled
	db, err := OpenDatabaseWithRetry(cfg, dbOpts, DatabaseRetryOptions{
		MaxRetries: 10,
		RetryDelay: 2 * time.Second,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("Failed to connect to database: %w", err)
	}
	app.DB = db

	if opts.Migrate {
		switch resolveSchemaMode(dbOpts.Driver) {
		case SchemaModeSkip:
			logger.Info("Automatic schema management disabled (MIGRATIONS_ENABLED is falsy); expecting a pre-migrated database")
		case SchemaModeVersioned:
			logger.Info("Applying versioned database migrations...")
			if err := runVersionedMigrations(db); err != nil {
				return fmt.Errorf("Failed to run database migrations: %w", err)
			}
		case SchemaModeAutoMigrate:
			if AutoMigrateRequested() {
				logger.Info("SERVIFY_AUTO_MIGRATE is set; running legacy GORM AutoMigrate instead of versioned migrations")
			}
			if err := AutoMigrate(db); err != nil {
				return fmt.Errorf("Failed to migrate database: %w", err)
			}
		}
	}

	runtime, err := app.BuildServerRuntime()
	if err != nil {
		return fmt.Errorf("Failed to build runtime: %w", err)
	}
	if err := startAppRuntime(app); err != nil {
		return fmt.Errorf("Failed to start runtime: %w", err)
	}

	if opts.RegisterWorkers != nil {
		opts.RegisterWorkers(app, cfg, db, runtime)
		if err := startAppWorkers(app); err != nil {
			return fmt.Errorf("Failed to start workers: %w", err)
		}
	}

	srv := BuildHTTPServer(app, overrides.HTTP)
	StartHTTPServer(srv, logger, fmt.Sprintf("Starting server on %s", srv.Addr))

	WaitForShutdownSignal()
	logger.Info("Shutting down server...")
	return shutdownStandalone(logger, app, srv)
}

// shutdownStandalone 执行收尾段：跑 app 级 shutdown（workers/runtime/shutdown
// hooks 聚合）、关 HTTP server。拆出成可直接调用的函数便于包内测试；HTTP
// server 关闭失败是收尾硬错误，交由入口 Fatal 退出（与原 cmd/server 语义
// 一致）。
func shutdownStandalone(logger *logrus.Logger, app *App, srv *http.Server) error {
	ctx, cancel := ShutdownContext(30 * time.Second)
	defer cancel()

	if err := shutdownAppGracefully(app, ctx); err != nil {
		logger.Errorf("Failed to shutdown cleanly: %v", err)
	}
	if err := shutdownHTTPServer(srv, ctx); err != nil {
		return fmt.Errorf("Server forced to shutdown: %w", err)
	}
	logger.Info("Server exited")
	return nil
}
