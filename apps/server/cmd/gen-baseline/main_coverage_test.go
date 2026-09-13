package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openSQLiteSeam 返回一个基于 sqlite 的 openBaselineDB seam 实现，并把原
// seam 注册到 t.Cleanup 恢复。生产实现（postgres）由
// TestOpenPostgresBaselineUnreachable 直接驱动。
func openSQLiteSeam(t *testing.T) string {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "baseline.db")
	orig := openBaselineDB
	openBaselineDB = func(dsn string, cfg *gorm.Config) (*gorm.DB, error) {
		return gorm.Open(sqlite.Open(dsn), cfg)
	}
	t.Cleanup(func() { openBaselineDB = orig })
	return dsn
}

// emptyProbeSeam 让空库探测恒返回 0（sqlite 兼容实现）。
func emptyProbeSeam(t *testing.T) {
	t.Helper()
	orig := probePublicTables
	probePublicTables = func(*gorm.DB) (int, error) { return 0, nil }
	t.Cleanup(func() { probePublicTables = orig })
}

// noopExtensionsSeam 跳过 postgres 扩展创建（sqlite 无对应概念；生产实现的
// 循环与错误返回由 TestCreatePostgresExtensionsSQLiteFails 直接驱动）。
func noopExtensionsSeam(t *testing.T) {
	t.Helper()
	orig := prepareExtensions
	prepareExtensions = func(*gorm.DB) error { return nil }
	t.Cleanup(func() { prepareExtensions = orig })
}

// TestRunSQLiteHappyPathToWriter 用 sqlite 驱动 run() 全流程：空库探测、
// AutoMigrate、CreateIndexes、DDL 抓取与非 DDL 过滤，最终写 stdout。
func TestRunSQLiteHappyPathToWriter(t *testing.T) {
	dsn := openSQLiteSeam(t)
	emptyProbeSeam(t)
	noopExtensionsSeam(t)

	var out bytes.Buffer
	require.NoError(t, run(dsn, "", &out))
	generated := out.String()
	assert.Contains(t, generated, "CREATE TABLE")
	assert.Contains(t, generated, "CREATE INDEX")
	assert.True(t, strings.HasSuffix(generated, ";\n"), "every statement must end with \";\\n\"")
}

// TestRunSQLiteHappyPathToFile 覆盖 -out 落盘分支。
func TestRunSQLiteHappyPathToFile(t *testing.T) {
	dsn := openSQLiteSeam(t)
	emptyProbeSeam(t)
	noopExtensionsSeam(t)

	target := filepath.Join(t.TempDir(), "baseline.sql")
	require.NoError(t, run(dsn, target, io.Discard))
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(content), "CREATE TABLE")
}

// TestRunMissingDSN 覆盖缺 DSN 的入参校验。
func TestRunMissingDSN(t *testing.T) {
	err := run("   ", "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-dsn or GEN_BASELINE_DSN is required")
}

// TestRunConnectErrorWrapped 覆盖连接失败分支（seam 注入，不经真实拨号）。
func TestRunConnectErrorWrapped(t *testing.T) {
	dsn := openSQLiteSeam(t)
	orig := openBaselineDB
	openBaselineDB = func(string, *gorm.Config) (*gorm.DB, error) { return nil, errors.New("dial boom") }
	t.Cleanup(func() { openBaselineDB = orig })

	err := run(dsn, "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect scratch postgres: dial boom")
}

// TestRunProbeErrorWrapped 覆盖空库探测失败分支。
func TestRunProbeErrorWrapped(t *testing.T) {
	dsn := openSQLiteSeam(t)
	orig := probePublicTables
	probePublicTables = func(*gorm.DB) (int, error) { return 0, errors.New("probe boom") }
	t.Cleanup(func() { probePublicTables = orig })

	err := run(dsn, "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe existing tables: probe boom")
}

// TestRunScratchDBNotEmpty 覆盖非空库的防呆分支。
func TestRunScratchDBNotEmpty(t *testing.T) {
	dsn := openSQLiteSeam(t)
	orig := probePublicTables
	probePublicTables = func(*gorm.DB) (int, error) { return 3, nil }
	t.Cleanup(func() { probePublicTables = orig })

	err := run(dsn, "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scratch database is not empty (3 public tables)")
}

// TestRunExtensionErrorPropagates 覆盖扩展创建失败向上传播。
func TestRunExtensionErrorPropagates(t *testing.T) {
	dsn := openSQLiteSeam(t)
	emptyProbeSeam(t)
	orig := prepareExtensions
	prepareExtensions = func(*gorm.DB) error { return errors.New("create extension vector: no such extension") }
	t.Cleanup(func() { prepareExtensions = orig })

	err := run(dsn, "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create extension vector: no such extension")
}

// TestRunAutoMigrateFailure 用同名 VIEW 让 AutoMigrate 在 sqlite 上失败。
func TestRunAutoMigrateFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "view-clash.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE VIEW users AS SELECT 1 AS id").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	orig := openBaselineDB
	openBaselineDB = func(dsn string, cfg *gorm.Config) (*gorm.DB, error) { return gorm.Open(sqlite.Open(dsn), cfg) }
	t.Cleanup(func() { openBaselineDB = orig })
	emptyProbeSeam(t)
	noopExtensionsSeam(t)

	err = run(dsn, "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AutoMigrate:")
}

