package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---- seam helpers (gen-baseline pattern) ----

// openSQLiteAdminSeam points openAdminDB at a sqlite file.
func openSQLiteAdminSeam(t *testing.T) {
	t.Helper()
	orig := openAdminDB
	openAdminDB = func(string) (*gorm.DB, error) {
		return gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "admin.db")), &gorm.Config{})
	}
	t.Cleanup(func() { openAdminDB = orig })
}

// noopScratchSeams no-ops every scratch preparation and schema-application
// step so run() passes through both sides without a postgres.
func noopScratchSeams(t *testing.T) {
	t.Helper()
	origReset, origChain := resetScratchDBs, runChainMigrations
	origAuto, origIdx, origExt := runAutoMigrate, runCreateIndexes, prepareExtensions
	resetScratchDBs = func(*gorm.DB) error { return nil }
	runChainMigrations = func(*gorm.DB) error { return nil }
	runAutoMigrate = func(*gorm.DB) error { return nil }
	runCreateIndexes = func(*gorm.DB) error { return nil }
	prepareExtensions = func(*gorm.DB) error { return nil }
	t.Cleanup(func() {
		resetScratchDBs, runChainMigrations = origReset, origChain
		runAutoMigrate, runCreateIndexes, prepareExtensions = origAuto, origIdx, origExt
	})
}

// sqliteChildSeam routes openChildDB to per-name sqlite files and returns the
// paths eagerly so tests can seed each side before run().
func sqliteChildSeam(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{
		scratchMigratedDB: filepath.Join(dir, scratchMigratedDB+".db"),
		scratchAutomigDB:  filepath.Join(dir, scratchAutomigDB+".db"),
	}
	orig := openChildDB
	openChildDB = func(_, name string) (*gorm.DB, error) {
		return gorm.Open(sqlite.Open(paths[name]), &gorm.Config{})
	}
	t.Cleanup(func() { openChildDB = orig })
	return paths
}

// parityColumnsQuerySeam swaps the metadata query for the sqlite-compatible
// read of the seeded parity_columns table.
func parityColumnsQuerySeam(t *testing.T) {
	t.Helper()
	orig := columnsQuery
	columnsQuery = `SELECT table_name, column_name, data_type, udt_name,
       character_maximum_length, is_nullable, column_default,
       numeric_precision, numeric_scale
FROM parity_columns`
	t.Cleanup(func() { columnsQuery = orig })
}

