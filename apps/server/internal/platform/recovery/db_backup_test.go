package recovery

// db_backup_test.go 覆盖数据库备份/恢复的错误分支与序列恢复语义。
// 演练主路径（happy path + 损坏注入）在 drill_test.go。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func openRawSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openGormSQLite(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		raw, rawErr := db.DB()
		if rawErr == nil {
			_ = raw.Close()
		}
	})
	return db
}

// TestBackupDatabaseRejectsPostgres 锁定 pg 方言的显式拒绝：生产备份走
// pg_dump SOP，静默用 sqlite 逻辑备份处理 pg 是错误行为。用最小 stub
// dialector 模拟 postgres（gorm.Open 不做任何连接），方言检查先于连接
// 获取，因此分支在无 pg 环境也可确定性触发。
func TestBackupDatabaseRejectsPostgres(t *testing.T) {
	pgDB, err := gorm.Open(stubDialector{name: "postgres"}, &gorm.Config{})
	require.NoError(t, err)

	_, backupErr := BackupDatabase(pgDB, filepath.Join(t.TempDir(), "out"))
	assert.ErrorContains(t, backupErr, "pg_dump runbook")
	_, restoreErr := RestoreDatabase(pgDB, filepath.Join(t.TempDir(), "out"))
	assert.ErrorContains(t, restoreErr, "pg_restore runbook")
}

// stubDialector 是只报名字的最小 dialector，用于方言拒绝分支测试。
type stubDialector struct{ name string }

func (d stubDialector) Name() string { return d.name }

func (d stubDialector) Initialize(*gorm.DB) error { return nil }

func (d stubDialector) Migrator(*gorm.DB) gorm.Migrator { return nil }

func (d stubDialector) DataTypeOf(*schema.Field) string { return "" }

func (d stubDialector) DefaultValueOf(*schema.Field) clause.Expression { return clause.Expr{} }

func (d stubDialector) BindVarTo(clause.Writer, *gorm.Statement, any) {}

func (d stubDialector) QuoteTo(clause.Writer, string) {}

func (d stubDialector) Explain(sql string, _ ...any) string { return sql }

// TestBackupDatabaseClosesDBErrors 用关闭的连接注入查询失败分支。
func TestBackupDatabaseClosesDBErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "x.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, backupErr := BackupDatabase(db, filepath.Join(t.TempDir(), "out"))
	assert.ErrorContains(t, backupErr, "list tables")
}

func TestBackupDatabaseCannotCreateOutputDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o644))

	db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error)

	_, err := BackupDatabase(db, filepath.Join(blocker, "out"))
	assert.ErrorContains(t, err, "create backup dir")
}