// TestRunCreateIndexesFailure 预建与索引同名的 TABLE：AutoMigrate 不受影响，
// CreateIndexes 的 CREATE INDEX 报名字冲突。
func TestRunCreateIndexesFailure(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "index-clash.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE idx_messages_session_created (id integer)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	orig := openBaselineDB
	openBaselineDB = func(dsn string, cfg *gorm.Config) (*gorm.DB, error) { return gorm.Open(sqlite.Open(dsn), cfg) }
	t.Cleanup(func() { openBaselineDB = orig })
	emptyProbeSeam(t)
	noopExtensionsSeam(t)

	err = run(dsn, "", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CreateIndexes:")
}

// TestOpenPostgresBaselineUnreachable 直接驱动生产 open seam：连接被拒绝时
// 返回错误（端口 1 立即 refused，无等待）。gorm.Open 失败时返回值里的
// *gorm.DB 不承载错误（其 Error 字段为 nil），错误只在返回的 err 上。
func TestOpenPostgresBaselineUnreachable(t *testing.T) {
	_, err := openPostgresBaseline(
		"postgres://postgres:postgres@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		&gorm.Config{},
	)
	require.Error(t, err, "open against a refused port must fail")
}

// TestProbePostgresPublicTablesSQLiteFails 直接驱动生产探测实现：pg_tables
// 元数据查询在 sqlite 下必然失败。
func TestProbePostgresPublicTablesSQLiteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "probe.db")), &gorm.Config{})
	require.NoError(t, err)
	count, err := probePostgresPublicTables(db)
	require.Error(t, err)
	assert.Zero(t, count)
}

// TestCreatePostgresExtensionsSQLiteFails 直接驱动生产扩展实现：CREATE
// EXTENSION 在 sqlite 下必然失败，覆盖循环体与错误返回。
func TestCreatePostgresExtensionsSQLiteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ext.db")), &gorm.Config{})
	require.NoError(t, err)
	err = createPostgresExtensions(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create extension vector")
}

// TestRunWriteFileFailure 覆盖 -out 写盘失败的 fatal 分支（父目录不存在）。
func TestRunWriteFileFailure(t *testing.T) {
	dsn := openSQLiteSeam(t)
	emptyProbeSeam(t)
	noopExtensionsSeam(t)

	dir := t.TempDir()
	bad := filepath.Join(dir, "missing-dir", "baseline.sql")
	err := run(dsn, bad, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write "+bad)
}

// TestCreatePostgresExtensionsNoExtensions 覆盖扩展列表为空时循环直接走
// 成功返回（数据 seam 默认仍为 vector/hstore，生产行为不变）。
func TestCreatePostgresExtensionsNoExtensions(t *testing.T) {
	orig := baselineExtensions
	baselineExtensions = nil
	t.Cleanup(func() { baselineExtensions = orig })

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "no-ext.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, createPostgresExtensions(db))

	// 默认列表恢复后回到生产语义：sqlite 上首个扩展即失败。
	baselineExtensions = orig
	extErr := createPostgresExtensions(db)
	require.Error(t, extErr)
	assert.Contains(t, extErr.Error(), "create extension vector")
}

// TestSummarizeSkipped 覆盖去重与 120 字节截断（接管全局 log 输出做断言）。
func TestSummarizeSkipped(t *testing.T) {
	var buf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOut) })

	long := strings.Repeat("x", 200)
	summarizeSkipped([]string{"SELECT 1", "SELECT 1", long})

	logged := buf.String()
	assert.Equal(t, 1, strings.Count(logged, "SKIP: SELECT 1"), "duplicates must be deduped: %s", logged)
	assert.Contains(t, logged, long[:120]+"...")
	assert.NotContains(t, logged, long[:130])
}

// TestRunSQLiteCapturesAndClassifies 验证抓取到的语句同时包含 DDL 与被跳过
// 的元数据探测（sqlite migrator 的 SELECT），并确保两者都被记录。
func TestRunSQLiteCapturesAndClassifies(t *testing.T) {
	dsn := openSQLiteSeam(t)
	emptyProbeSeam(t)
	noopExtensionsSeam(t)

	var buf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOut) })

	require.NoError(t, run(dsn, "", io.Discard))
	assert.Contains(t, buf.String(), "skipped")
	assert.Contains(t, buf.String(), "SKIP:")
}

// TestSeamDefaultsMatchProductionSeams 验证 seam 默认值指向生产实现：在
// sqlite 上调用默认 seam 应得到生产实现的 postgres 语义失败（pg_tables 不
// 存在、CREATE EXTENSION 不支持）。
func TestSeamDefaultsMatchProductionSeams(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "guard.db")), &gorm.Config{})
	require.NoError(t, err)

	_, probeErr := probePublicTables(db)
	require.Error(t, probeErr, "default probe seam must be the postgres implementation")
	assert.Contains(t, probeErr.Error(), "no such table")

	extErr := prepareExtensions(db)
	require.Error(t, extErr, "default extensions seam must be the postgres implementation")
	assert.Contains(t, extErr.Error(), "create extension vector")
}
