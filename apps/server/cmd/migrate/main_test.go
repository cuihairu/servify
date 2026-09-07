package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetenvDefault(t *testing.T) {
	t.Setenv("SERVIFY_TEST_ENV_VAR", "value")
	assert.Equal(t, "value", getenvDefault("SERVIFY_TEST_ENV_VAR", "fallback"))
	assert.Equal(t, "fallback", getenvDefault("SERVIFY_TEST_ENV_VAR_MISSING", "fallback"))

	t.Setenv("SERVIFY_TEST_ENV_VAR", "")
	assert.Equal(t, "fallback", getenvDefault("SERVIFY_TEST_ENV_VAR", "fallback"))
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "", firstNonEmpty())
	assert.Equal(t, "", firstNonEmpty("", ""))
	assert.Equal(t, "a", firstNonEmpty("", "a", "b"))
	assert.Equal(t, "b", firstNonEmpty("", "", "b", "c"))
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, appbootstrap.AutoMigrate(db))
	return db
}

func TestSeedDefaultData(t *testing.T) {
	db := openTestDB(t)

	seedDefaultData(db)

	var count int64
	require.NoError(t, db.Model(&models.User{}).Count(&count).Error)
	assert.EqualValues(t, 3, count)

	var admin models.User
	require.NoError(t, db.Where("username = ?", "admin").First(&admin).Error)
	assert.Equal(t, "admin", admin.Role)
	assert.Equal(t, "admin@servify.com", admin.Email)
	assert.Equal(t, "active", admin.Status)

	var customer models.Customer
	require.NoError(t, db.Where("user_id = ?", admin.ID+1).First(&customer).Error)
	assert.Equal(t, "technology", customer.Industry)

	var agent models.Agent
	require.NoError(t, db.Where("department = ?", "客户服务部").First(&agent).Error)
	assert.EqualValues(t, 5, agent.MaxConcurrent)

	var docs int64
	require.NoError(t, db.Model(&models.KnowledgeDoc{}).Count(&docs).Error)
	assert.EqualValues(t, 1, docs)

	var stats int64
	require.NoError(t, db.Model(&models.DailyStats{}).Count(&stats).Error)
	assert.EqualValues(t, 1, stats)

	// Seeding again must be idempotent.
	seedDefaultData(db)
	require.NoError(t, db.Model(&models.User{}).Count(&count).Error)
	assert.EqualValues(t, 3, count)
	require.NoError(t, db.Model(&models.Customer{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&models.Agent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestMainSQLiteDatabasePath(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	cfgPath := filepath.Join(dir, "override.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(strings.Join([]string{
		"server:",
		"  host: 127.0.0.1",
		"  port: 9999",
		"database:",
		"  host: 127.0.0.1",
		"  port: 5432",
		"  user: u",
		"  password: p",
		"  name: n",
	}, "\n")), 0o600))

	dbPath := filepath.Join(dir, "migrated.db")

	origArgs := os.Args
	os.Args = []string{"migrate", "-config=" + cfgPath, "-db-driver=sqlite", "-dsn=" + dbPath, "-seed"}
	t.Cleanup(func() { os.Args = origArgs })

	main()

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&models.User{}).Count(&count).Error)
	assert.EqualValues(t, 3, count)
	require.NoError(t, db.Model(&models.KnowledgeDoc{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// TestMigrateSubprocess is executed inside a child process; it runs main()
// with flags provided through SERVIFY_MIGRATE_FLAGS (\x1f separated).
func TestMigrateSubprocess(t *testing.T) {
	if os.Getenv("SERVIFY_MIGRATE_SUBPROCESS") != "1" {
		return
	}
	var args []string
	if f := os.Getenv("SERVIFY_MIGRATE_FLAGS"); f != "" {
		args = strings.Split(f, "\x1f")
	}
	os.Args = append([]string{"migrate"}, args...)
	main()
	os.Exit(0)
}

func runMigrateSubprocess(t *testing.T, dir string, extraEnv []string, flags ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrateSubprocess$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SERVIFY_MIGRATE_SUBPROCESS=1",
		"SERVIFY_MIGRATE_FLAGS="+strings.Join(flags, "\x1f"),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, _ := cmd.CombinedOutput()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return string(out), code
}

func TestMainDefaultConfigLoadFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte("{{{{ not yaml"), 0o600))

	out, code := runMigrateSubprocess(t, dir, nil, "-db-driver=sqlite", "-dsn="+filepath.Join(dir, "x.db"))
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to load config")
}

func TestMainConfigFlagLoadFailure(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "missing.yml")

	out, code := runMigrateSubprocess(t, dir, nil, "-config="+bad, "-db-driver=sqlite", "-dsn="+filepath.Join(dir, "x.db"))
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to load config "+bad)
}

func TestMainSQLiteConnectFailure(t *testing.T) {
	dir := t.TempDir()
	badDSN := filepath.Join(dir, "no-such-dir", "x.db")

	out, code := runMigrateSubprocess(t, dir, nil, "-db-driver=sqlite", "-dsn="+badDSN)
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to connect to database")
}

func TestMainSQLiteEnvDSNConnectFailure(t *testing.T) {
	dir := t.TempDir()
	badDSN := filepath.Join(dir, "no-such-dir", "env.db")

	out, code := runMigrateSubprocess(t, dir, []string{"DB_DRIVER=sqlite", "DB_DSN=" + badDSN})
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to connect to database")
}

func TestMainPostgresDSNConnectFailure(t *testing.T) {
	dir := t.TempDir()

	// Empty -db-port exercises the config-port fallback inside DSN assembly.
	out, code := runMigrateSubprocess(t, dir, nil,
		"-db-driver=postgres",
		"-db-host=127.0.0.1",
		"-db-port=",
		"-db-user=u",
		"-db-pass=p",
		"-db-name=n",
		"-db-sslmode=disable",
		"-db-timezone=UTC",
	)
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to connect to database")
}

func TestMainAutoMigrateFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conflict.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	// A view named `users` makes AutoMigrate fail when it tries to create
	// the table (sqlite reports a name clash with the existing object).
	require.NoError(t, db.Exec("CREATE VIEW users AS SELECT 1 AS id").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	out, code := runMigrateSubprocess(t, dir, nil, "-db-driver=sqlite", "-dsn="+dbPath)
	assert.EqualValues(t, 1, code)
	assert.Contains(t, out, "Failed to migrate database")
}