func TestRestoreDatabaseManifestErrors(t *testing.T) {
	db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
	backupDir := t.TempDir()

	// manifest 缺失。
	_, err := RestoreDatabase(db, backupDir)
	assert.ErrorContains(t, err, "read manifest")

	// manifest 版本不符。
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "manifest.json"),
		[]byte(`{"version": 99, "dialect": "sqlite", "tables": []}`), 0o644))
	_, err = RestoreDatabase(db, backupDir)
	assert.ErrorIs(t, err, errUnsupportedManifestVersion)

	// manifest 内容损坏。
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "manifest.json"),
		[]byte("{not json"), 0o644))
	_, err = RestoreDatabase(db, backupDir)
	assert.ErrorContains(t, err, "parse manifest")

	// 数据文件校验和不匹配（防篡改口径）。
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "manifest.json"),
		[]byte(`{"version": 1, "dialect": "sqlite", "tables": [{"name": "t", "columns": ["id", "name"], "rows": 1, "file": "tables/t.jsonl", "sha256": "deadbeef"}]}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(backupDir, "tables"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "tables", "t.jsonl"), []byte("[1, \"x\"]\n"), 0o644))
	_, err = RestoreDatabase(db, backupDir)
	assert.ErrorIs(t, err, errChecksumMismatch)
}

// TestRestoreDatabaseDetectsRowWidthMismatch 锁定 dump 行宽防御：
// 列数与 manifest 不符的行必须显式失败，而不是错位写入。
func TestRestoreDatabaseDetectsRowWidthMismatch(t *testing.T) {
	db := openGormSQLite(t, filepath.Join(t.TempDir(), "target.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)

	source := openGormSQLite(t, filepath.Join(t.TempDir(), "source.db"))
	require.NoError(t, source.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
	require.NoError(t, source.Exec("INSERT INTO t (id, name) VALUES (1, 'a')").Error)

	backupDir := t.TempDir()
	manifest, err := BackupDatabase(source, backupDir)
	require.NoError(t, err)

	// 篡改行宽并同步改写校验和，确保失败发生在行宽检查而非校验和。
	dumpPath := filepath.Join(backupDir, manifest.Tables[0].File)
	require.NoError(t, os.WriteFile(dumpPath, []byte("[1, \"a\", \"extra\"]\n"), 0o644))
	entry := manifest.Tables[0]
	entry.SHA256 = mustSHA256(t, dumpPath)
	manifest.Tables[0] = entry
	writeManifestFile(t, backupDir, manifest)

	_, err = RestoreDatabase(db, backupDir)
	assert.ErrorContains(t, err, "does not match")
}

// TestRestoreDatabaseRejectsNullIntoNotNull 锁定回写阶段的数据错误传播：
// 违反 NOT NULL 的行让整个恢复失败（事务回滚，目标库不留半恢复状态）。
func TestRestoreDatabaseRejectsNullIntoNotNull(t *testing.T) {
	db := openGormSQLite(t, filepath.Join(t.TempDir(), "target.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO t (id, name) VALUES (5, 'pre-existing')").Error)

	source := openGormSQLite(t, filepath.Join(t.TempDir(), "source.db"))
	require.NoError(t, source.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL)").Error)
	require.NoError(t, source.Exec("INSERT INTO t (id, name) VALUES (1, 'a'), (2, 'b')").Error)

	backupDir := t.TempDir()
	manifest, err := BackupDatabase(source, backupDir)
	require.NoError(t, err)

	// 把其中一行 name 篡改为 null（并同步校验和），回写必须在第二行失败。
	dumpPath := filepath.Join(backupDir, manifest.Tables[0].File)
	require.NoError(t, os.WriteFile(dumpPath, []byte("[1, \"a\"]\n[2, null]\n"), 0o644))
	entry := manifest.Tables[0]
	entry.SHA256 = mustSHA256(t, dumpPath)
	manifest.Tables[0] = entry
	writeManifestFile(t, backupDir, manifest)

	_, err = RestoreDatabase(db, backupDir)
	assert.ErrorContains(t, err, "NOT NULL")

	// 半恢复状态不可见：失败后目标表仍是恢复前的内容。
	targetSQL, rawErr := db.DB()
	require.NoError(t, rawErr)
	var count int
	require.NoError(t, targetSQL.QueryRow("SELECT count(*) FROM t").Scan(&count))
	assert.Equal(t, 1, count)
	var name string
	require.NoError(t, targetSQL.QueryRow("SELECT name FROM t WHERE id = 5").Scan(&name))
	assert.Equal(t, "pre-existing", name)
}

// TestSequenceRestoration 覆盖 sqlite_sequence 的三个分支：备份源无
// AUTOINCREMENT（空序列）、恢复目标无 AUTOINCREMENT（跳过）、正常回插
// （恢复后自增从上次位置继续，不撞旧行）。
func TestSequenceRestoration(t *testing.T) {
	source := openRawSQLite(t, filepath.Join(t.TempDir(), "source.db"))
	_, err := source.ExecContext(context.Background(), "CREATE TABLE plain_t (id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)
	_, err = source.ExecContext(context.Background(), "INSERT INTO plain_t (name) VALUES ('x')")
	require.NoError(t, err)

	plainDir := t.TempDir()
	plainDB := openGormSQLite(t, filepath.Join(t.TempDir(), "plain.db"))
	plainManifest, err := BackupDatabase(plainDB, plainDir)
	require.NoError(t, err)
	assert.Empty(t, plainManifest.Sequence, "no AUTOINCREMENT table means no sequence entries")

	autoDir := t.TempDir()
	autoSource := openRawSQLite(t, filepath.Join(t.TempDir(), "auto-source.db"))
	_, err = autoSource.ExecContext(context.Background(), "CREATE TABLE auto_t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)")
	require.NoError(t, err)
	_, err = autoSource.ExecContext(context.Background(), "INSERT INTO auto_t (name) VALUES ('a'), ('b')")
	require.NoError(t, err)

	autoGorm := openGormSQLite(t, filepath.Join(t.TempDir(), "auto.db"))
	_, err = autoGorm.DB()
	require.NoError(t, err)
	require.NoError(t, autoGorm.Exec("CREATE TABLE auto_t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)").Error)
	require.NoError(t, autoGorm.Exec("INSERT INTO auto_t (name) VALUES ('a'), ('b')").Error)

	autoManifest, err := BackupDatabase(autoGorm, autoDir)
	require.NoError(t, err)
	require.Len(t, autoManifest.Sequence, 1)
	assert.Equal(t, "auto_t", autoManifest.Sequence[0].Name)
	assert.EqualValues(t, 2, autoManifest.Sequence[0].Seq)

	// 目标无 AUTOINCREMENT：序列恢复被跳过而非失败。
	noSeqTarget := openGormSQLite(t, filepath.Join(t.TempDir(), "no-seq.db"))
	require.NoError(t, noSeqTarget.Exec("CREATE TABLE auto_t (id INTEGER PRIMARY KEY, name TEXT)").Error)
	_, err = RestoreDatabase(noSeqTarget, autoDir)
	require.NoError(t, err)

	// 正常目标：恢复后 sqlite_sequence 回插，新行从 3 开始。
	seqTarget := openGormSQLite(t, filepath.Join(t.TempDir(), "seq.db"))
	require.NoError(t, seqTarget.Exec("CREATE TABLE auto_t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)").Error)
	_, err = RestoreDatabase(seqTarget, autoDir)
	require.NoError(t, err)
	require.NoError(t, seqTarget.Exec("INSERT INTO auto_t (name) VALUES ('c')").Error)
	var maxID int
	require.NoError(t, seqTarget.Raw("SELECT max(id) FROM auto_t").Scan(&maxID).Error)
	assert.Equal(t, 3, maxID, "sequence continues after restore instead of colliding")
}

func writeManifestFile(t *testing.T, backupDir string, manifest DatabaseManifest) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "manifest.json"), raw, 0o644))
}

func mustSHA256(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
