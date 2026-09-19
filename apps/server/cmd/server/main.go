package main

import (
	"os"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	appserver "servify/apps/server/internal/app/server"
	appworker "servify/apps/server/internal/app/worker"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/observability/async"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// main 是薄入口：config 加载 + 共享启动编排（RunStandalone）。装配细节
// （观测接线、数据库重试、schema 管理、runtime/workers/HTTP server 生命周期
// 与统一 shutdown）全部收口在 bootstrap 层，与 cmd/cli run 共享同一编排根；
// 测试 seam 一并托管在 bootstrap.ApplyFault（见 server_seams_test.go）。
func main() {
	cfg, err := appbootstrap.LoadConfig("")
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}
	if err := appbootstrap.RunStandalone(cfg, appbootstrap.StandaloneOptions{
		StartupArgs:   os.Args[1:],
		StartupOutput: os.Stdout,
		Migrate:       true,
		RegisterWorkers: func(app *appbootstrap.App, cfg *config.Config, db *gorm.DB, rt *appserver.Runtime) {
			appworker.RegisterDefaultWorkers(app, cfg, db, rt)
		},
		EventBusDecorator: func(bus eventbus.Bus, logger *logrus.Logger) eventbus.Bus {
			return async.WireDefaultObservableBus(bus, logger)
		},
	}); err != nil {
		logrus.Fatalf("%v", err)
	}
}
