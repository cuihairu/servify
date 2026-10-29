package bootstrap

// 覆盖补充：BackfillTelegramEventIDs 的错误分支（Find/Delete/UpdateColumns
// 注入失败）与 allDigits 空串防御分支——sqlite 触发器/缺表注入，无生产 seam。

import (
	"testing"

	"servify/apps/server/internal/platform/channel"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// gormOpenMemory 打开未迁移的内存库（仅供缺表错误注入）。
func gormOpenMemory(t *testing.T) (*gorm.DB, error) {
	t.Helper()
	return gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
}

// TestTelegramEventIDBackfillFindError 覆盖 inbox 表缺失时 Find 失败的分支：
// 未迁移的裸内存库直接查询报 "no such table"。
func TestTelegramEventIDBackfillFindError(t *testing.T) {
	db, err := gormOpenMemory(t)
	require.NoError(t, err)
	_, err = BackfillTelegramEventIDs(db)
	require.Error(t, err)
	require.Contains(t, err.Error(), "backfill telegram event ids")
}

// TestTelegramEventIDBackfillDeleteError 覆盖目标键占用行删除失败分支：
// BEFORE DELETE 触发器 RAISE，旧行删除即报错。
func TestTelegramEventIDBackfillDeleteError(t *testing.T) {
	db := newTelegramBackfillDB(t)
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7")
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "-100123:7", "inst-1\x1f-100123:7")
	require.NoError(t, db.Exec(`CREATE TRIGGER block_inbox_delete BEFORE DELETE ON chat_connector_inboxes
		BEGIN SELECT RAISE(ABORT, 'injected delete failure'); END;`).Error)

	_, err := BackfillTelegramEventIDs(db)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected delete failure")
}

// TestTelegramEventIDBackfillUpdateError 覆盖旧行重写（UpdateColumns）失败
// 分支：BEFORE UPDATE 触发器 RAISE，回填 UPDATE 命中即报错。
func TestTelegramEventIDBackfillUpdateError(t *testing.T) {
	db := newTelegramBackfillDB(t)
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7")
	require.NoError(t, db.Exec(`CREATE TRIGGER block_inbox_update BEFORE UPDATE ON chat_connector_inboxes
		BEGIN SELECT RAISE(ABORT, 'injected update failure'); END;`).Error)

	_, err := BackfillTelegramEventIDs(db)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected update failure")
}

// TestTelegramEventIDBackfillEmptyEventIDSkipped 覆盖 allDigits 空串防御
// 分支：event_id 为空的 telegram 行不是旧行，跳过不动。
func TestTelegramEventIDBackfillEmptyEventIDSkipped(t *testing.T) {
	db := newTelegramBackfillDB(t)
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "", "inst-1\x1f")

	touched, err := BackfillTelegramEventIDs(db)
	require.NoError(t, err)
	require.EqualValues(t, 0, touched)
	var row channel.ChatConnectorInbox
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, "", row.EventID)
}

// TestAllDigitsBoundary 覆盖 allDigits 的空串与非 ASCII 数字边界。
func TestAllDigitsBoundary(t *testing.T) {
	require.False(t, allDigits(""))
	require.False(t, allDigits("１２３"))
	require.True(t, allDigits("0"))
	require.False(t, allDigits("12a3"))
}
