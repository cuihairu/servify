package cli

import (
	appbootstrap "servify/apps/server/internal/app/bootstrap"
	appserver "servify/apps/server/internal/app/server"
	appworker "servify/apps/server/internal/app/worker"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/observability/async"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
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

// run 是薄入口：config 加载 + 共享启动编排（bootstrap.RunStandalone，与
// cmd/server 同一编排根）。差异仅三点：不传启动 flags（只吃 env/cfg 默认值，
// DB_DRIVER/DB_DSN/SERVIFY_PORT 经 ResolveRuntimeOverrides 生效）、不做迁移
// （期望预迁移的库）、不注册后台 workers（轻量本地运行定位）。路由面即
// BuildRouter 全量（生产同源）。测试 seam 托管在 bootstrap.ApplyFault
// （见 cli_shutdown_test.go 的子进程注入）。
func run(cmd *cobra.Command, args []string) {
	cfg, err := appbootstrap.LoadConfig("")
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}
	if err := appbootstrap.RunStandalone(cfg, appbootstrap.StandaloneOptions{
		RegisterWorkers: func(app *appbootstrap.App, cfg *config.Config, db *gorm.DB, rt *appserver.Runtime) {
			// CLI 保持轻量本地运行定位，但仍需投递事务性 ticket.created
			// outbox 行（否则创建事件永久停留）。
			appworker.RegisterTicketOutboxWorker(app, app.Logger, rt)
		},
		EventBusDecorator: func(bus eventbus.Bus, logger *logrus.Logger) eventbus.Bus {
			return async.WireDefaultObservableBus(bus, logger)
		},
	}); err != nil {
		logrus.Fatalf("%v", err)
	}
}
