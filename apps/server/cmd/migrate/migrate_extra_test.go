package main

import (
	"path/filepath"
	"testing"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestMainAutoMigrateFlagLogsLegacyPath 覆盖显式 -auto-migrate 的日志分支：
// sqlite 本来就走 AutoMigrate，只有显式传 flag 才打印旧路径提示。
func TestMainAutoMigrateFlagLogsLegacyPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	out, code := runMigrateSubprocess(t, dir, nil,
		"-db-driver=sqlite", "-dsn="+dbPath, "-auto-migrate")
	assert.EqualValues(t, 0, code, "migrate subprocess output: %s", out)
	assert.Contains(t, out, "-auto-migrate set; running legacy GORM AutoMigrate")
	assert.Contains(t, out, "Additional indexes created successfully!")
}

// TestMainCreateIndexesFailure 覆盖 CreateIndexes 的错误 fatal 分支：
// 预先创建与第一个索引同名的 TABLE，AutoMigrate 仍然成功（该名字不属于
// 任何模型），而 CREATE INDEX IF NOT EXISTS 在 sqlite 下报
// "there is already a table named ..."。
func TestMainCreateIndexesFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index-conflict.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, appbootstrap.AutoMigrate(db))
	require.NoError(t, db.Exec("CREATE TABLE idx_messages_session_created (id integer)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	out, code := runMigrateSubprocess(t, dir, nil, "-db-driver=sqlite", "-dsn="+dbPath)
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to create indexes")
}