// seedParityColumns creates the parity_columns table on an existing sqlite
// file and inserts rows (nil = NULL for the nullable fields).
func seedParityColumns(t *testing.T, path string, rows [][]interface{}) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE parity_columns (
	table_name text, column_name text, data_type text, udt_name text,
	character_maximum_length integer, is_nullable text, column_default text,
	numeric_precision integer, numeric_scale integer)`).Error)
	for _, row := range rows {
		require.NoError(t, db.Exec(`INSERT INTO parity_columns VALUES (?,?,?,?,?,?,?,?,?)`, row...).Error)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// shapeRow builds one parity_columns row.
func shapeRow(table, column, dataType, udtName string, charMax interface{}, nullable string) []interface{} {
	return []interface{}{table, column, dataType, udtName, charMax, nullable, nil, nil, nil}
}

// modelColumnRow is a plain text column row for a real model table.
func modelColumnRow(table, column, nullable string) []interface{} {
	return shapeRow(table, column, "text", "text", nil, nullable)
}

// happyPathSeams wires run() to sqlite with no-op schema application; tests
// seed side data or override one seam to reach a specific branch.
func happyPathSeams(t *testing.T) map[string]string {
	t.Helper()
	openSQLiteAdminSeam(t)
	noopScratchSeams(t)
	return sqliteChildSeam(t)
}

// ---- run() happy path ----

// TestRunSQLiteHappyPathZeroDiffs drives run() end to end on sqlite: both
// sides seeded identically, the non-model row filtered, summary green.
func TestRunSQLiteHappyPathZeroDiffs(t *testing.T) {
	paths := happyPathSeams(t)
	parityColumnsQuerySeam(t)

	seedParityColumns(t, paths[scratchMigratedDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
		shapeRow("weknora_docs", "id", "text", "text", nil, "YES"), // non-model: filtered
	})
	seedParityColumns(t, paths[scratchAutomigDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
	})

	var out bytes.Buffer
	require.NoError(t, run("ignored", &out))
	got := out.String()
	assert.Contains(t, got, "compared 1 columns across")
	assert.Contains(t, got, ": 0 diffs (0 allowlisted)")
}

// ---- run() error branches ----

func TestRunMissingDSN(t *testing.T) {
	err := run("   ", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-dsn or SCHEMA_PARITY_DSN is required")
}

func TestRunAdminConnectErrorWrapped(t *testing.T) {
	orig := openAdminDB
	openAdminDB = func(string) (*gorm.DB, error) { return nil, errors.New("dial boom") }
	t.Cleanup(func() { openAdminDB = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect admin postgres: dial boom")
}

// TestRunAdminPoolError covers the admin.DB() error branch: a gorm.DB whose
// Config has no connection pool fails ErrInvalidDB (an empty gorm.DB without
// Config would nil-panic on the promoted ConnPool access instead).
func TestRunAdminPoolError(t *testing.T) {
	orig := openAdminDB
	openAdminDB = func(string) (*gorm.DB, error) {
		return &gorm.DB{Config: &gorm.Config{}}, nil
	}
	t.Cleanup(func() { openAdminDB = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrInvalidDB)
}

func TestRunModelTablesError(t *testing.T) {
	openSQLiteAdminSeam(t)
	orig := loadModelTables
	loadModelTables = func([]interface{}) (map[string]bool, error) { return nil, errors.New("parse boom") }
	t.Cleanup(func() { loadModelTables = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse boom")
}

func TestRunResetScratchError(t *testing.T) {
	openSQLiteAdminSeam(t)
	orig := resetScratchDBs
	resetScratchDBs = func(*gorm.DB) error { return errors.New("reset boom") }
	t.Cleanup(func() { resetScratchDBs = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reset boom")
}

func TestRunOpenMigratedChildError(t *testing.T) {
	openSQLiteAdminSeam(t)
	noopScratchSeams(t)
	orig := openChildDB
	openChildDB = func(string, string) (*gorm.DB, error) { return nil, errors.New("open boom") }
	t.Cleanup(func() { openChildDB = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open boom")
}

// TestRunOpenAutomigChildError fails only the second openChildDB call so the
// side-B branch is covered.
func TestRunOpenAutomigChildError(t *testing.T) {
	openSQLiteAdminSeam(t)
	noopScratchSeams(t)
	calls := 0
	orig := openChildDB
	openChildDB = func(string, string) (*gorm.DB, error) {
		calls++
		if calls >= 2 {
			return nil, errors.New("second open boom")
		}
		return gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "side-a.db")), &gorm.Config{})
	}
	t.Cleanup(func() { openChildDB = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "second open boom")
}

func TestRunChainMigrationsError(t *testing.T) {
	happyPathSeams(t)
	orig := runChainMigrations
	runChainMigrations = func(*gorm.DB) error { return errors.New("chain boom") }
	t.Cleanup(func() { runChainMigrations = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate "+scratchMigratedDB+": chain boom")
}

func TestRunExtensionsError(t *testing.T) {
	happyPathSeams(t)
	orig := prepareExtensions
	prepareExtensions = func(*gorm.DB) error { return errors.New("ext boom") }
	t.Cleanup(func() { prepareExtensions = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ext boom")
}

func TestRunAutoMigrateError(t *testing.T) {
	happyPathSeams(t)
	orig := runAutoMigrate
	runAutoMigrate = func(*gorm.DB) error { return errors.New("auto boom") }
	t.Cleanup(func() { runAutoMigrate = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "automigrate "+scratchAutomigDB+": auto boom")
}

func TestRunCreateIndexesError(t *testing.T) {
	happyPathSeams(t)
	orig := runCreateIndexes
	runCreateIndexes = func(*gorm.DB) error { return errors.New("index boom") }
	t.Cleanup(func() { runCreateIndexes = orig })

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "createindexes "+scratchAutomigDB+": index boom")
}

// TestRunReadColumnsMigratedError: the default information_schema query fails
// on the migrated side's sqlite.
func TestRunReadColumnsMigratedError(t *testing.T) {
	happyPathSeams(t)

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read information_schema.columns")
}

// TestRunReadColumnsAutomigError: side A reads fine, side B's file lacks the
// parity table.
func TestRunReadColumnsAutomigError(t *testing.T) {
	paths := happyPathSeams(t)
	parityColumnsQuerySeam(t)
	seedParityColumns(t, paths[scratchMigratedDB], [][]interface{}{modelColumnRow("api_keys", "id", "NO")})

	err := run("ignored", io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read information_schema.columns")
}

// ---- drift classification ----

func TestRunDriftMissingInAutomigrate(t *testing.T) {
	paths := happyPathSeams(t)
	parityColumnsQuerySeam(t)
	seedParityColumns(t, paths[scratchMigratedDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
		modelColumnRow("api_keys", "extra", "YES"),
	})
	seedParityColumns(t, paths[scratchAutomigDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
	})

	var out bytes.Buffer
	err := run("ignored", &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema parity drift detected: 1 diffs")
	assert.Contains(t, out.String(), "MISSING-IN-AUTOMIGRATE api_keys.extra")
}

func TestRunDriftMissingInMigrated(t *testing.T) {
	paths := happyPathSeams(t)
	parityColumnsQuerySeam(t)
	seedParityColumns(t, paths[scratchMigratedDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
	})
	seedParityColumns(t, paths[scratchAutomigDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
		modelColumnRow("api_keys", "extra", "YES"),
	})

	var out bytes.Buffer
	err := run("ignored", &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema parity drift detected: 1 diffs")
	assert.Contains(t, out.String(), "MISSING-IN-MIGRATED api_keys.extra")
}

// TestRunDriftAllowlistedSkipped: a differing shape on an allowlisted column
// is counted as skipped and the run stays green.
func TestRunDriftAllowlistedSkipped(t *testing.T) {
	paths := happyPathSeams(t)
	parityColumnsQuerySeam(t)
	seedParityColumns(t, paths[scratchMigratedDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
		shapeRow("api_keys", "prefix", "text", "text", nil, "YES"),
	})
	seedParityColumns(t, paths[scratchAutomigDB], [][]interface{}{
		modelColumnRow("api_keys", "id", "NO"),
		shapeRow("api_keys", "prefix", "character varying", "varchar", int64(16), "NO"),
	})

	var out bytes.Buffer
	require.NoError(t, run("ignored", &out))
	assert.Contains(t, out.String(), ": 0 diffs (1 allowlisted)")
}

// TestRunDriftReported: a differing shape outside the allowlist fails.
func TestRunDriftReported(t *testing.T) {
	paths := happyPathSeams(t)
	parityColumnsQuerySeam(t)
	seedParityColumns(t, paths[scratchMigratedDB], [][]interface{}{modelColumnRow("api_keys", "id", "NO")})
	seedParityColumns(t, paths[scratchAutomigDB], [][]interface{}{modelColumnRow("api_keys", "id", "YES")})

	var out bytes.Buffer
	err := run("ignored", &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema parity drift detected: 1 diffs")
	assert.Contains(t, out.String(), "DRIFT api_keys.id\n")
	assert.Contains(t, out.String(), "nullable=NO")
	assert.Contains(t, out.String(), "nullable=YES")
}

// ---- readColumns direct drives ----

func TestReadColumnsScansAndFilters(t *testing.T) {
	parityColumnsQuerySeam(t)
	path := filepath.Join(t.TempDir(), "cols.db")
	seedParityColumns(t, path, [][]interface{}{
		{"api_keys", "id", "integer", "int4", nil, "NO", nil, int64(64), int64(0)},
		shapeRow("weknora_docs", "id", "text", "text", nil, "YES"), // non-model: filtered
	})
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })

	cols, err := readColumns(db, map[string]bool{"api_keys": true})
	require.NoError(t, err)
	require.Len(t, cols, 1)
	shape := cols[columnKey{"api_keys", "id"}]
	assert.Equal(t, "integer", shape.dataType)
	assert.Equal(t, "int4", shape.udtName)
	assert.Equal(t, "NO", shape.nullable)
	assert.False(t, shape.charMax.Valid)
	assert.False(t, shape.def.Valid)
	assert.Equal(t, int64(64), shape.numericPrec.Int64)
	assert.True(t, shape.numericPrec.Valid)
	assert.Equal(t, int64(0), shape.numScale.Int64)
}

// TestReadColumnsDefaultQueryFailsOnSQLite pins the production query's
// behavior outside postgres.
func TestReadColumnsDefaultQueryFailsOnSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cols.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })

	cols, err := readColumns(db, map[string]bool{"api_keys": true})
	require.Error(t, err)
	assert.Nil(t, cols)
	assert.Contains(t, err.Error(), "read information_schema.columns")
}

// TestReadColumnsScanErrorOnNullKey: a NULL table_name fails the string scan.
func TestReadColumnsScanErrorOnNullKey(t *testing.T) {
	parityColumnsQuerySeam(t)
	path := filepath.Join(t.TempDir(), "cols.db")
	seedParityColumns(t, path, [][]interface{}{
		{nil, "id", "text", "text", nil, "NO", nil, nil, nil},
	})
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })

	cols, err := readColumns(db, map[string]bool{"api_keys": true})
	require.Error(t, err)
	assert.Nil(t, cols)
}

// ---- production seam implementations, driven directly ----

// TestOpenPostgresAdminUnreachable drives the default openAdminDB wiring:
// a refused port must fail without a wait.
func TestOpenPostgresAdminUnreachable(t *testing.T) {
	_, err := openAdminDB("postgres://postgres:postgres@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	require.Error(t, err)
}

func TestOpenPostgresChildUnreachable(t *testing.T) {
	_, err := openPostgresChild(
		"postgres://postgres:postgres@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		scratchMigratedDB,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect "+scratchMigratedDB+":")
}

// TestOpenPostgresChildBadDSN covers the childDSN parse-error passthrough.
func TestOpenPostgresChildBadDSN(t *testing.T) {
	_, err := openPostgresChild("://bad", scratchMigratedDB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse admin dsn:")
}

// TestOpenPostgresChildSQLiteSuccess covers the connect-success return by
// injecting the sqlite dialector.
func TestOpenPostgresChildSQLiteSuccess(t *testing.T) {
	orig := postgresDialector
	postgresDialector = func(string) gorm.Dialector {
		return sqlite.Open(filepath.Join(t.TempDir(), "child.db"))
	}
	t.Cleanup(func() { postgresDialector = orig })

	db, err := openPostgresChild("ignored-admin-dsn", scratchAutomigDB)
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })
	require.NoError(t, db.Exec("SELECT 1").Error)
}

// TestDropCreateScratchDBsSQLiteFails: the default DROP DATABASE statements
// fail loudly on sqlite (error message carries the statement).
func TestDropCreateScratchDBsSQLiteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "reset.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })

	err = dropCreateScratchDBs(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DROP DATABASE IF EXISTS "+scratchMigratedDB+" WITH (FORCE)")
}

// TestDropCreateScratchDBsSQLiteSuccess covers the full nested loop and the
// success return with sqlite-executable statements.
func TestDropCreateScratchDBsSQLiteSuccess(t *testing.T) {
	orig := scratchResetStatements
	scratchResetStatements = func(name string) []string {
		return []string{"CREATE TABLE IF NOT EXISTS reset_probe_" + name + " (id integer)"}
	}
	t.Cleanup(func() { scratchResetStatements = orig })

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "reset.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })
	require.NoError(t, dropCreateScratchDBs(db))
}

func TestCreatePostgresExtensionsSQLiteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ext.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })

	err = createPostgresExtensions(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create extension vector")
}

// TestCreatePostgresExtensionsNone covers the empty-list success return, then
// restores the default list and re-checks it still fails on sqlite (wiring).
func TestCreatePostgresExtensionsNone(t *testing.T) {
	orig := postgresExtensions
	postgresExtensions = nil
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "no-ext.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })
	require.NoError(t, createPostgresExtensions(db))

	postgresExtensions = orig
	extErr := createPostgresExtensions(db)
	require.Error(t, extErr)
	assert.Contains(t, extErr.Error(), "create extension vector")
}

// TestSeamDefaultsMatchProductionSeams pins the data-seam defaults so a
// refactor cannot silently drop the production wiring.
func TestSeamDefaultsMatchProductionSeams(t *testing.T) {
	assert.Equal(t,
		[]string{"DROP DATABASE IF EXISTS x WITH (FORCE)", "CREATE DATABASE x"},
		scratchResetStatements("x"))
	assert.Equal(t, []string{"vector", "hstore"}, postgresExtensions)
	assert.Contains(t, columnsQuery, "information_schema.columns")
}

// filterEnv drops the given environment variable so the parent's value cannot
// leak into the subprocess (see also cmd/gen-baseline/main_test.go).
func filterEnv(key string) []string {
	env := os.Environ()[:0]
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, key+"=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}
