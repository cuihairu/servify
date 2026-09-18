package main

// main_test.go 覆盖 dbrecovery 的参数解析、子命令分发与错误出口。
// main() 本体经子进程执行（进程隔离 exit/fatal，同 migrate 命令惯例）；
// parseArgs/run 使用独立 FlagSet，可在测试进程内直接调用。

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/recovery"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetenvDefault(t *testing.T) {
	t.Setenv("SERVIFY_TEST_RECOVERY_VAR", "value")
	assert.Equal(t, "value", getenvDefault("SERVIFY_TEST_RECOVERY_VAR", "fallback"))
	assert.Equal(t, "fallback", getenvDefault("SERVIFY_TEST_RECOVERY_VAR_MISSING", "fallback"))
}

func TestParseArgs(t *testing.T) {
	opts, err := parseArgs([]string{"files-backup", "-dir", "/data", "-archive", "/tmp/a.tar.gz"})
	require.NoError(t, err)
	assert.Equal(t, "files-backup", opts.command)
	assert.Equal(t, "/data", opts.dir)
	assert.Equal(t, "/tmp/a.tar.gz", opts.archive)
	assert.Equal(t, "sqlite", opts.dbDriver, "default driver is sqlite")

	_, err = parseArgs(nil)
	assert.ErrorContains(t, err, "missing command")

	_, err = parseArgs([]string{"db-backup", "-no-such-flag"})
	assert.Error(t, err)
}

func TestRunUnknownCommand(t *testing.T) {
	opts, err := parseArgs([]string{"reboot"})
	require.NoError(t, err)
	err = opts.run()
	assert.ErrorContains(t, err, `unknown command "reboot"`)
}

func TestRunDBCommandsRejectPostgres(t *testing.T) {
	for _, command := range []string{"db-backup", "db-restore"} {
		opts, err := parseArgs([]string{command, "-db-driver", "postgres"})
		require.NoError(t, err)
		err = opts.run()
		assert.ErrorContains(t, err, "pg_dump runbook")
	}
}

func TestRunDBBackupConfigFailure(t *testing.T) {
	dir := t.TempDir()
	opts, err := parseArgs([]string{"db-backup", "-config", filepath.Join(dir, "missing.yml"), "-out", dir})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "load config")
}

func TestRunDBBackupConnectFailure(t *testing.T) {
	dir := t.TempDir()
	opts, err := parseArgs([]string{
		"db-backup",
		"-dsn", filepath.Join(dir, "no-such-dir", "x.db"),
		"-out", filepath.Join(dir, "backup"),
	})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "connect database")
}

// seedRecoveryDB 建一个带 schema 与两行数据的 sqlite 库。
func seedRecoveryDB(t *testing.T, path string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, appbootstrap.AutoMigrate(db))
	require.NoError(t, db.Create(&models.User{Username: "u1", Email: "u1@t", Role: "customer", Status: "active"}).Error)
	require.NoError(t, db.Create(&models.User{Username: "u2", Email: "u2@t", Role: "customer", Status: "active"}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func countUsers(t *testing.T, path string) int64 {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	defer func() {
		sqlDB, rawErr := db.DB()
		if rawErr == nil {
			_ = sqlDB.Close()
		}
	}()
	var count int64
	require.NoError(t, db.Model(&models.User{}).Count(&count).Error)
	return count
}

func TestDBBackupRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	seedRecoveryDB(t, dbPath)
	backupDir := filepath.Join(dir, "backup")

	backupOpts, err := parseArgs([]string{"db-backup", "-dsn", dbPath, "-out", backupDir})
	require.NoError(t, err)
	require.NoError(t, backupOpts.run())

	// 备份后新增一行，恢复后必须被清掉。
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.User{Username: "post-backup", Email: "pb@t", Role: "customer", Status: "active"}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	restoreOpts, err := parseArgs([]string{"db-restore", "-dsn", dbPath, "-backup", backupDir})
	require.NoError(t, err)
	require.NoError(t, restoreOpts.run())

	assert.EqualValues(t, 2, countUsers(t, dbPath), "restore rolls the database back to the backup point")
}

func TestFilesBackupRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, "uploads")
	require.NoError(t, os.MkdirAll(filepath.Join(uploads, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "a.txt"), []byte("alpha"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "nested", "b.bin"), []byte{0, 1, 2, 255}, 0o644))

	archive := filepath.Join(dir, "uploads.tar.gz")
	backupOpts, err := parseArgs([]string{"files-backup", "-dir", uploads, "-archive", archive})
	require.NoError(t, err)
	require.NoError(t, backupOpts.run())

	manifestPath := archive + ".manifest.json"
	_, err = os.Stat(manifestPath)
	require.NoError(t, err, "default manifest path is next to the archive")

	// 恢复到全新目录并字节级对账。
	dest := filepath.Join(dir, "restored")
	restoreOpts, err := parseArgs([]string{"files-restore", "-archive", archive, "-manifest", manifestPath, "-dest", dest})
	require.NoError(t, err)
	require.NoError(t, restoreOpts.run())

	var manifest recovery.FilesManifest
	raw, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &manifest))
	assert.Empty(t, recovery.VerifyFiles(dest, manifest))

	restored, err := os.ReadFile(filepath.Join(dest, "nested", "b.bin"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 1, 2, 255}, restored)
}

