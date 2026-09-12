package main

import (
	"context"
	"fmt"
	"os"
	"time"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	appworker "servify/apps/server/internal/app/worker"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm/logger"
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

	switch appbootstrap.ResolveSchemaMode(dbOpts.Driver) {
	case appbootstrap.SchemaModeSkip:
		appLogger.Info("Automatic schema management disabled (MIGRATIONS_ENABLED is falsy); expecting a pre-migrated database")
	case appbootstrap.SchemaModeVersioned:
		appLogger.Info("Applying versioned database migrations...")
		if err := appbootstrap.RunMigrations(db); err != nil {
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
	if err := app.StartRuntime(); err != nil {
		appLogger.Fatalf("Failed to start runtime: %v", err)
	}

	appworker.RegisterDefaultWorkers(app, cfg, db, runtime)
	if err := app.StartWorkers(); err != nil {
		appLogger.Fatalf("Failed to start workers: %v", err)
	}

	srv := appbootstrap.BuildHTTPServer(app, overrides.HTTP)
	appbootstrap.StartHTTPServer(srv, appLogger, fmt.Sprintf("Starting server on %s", srv.Addr))

	appbootstrap.WaitForShutdownSignal()
	appLogger.Info("Shutting down server...")
	shutdownCtx, cancel := appbootstrap.ShutdownContext(30 * time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		appLogger.Errorf("Failed to shutdown cleanly: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		appLogger.Fatalf("Server forced to shutdown: %v", err)
	}
	appLogger.Info("Server exited")
}
