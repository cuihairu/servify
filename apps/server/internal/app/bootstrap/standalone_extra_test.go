package bootstrap

// 覆盖补充：standalone 编排的 scope 回填两分支——回填命中行数落日志、
// 回填库错误上抛（sqlite 触发器注入，无生产 seam）。

import (
	"path/filepath"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedScopeBackfillDB 预置 sqlite 文件库：建指定业务表并插入一行 scope
// 为空的记录（回填条件 tenant_id = ”）。
func seedScopeBackfillDB(t *testing.T, dsn string, model interface{}, insertSQL string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model))
	require.NoError(t, db.Exec(insertSQL).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// TestRunStandaloneBackfillLogsTouchedRows 覆盖回填命中行数 > 0 的日志
// 分支：预置一行空 scope 记录，AutoMigrate 幂等后回填命中并记日志，随后
// 被 start-runtime 注入失败快速收束编排。
func TestRunStandaloneBackfillLogsTouchedRows(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "seeded.db")
	seedScopeBackfillDB(t, dsn, &models.Customer{},
		"INSERT INTO customers (created_at, updated_at) VALUES (datetime('now'), datetime('now'))")

	withFault(t, "start-runtime")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", dsn)
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.Monitoring.Enabled = false

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to start runtime: injected start failure")
}

// TestRunStandaloneBackfillFailure 覆盖回填失败的 fatal 分支：tickets 上
// 预挂 BEFORE UPDATE 触发器，回填 UPDATE 命中空 scope 行即触发 RAISE，
// 编排以 "Failed to backfill default scope" 返回。
func TestRunStandaloneBackfillFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "backfill-fail.db")
	seedScopeBackfillDB(t, dsn, &models.Ticket{}, `
		INSERT INTO tickets (title, created_at, updated_at) VALUES ('t', datetime('now'), datetime('now'));
		CREATE TRIGGER block_scope_backfill BEFORE UPDATE ON tickets
		BEGIN SELECT RAISE(ABORT, 'injected backfill failure'); END;`)

	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", dsn)
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.Monitoring.Enabled = false

	runStandaloneExpectError(t, cfg, StandaloneOptions{Migrate: true},
		"Failed to backfill default scope")
}
