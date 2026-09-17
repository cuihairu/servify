package main

import (
	"context"
	"fmt"
	"os"
	"time"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	appworker "servify/apps/server/internal/app/worker"
	"servify/apps/server/internal/observability/async"
	svcmetrics "servify/apps/server/internal/observability/metrics"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm/logger"
)

// main 的测试 seam：默认全部指向生产实现，生产行为不变。main 只能在子进程
// 测试里执行，而 versioned 迁移分支仅 postgres driver 会进入、
// StartRuntime/StartWorkers/Shutdown 的错误分支在真实依赖下恒为 nil，子进程
// 测试通过 SERVIFY_SERVER_FAULT 环境变量（见 server_seams_test.go）注入失败
// 以覆盖这些防御性分支。
var (
	resolveSchemaMode      = appbootstrap.ResolveSchemaMode
	runVersionedMigrations = appbootstrap.RunMigrations
	startRuntime           = func(app *appbootstrap.App) error { return app.StartRuntime() }
	startWorkers           = func(app *appbootstrap.App) error { return app.StartWorkers() }
	shutdownApp            = func(app *appbootstrap.App, ctx context.Context) error { return app.Shutdown(ctx) }
)

func main() {
	cfg, err := appbootstrap.LoadConfig("")
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}

	overrides, err := appbootstrap.ResolveRuntimeOverrides(cfg, os.Args[1:], os.Stdout)
	if err != nil {
		logrus.Fatalf("Failed to parse startup options: %v", err)
	}
	app, err := appbootstrap.BuildApp(cfg)
	if err != nil {
		logrus.Fatalf("Failed to build app: %v", err)
	}
	appLogger := app.Logger

	// 事件总线观测接线：发布/订阅/失败/死信全量打点（monitoring 关闭时
	// collector 注册在进程级 registry，/metrics 未挂载即无暴露面，无副作用）。
	app.EventBus = async.NewObservableBus(
		app.EventBus,
		async.NewBusMetrics(svcmetrics.DefaultRegistry),
		async.NewInMemoryDeadLetterRecorder(1000),
		appLogger,
	)

	if err := appbootstrap.SetupObservability(context.Background(), cfg, app); err != nil {
		appLogger.Warnf("init tracing: %v", err)
	}

	dbOpts := overrides.Database
	dbOpts.LogLevel = logger.Info
	dbOpts.EnableTracing = cfg.Monitoring.Tracing.Enabled
	db, err := appbootstrap.OpenDatabaseWithRetry(cfg, dbOpts, appbootstrap.DatabaseRetryOptions{
		MaxRetries: 10,
		RetryDelay: 2 * time.Second,
		Logger:     appLogger,
	})
	if err != nil {
		appLogger.Fatalf("Failed to connect to database: %v", err)
	}
	app.DB = db

	switch resolveSchemaMode(dbOpts.Driver) {
	case appbootstrap.SchemaModeSkip:
		appLogger.Info("Automatic schema management disabled (MIGRATIONS_ENABLED is falsy); expecting a pre-migrated database")
	case appbootstrap.SchemaModeVersioned:
		appLogger.Info("Applying versioned database migrations...")
		if err := runVersionedMigrations(db); err != nil {
			appLogger.Fatalf("Failed to run database migrations: %v", err)
		}
	case appbootstrap.SchemaModeAutoMigrate:
		if appbootstrap.AutoMigrateRequested() {
			appLogger.Info("SERVIFY_AUTO_MIGRATE is set; running legacy GORM AutoMigrate instead of versioned migrations")
		}
		if err := appbootstrap.AutoMigrate(db); err != nil {
			appLogger.Fatalf("Failed to migrate database: %v", err)
		}
	}

	runtime, err := app.BuildServerRuntime()
	if err != nil {
		appLogger.Fatalf("Failed to build runtime: %v", err)
	}
	if err := startRuntime(app); err != nil {
		appLogger.Fatalf("Failed to start runtime: %v", err)
	}

	appworker.RegisterDefaultWorkers(app, cfg, db, runtime)
	if err := startWorkers(app); err != nil {
		appLogger.Fatalf("Failed to start workers: %v", err)
	}

	srv := appbootstrap.BuildHTTPServer(app, overrides.HTTP)
	appbootstrap.StartHTTPServer(srv, appLogger, fmt.Sprintf("Starting server on %s", srv.Addr))

	appbootstrap.WaitForShutdownSignal()
	appLogger.Info("Shutting down server...")
	shutdownCtx, cancel := appbootstrap.ShutdownContext(30 * time.Second)
	defer cancel()
	if err := shutdownApp(app, shutdownCtx); err != nil {
		appLogger.Errorf("Failed to shutdown cleanly: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		appLogger.Fatalf("Server forced to shutdown: %v", err)
	}
	appLogger.Info("Server exited")
}
