package main

// 覆盖补充：migrate 主流程 sqlite AutoMigrate 分支的 Telegram EventID 回填
// 失败 fatal——chat_connector_inboxes 预挂 BEFORE UPDATE 触发器，旧行回填
// UPDATE 命中即 RAISE，子进程以 "Failed to backfill telegram event ids"
// 退出（复用 main_test.go 的子进程协议，惯例参照 migrate_backfill_extra_test.go）。

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMainTelegramBackfillFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "telegram-backfill-fail.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE chat_connector_inboxes (
		id integer PRIMARY KEY AUTOINCREMENT,
		tenant_id text NOT NULL,
		workspace_id text NOT NULL,
		idempotency_key text NOT NULL,
		platform text NOT NULL,
		installation_id text NOT NULL,
		event_id text NOT NULL,
		event_type text NOT NULL,
		status text NOT NULL,
		conversation_platform_id text NOT NULL,
		sender_platform_id text NOT NULL,
		message_platform_id text NOT NULL,
		occurred_at datetime,
		received_at datetime,
		created_at datetime,
		updated_at datetime
	)`).Error)
	// key 经参数绑定注入 \x1f 分隔（纯 Go sqlite 驱动无 chr()）。
	require.NoError(t, db.Exec(`INSERT INTO chat_connector_inboxes
		(tenant_id, workspace_id, idempotency_key, platform, installation_id,
		 event_id, event_type, status, conversation_platform_id,
		 sender_platform_id, message_platform_id, occurred_at, received_at, created_at, updated_at)
		VALUES (?, 'w1', ?, 'telegram', 'inst-1', '7', 'message.created', 'committed',
			'-100123', 'u1', '7', datetime('now'), datetime('now'), datetime('now'), datetime('now'))`,
		"t1", "inst-1\x1f7").Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER block_inbox_update BEFORE UPDATE ON chat_connector_inboxes
		BEGIN SELECT RAISE(ABORT, 'injected telegram backfill failure'); END;`).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	out, code := runMigrateSubprocess(t, dir, nil, "-db-driver=sqlite", "-dsn="+dsn)
	assert.EqualValues(t, 1, code, "migrate subprocess output: %s", out)
	assert.Contains(t, out, "Failed to backfill telegram event ids")
}