func TestRunFilesBackupMissingFlags(t *testing.T) {
	opts, err := parseArgs([]string{"files-backup", "-dir", "/data"})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "requires -dir and -archive")
}

func TestRunFilesRestoreMissingFlags(t *testing.T) {
	opts, err := parseArgs([]string{"files-restore", "-archive", "/tmp/a.tar.gz"})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "requires -archive, -manifest and -dest")
}

func TestRunFilesRestoreBadManifest(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o600))

	opts, err := parseArgs([]string{"files-restore", "-archive", filepath.Join(dir, "a.tar.gz"), "-manifest", bad, "-dest", dir})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "parse manifest")
}

func TestRunFilesBackupSourceMissing(t *testing.T) {
	dir := t.TempDir()
	opts, err := parseArgs([]string{"files-backup", "-dir", filepath.Join(dir, "missing"), "-archive", filepath.Join(dir, "a.tar.gz")})
	require.NoError(t, err)
	assert.Error(t, opts.run())
}

func TestRunDBBackupTargetNotCreatable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	seedRecoveryDB(t, dbPath)
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o600))

	opts, err := parseArgs([]string{"db-backup", "-dsn", dbPath, "-out", filepath.Join(blocker, "backup")})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "create backup dir")
}

func TestRunDBRestoreBackupMissing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	seedRecoveryDB(t, dbPath)

	opts, err := parseArgs([]string{"db-restore", "-dsn", dbPath, "-backup", filepath.Join(dir, "no-backup")})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "read manifest")
}

func TestRunFilesBackupManifestNotWritable(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, "uploads")
	require.NoError(t, os.MkdirAll(uploads, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "a.txt"), []byte("alpha"), 0o644))
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o600))

	opts, err := parseArgs([]string{
		"files-backup", "-dir", uploads,
		"-archive", filepath.Join(dir, "a.tar.gz"),
		"-manifest", filepath.Join(blocker, "m.json"),
	})
	require.NoError(t, err)
	assert.ErrorContains(t, opts.run(), "write manifest")
}

func TestRunFilesRestoreArchiveMissing(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`{"version": 1, "files": []}`), 0o600))

	opts, err := parseArgs([]string{"files-restore", "-archive", filepath.Join(dir, "missing.tar.gz"), "-manifest", manifestPath, "-dest", dir})
	require.NoError(t, err)
	assert.Error(t, opts.run())
}

func TestWriteFilesManifestEncodeError(t *testing.T) {
	original := jsonMarshalIndent
	jsonMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errInjectedMarshal }
	t.Cleanup(func() { jsonMarshalIndent = original })

	err := writeFilesManifest(filepath.Join(t.TempDir(), "m.json"), recovery.FilesManifest{})
	assert.ErrorContains(t, err, "encode manifest")
}

var errInjectedMarshal = errors.New("injected marshal failure")

func TestWriteFilesManifestError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o600))

	err := writeFilesManifest(filepath.Join(blocker, "m.json"), recovery.FilesManifest{})
	assert.ErrorContains(t, err, "write manifest")
}

func TestReadFilesManifestErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := readFilesManifest(filepath.Join(dir, "missing.json"))
	assert.ErrorContains(t, err, "read manifest")

	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o600))
	_, err = readFilesManifest(bad)
	assert.ErrorContains(t, err, "parse manifest")
}

// TestMainSubprocess is executed inside a child process; it runs main()
// with arguments provided through SERVIFY_DBRECOVERY_ARGS (\x1f separated).
func TestMainSubprocess(t *testing.T) {
	if os.Getenv("SERVIFY_DBRECOVERY_SUBPROCESS") != "1" {
		return
	}
	var args []string
	if raw := os.Getenv("SERVIFY_DBRECOVERY_ARGS"); raw != "" {
		args = strings.Split(raw, "\x1f")
	}
	os.Args = append([]string{"dbrecovery"}, args...)
	main()
	os.Exit(0)
}

func runSubprocess(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainSubprocess$", "-test.timeout=2m")
	cmd.Env = append(os.Environ(),
		"SERVIFY_DBRECOVERY_SUBPROCESS=1",
		"SERVIFY_DBRECOVERY_ARGS="+strings.Join(args, "\x1f"),
	)
	out, _ := cmd.CombinedOutput()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return string(out), code
}

func TestMainExitPaths(t *testing.T) {
	out, code := runSubprocess(t)
	assert.EqualValues(t, 1, code, "missing command exits non-zero")
	assert.Contains(t, out, "missing command")

	out, code = runSubprocess(t, "reboot")
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "unknown command")

	dir := t.TempDir()
	uploads := filepath.Join(dir, "uploads")
	require.NoError(t, os.MkdirAll(uploads, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "a.txt"), []byte("alpha"), 0o644))
	archive := filepath.Join(dir, "a.tar.gz")

	// 成功路径经 main() 本体（而非 run）走完整输出。
	out, code = runSubprocess(t, "files-backup", "-dir", uploads, "-archive", archive)
	assert.EqualValues(t, 0, code, "files-backup output: %s", out)
	assert.Contains(t, out, "files-backup completed")
}
