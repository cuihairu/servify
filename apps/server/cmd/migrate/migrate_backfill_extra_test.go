package main

// 覆盖补充：sqlite AutoMigrate 分支的 scope 回填失败 fatal——tickets 预挂
// BEFORE UPDATE 触发器，回填 UPDATE 命中空 scope 行即 RAISE，子进程以
// "Failed to backfill default scope" 退出（复用 main_test.go 的子进程协议）。

import (
	"path/filepath"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMainBackfillFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "backfill-fail.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Ticket{}))
	require.NoError(t, db.Exec(`
		INSERT INTO tickets (title, created_at, updated_at) VALUES ('t', datetime('now'), datetime('now'));
		CREATE TRIGGER block_scope_backfill BEFORE UPDATE ON tickets
		BEGIN SELECT RAISE(ABORT, 'injected backfill failure'); END;`).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	out, code := runMigrateSubprocess(t, dir, nil, "-db-driver=sqlite", "-dsn="+dsn)
	assert.EqualValues(t, 1, code, "migrate subprocess output: %s", out)
	assert.Contains(t, out, "Failed to backfill default scope")
}
