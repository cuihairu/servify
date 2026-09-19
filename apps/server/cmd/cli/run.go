package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/observability/async"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the servify application",
	Long:  `Run the servify application`,
	Run:   run,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

// run 的测试 seam：默认全部指向生产实现，生产行为不变。run() 只能在子进程
// 测试里执行（WaitForShutdownSignal 阻塞），错误分支在真实依赖下无法触达，
// 子进程测试通过 CLI_RUN_VARIANT 环境变量（见 cli_shutdown_test.go）注入
// seam 失败以覆盖这些防御性分支。
var (
	cliStartRuntime   = func(app *appbootstrap.App) error { return app.StartRuntime() }
	cliShutdownApp    = func(app *appbootstrap.App, ctx context.Context) error { return app.Shutdown(ctx) }
	cliShutdownServer = func(srv *http.Server, ctx context.Context) error { return srv.Shutdown(ctx) }
)

// shutdownRuntime 执行 run() 的收尾段：跑 app 级 shutdown（workers/runtime/
// shutdown hooks 聚合）、关 HTTP server。从 run() 抽出成可直接调用的函数
// 便于测试；语句与新装配逐一致。
func shutdownRuntime(appLogger *logrus.Logger, app *appbootstrap.App, srv *http.Server) {
	ctx, cancel := appbootstrap.ShutdownContext(30 * time.Second)
	defer cancel()

	if err := cliShutdownApp(app, ctx); err != nil {
		appLogger.Errorf("Failed to shutdown cleanly: %v", err)
	}
	if err := cliShutdownServer(srv, ctx); err != nil {
		appLogger.Errorf("Server forced to shutdown: %v", err)
	}
	appLogger.Info("Server exited")
}

// run 装配路径与 cmd/server 同构（LoadConfig → BuildApp → 观测接线 → 数据库
// → BuildServerRuntime → StartRuntime → HTTP server），路由面即 BuildRouter
// 全量（生产同源）。差异仅两点：CLI 不做迁移与自动建表（期望预迁移的库）、
// 不注册后台 workers（轻量本地运行定位）。
func run(cmd *cobra.Command, args []string) {
	cfg, err := appbootstrap.LoadConfig("")
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}
	// 环境变量 overrides（DB_DRIVER/DB_DSN/SERVIFY_PORT 等）与 cmd/server
	// 同一解析器；不传启动 flags（cobra 参数集不同），只吃 env/cfg 默认值。
	overrides, err := appbootstrap.ResolveRuntimeOverrides(cfg, nil, nil)
	if err != nil {
		logrus.Fatalf("Failed to resolve runtime overrides: %v", err)
	}
	app, err := appbootstrap.BuildApp(cfg)
	if err != nil {
		logrus.Fatalf("Failed to build app: %v", err)
	}
	appLogger := app.Logger

	// 事件总线观测接线（与 cmd/server 同源）。
	app.EventBus = async.WireDefaultObservableBus(app.EventBus, appLogger)

	if err := appbootstrap.SetupObservability(context.Background(), cfg, app); err != nil {
		appLogger.Warnf("init tracing: %v", err)
	}

	dbOpts := overrides.Database
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

	// 装配完整 runtime（AI/realtime/全业务链）并挂载路由（app.Router）。
	if _, err := app.BuildServerRuntime(); err != nil {
		appLogger.Fatalf("Failed to build runtime: %v", err)
	}

	if err := cliStartRuntime(app); err != nil {
		appLogger.Fatalf("Failed to start runtime: %v", err)
	}

	srv := appbootstrap.BuildHTTPServer(app, overrides.HTTP)
	appbootstrap.StartHTTPServer(srv, appLogger, fmt.Sprintf("Starting server on %s", srv.Addr))
	appbootstrap.WaitForShutdownSignal()

	appLogger.Info("Shutting down server...")
	shutdownRuntime(appLogger, app, srv)
}
