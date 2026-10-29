package bootstrap

// 覆盖补充：standalone 编排的 Telegram EventID 回填两分支——回填命中行数
// 落日志、回填库错误上抛（sqlite 触发器注入，无生产 seam）。惯例参照
// standalone_extra_test.go 的 scope 回填两分支。

import (
	"path/filepath"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/channel"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// telegramBackfillStandaloneSeedSQL 预置一行旧格式 telegram message.created
// 行（key 经参数绑定注入 \x1f 分隔，纯 Go sqlite 驱动无 chr()）。
var telegramBackfillStandaloneSeedSQL = `INSERT INTO chat_connector_inboxes
	(tenant_id, workspace_id, idempotency_key, platform, installation_id,
	 event_id, event_type, status, conversation_platform_id,
	 sender_platform_id, message_platform_id, occurred_at, received_at, created_at, updated_at)
	VALUES (?, 'w1', ?, 'telegram', 'inst-1', '7', 'message.created', 'committed',
		'-100123', 'u1', '7', datetime('now'), datetime('now'), datetime('now'), datetime('now'))`

// seedTelegramBackfillStandaloneDB 预置 sqlite 文件库：建 inbox 表并按需
// 插入一行旧格式 telegram message.created 行 / 挂 UPDATE 触发器。
func seedTelegramBackfillStandaloneDB(t *testing.T, dsn string, withTrigger bool) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&channel.ChatConnectorInbox{}))
	require.NoError(t, db.Exec(telegramBackfillStandaloneSeedSQL, "t1", "inst-1\x1f7").Error)
	if withTrigger {
		require.NoError(t, db.Exec(`CREATE TRIGGER block_inbox_update BEFORE UPDATE ON chat_connector_inboxes
			BEGIN SELECT RAISE(ABORT, 'injected telegram backfill failure'); END;`).Error)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func standaloneTelegramCfg() *config.Config {
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.Monitoring.Enabled = false
	return cfg
}

// TestRunStandaloneTelegramBackfillLogsTouchedRows 覆盖回填命中行数 > 0 的
// 日志分支：预置一行旧格式 telegram 行，AutoMigrate 幂等后回填重写并记日志，
// 随后被 start-runtime 注入失败快速收束编排。
func TestRunStandaloneTelegramBackfillLogsTouchedRows(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "telegram-seeded.db")
	seedTelegramBackfillStandaloneDB(t, dsn, false)

	withFault(t, "start-runtime")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", dsn)

	runStandaloneExpectError(t, standaloneTelegramCfg(), StandaloneOptions{Migrate: true},
		"failed to start runtime: injected start failure")

	// 回填确实生效：旧行 event_id 已重写为 chatID:messageID。
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	var row channel.ChatConnectorInbox
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, "-100123:7", row.EventID)
}

// TestRunStandaloneTelegramBackfillFailure 覆盖回填失败分支：inbox 表预挂
// BEFORE UPDATE 触发器，回填 UPDATE 命中旧行即 RAISE，编排以
// "failed to backfill telegram event ids" 返回。
func TestRunStandaloneTelegramBackfillFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "telegram-backfill-fail.db")
	seedTelegramBackfillStandaloneDB(t, dsn, true)

	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", dsn)

	runStandaloneExpectError(t, standaloneTelegramCfg(), StandaloneOptions{Migrate: true},
		"failed to backfill telegram event ids")
}
