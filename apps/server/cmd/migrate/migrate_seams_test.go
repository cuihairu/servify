package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// applyMigrateFault 在子进程内按 SERVIFY_MIGRATE_FAULT 注入 seam 分支：生产
// 不设置该变量；versioned 分支仅 driver=postgres 进入，sqlite 子进程测试通过
// 这里强制走该分支以覆盖其成功与 fatal 路径。
func applyMigrateFault(fault string) {
	switch fault {
	case "versioned":
		usesVersionedMigrations = func(string) bool { return true }
		runVersionedMigrations = func(*gorm.DB) error { return nil }
	case "versioned-fail":
		usesVersionedMigrations = func(string) bool { return true }
		runVersionedMigrations = func(*gorm.DB) error { return errors.New("injected migration failure") }
	}
}

// TestMainVersionedMigrationsApplied 覆盖 versioned 分支的成功路径（含
// "Versioned migrations applied successfully!" 日志）。
func TestMainVersionedMigrationsApplied(t *testing.T) {
	dir := t.TempDir()

	out, code := runMigrateSubprocess(t, dir,
		[]string{"SERVIFY_MIGRATE_FAULT=versioned"},
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "versioned.db"),
	)
	assert.EqualValues(t, 0, code, "migrate subprocess output: %s", out)
	assert.Contains(t, out, "Versioned migrations applied successfully!")
	assert.Contains(t, out, "Migration process completed!")
	// versioned 分支不跑 AutoMigrate / CreateIndexes。
	assert.NotContains(t, out, "Additional indexes created successfully!")
}

// TestMainVersionedMigrationsFailure 覆盖 versioned 迁移失败的 fatal 分支。
func TestMainVersionedMigrationsFailure(t *testing.T) {
	dir := t.TempDir()

	out, code := runMigrateSubprocess(t, dir,
		[]string{"SERVIFY_MIGRATE_FAULT=versioned-fail"},
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "versioned-fail.db"),
	)
	assert.EqualValues(t, 1, code, "migrate subprocess output: %s", out)
	assert.Contains(t, out, "Failed to run versioned migrations: injected migration failure")
}

// TestMigrateSeamDefaults 验证 seam 默认值即生产实现：postgres 判定为
// versioned，sqlite 不是。
func TestMigrateSeamDefaults(t *testing.T) {
	assert.Equal(t, appbootstrap.UsesVersionedMigrations("postgres"), usesVersionedMigrations("postgres"))
	assert.Equal(t, appbootstrap.UsesVersionedMigrations("sqlite"), usesVersionedMigrations("sqlite"))
	assert.True(t, usesVersionedMigrations("postgres"))
	assert.False(t, usesVersionedMigrations("sqlite"))
	assert.NotNil(t, runVersionedMigrations)
}

// TestApplyMigrateFaultUnknownIsNoOp 未知 fault 值不改 seam。
func TestApplyMigrateFaultUnknownIsNoOp(t *testing.T) {
	before := usesVersionedMigrations("sqlite")
	applyMigrateFault("nonexistent")
	assert.Equal(t, before, usesVersionedMigrations("sqlite"))
}

// 子进程 worker 需要在 main() 前应用 fault；复用 main_test.go 的
// TestMigrateSubprocess 环境变量协议。
func init() {
	if os.Getenv("SERVIFY_MIGRATE_SUBPROCESS") == "1" {
		applyMigrateFault(os.Getenv("SERVIFY_MIGRATE_FAULT"))
	}
}
