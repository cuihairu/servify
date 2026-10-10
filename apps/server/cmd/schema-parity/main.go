// Command schema-parity proves the versioned-migration chain and the legacy
// GORM AutoMigrate path produce the same column-level schema on a real
// PostgreSQL. Static parity guards (migrations_parity_test.go family) pin
// table/column/index/constraint-name presence but cannot see dialector-level
// behavior — implicit NOT NULL on non-pointer fields, column type mapping —
// this command diffs information_schema.columns across two scratch databases
// (one migrated, one AutoMigrated) and fails on any drift.
//
// Usage (see also the Integration workflow step):
//
//	go -C apps/server run ./cmd/schema-parity \
//	  -dsn "postgres://postgres:postgres@localhost:55433/postgres?sslmode=disable"
//
// The two scratch databases are dropped and recreated on every run; the DSN
// must point at a maintenance database, not at a scratch database itself.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type columnKey struct {
	table, column string
}

type columnShape struct {
	dataType    string
	udtName     string
	charMax     sql.NullInt64
	nullable    string
	def         sql.NullString
	numericPrec sql.NullInt64
	numScale    sql.NullInt64
}

func (c columnShape) differ(b columnShape) bool {
	return c.dataType != b.dataType || c.udtName != b.udtName ||
		c.charMax != b.charMax || c.nullable != b.nullable || c.def != b.def ||
		c.numericPrec != b.numericPrec || c.numScale != b.numScale
}

func (c columnShape) describe() string {
	return fmt.Sprintf("type=%s udt=%s charMax=%v nullable=%s default=%v numPrec=%v numScale=%v",
		c.dataType, c.udtName, c.charMax, c.nullable, c.def, c.numericPrec, c.numScale)
}

// allowlist holds drifts that were evaluated and accepted (2026-10-10 parity
// audit, 28 entries). Each entry is a deliberate decision, not an oversight:
//
//   - "hand-written migration is looser than the gorm size tag" (text vs
//     varchar(N)): tightening the migration would ALTER production columns and
//     fail on over-limit legacy rows; dropping the size tag would discard the
//     declared intent. Only the legacy postgres AutoMigrate escape hatch is
//     affected; production (versioned) and dev/test (sqlite) already agree.
//   - "hand-written migration narrowed int" (integer vs bigint): counts/
//     durations fit integer; production columns stay integer.
//   - "hand-written migration added DEFAULT the model never declared": GORM
//     always sends explicit values for non-default-tag fields, so the DEFAULT
//     is inert on both paths; adding the tag would change GORM zero-value
//     insert behavior (zero values would be skipped and DB-filled), and
//     dropping the DEFAULT would churn production for no behavioral gain.
//
// New drifts outside this list fail the run. Revisit entries when a column is
// touched anyway (see also migrations_notnull_parity_test.go for the static
// side of the same discipline).
var allowlist = map[columnKey]string{
	{"api_keys", "prefix"}:                               "text vs varchar(16): migration looser than size tag",
	{"api_keys", "key_hash"}:                             "text vs varchar(64): migration looser than size tag",
	{"user_recovery_codes", "code_hash"}:                 "text vs varchar(64): migration looser than size tag",
	{"audit_logs", "prev_hash"}:                          "text vs varchar(64): migration looser than size tag",
	{"audit_logs", "entry_hash"}:                         "text vs varchar(64): migration looser than size tag",
	{"suggestion_exposure_logs", "kind"}:                 "text vs varchar(16): migration looser than size tag",
	{"suggestion_exposure_logs", "strategy"}:             "text vs varchar(64): migration looser than size tag",
	{"suggestion_exposure_logs", "converted_question"}:   "text vs varchar(512): migration looser than size tag",
	{"push_tokens", "platform"}:                          "text vs varchar(16): migration looser than size tag",
	{"agent_groups", "priority"}:                         "integer vs bigint: migration narrowed int",
	{"quality_reviews", "duration_seconds"}:              "integer vs bigint: migration narrowed int",
	{"quality_reviews", "message_count"}:                 "integer vs bigint: migration narrowed int",
	{"quality_reviews", "violation_count"}:               "integer vs bigint: migration narrowed int",
	{"quality_reviews", "attempt_count"}:                 "integer vs bigint + inert DEFAULT 0",
	{"quality_reviews", "llm_total_score"}:               "double precision vs numeric: float mapping wording",
	{"quality_reviews", "manual_score"}:                  "double precision vs numeric: float mapping wording",
	{"push_tokens", "tenant_id"}:                         "inert DEFAULT '' the model never declared",
	{"push_tokens", "workspace_id"}:                      "inert DEFAULT '' the model never declared",
	{"translation_language_preferences", "tenant_id"}:    "inert DEFAULT '' the model never declared",
	{"translation_language_preferences", "workspace_id"}: "inert DEFAULT '' the model never declared",
	{"workspace_configs", "rag_flow_json"}:               "inert DEFAULT '' the model never declared",
	{"tenant_configs", "rag_flow_json"}:                  "inert DEFAULT '' the model never declared",
	{"agent_groups", "enabled"}:                          "inert DEFAULT true the model never declared",
	{"agent_groups", "overflow_policy"}:                  "inert DEFAULT 'global' the model never declared",
	{"remote_assist_sessions", "status"}:                 "inert DEFAULT 'active' the model never declared",
	{"remote_assist_sessions", "recording_duration_ms"}:  "inert DEFAULT 0 the model never declared",
	{"remote_assist_sessions", "recording_size"}:         "inert DEFAULT 0 the model never declared",
	{"remote_assist_annotations", "timestamp_ms"}:        "inert DEFAULT 0 the model never declared",
}

// Names of the two scratch databases; openChildDB rewrites the admin DSN path
// onto these.
const (
	scratchMigratedDB = "servify_parity_migrated"
	scratchAutomigDB  = "servify_parity_automigrate"
)

// Test seams: run() reaches every postgres-specific step through these
// package variables; the defaults below are the production implementations.
// Tests inject sqlite/noop/fake implementations to drive run() end to end
// (including every error branch) without a postgres server — same pattern as
// cmd/gen-baseline.
var (
	// openAdminDB connects to the maintenance database.
	openAdminDB = openPostgresAdmin
	// resetScratchDBs drops and recreates both scratch databases.
	resetScratchDBs = dropCreateScratchDBs
	// openChildDB opens one scratch database.
	openChildDB = openPostgresChild
	// prepareExtensions creates the extensions the AutoMigrate side needs.
	prepareExtensions = createPostgresExtensions
	// Schema application for both sides; defaults are the appbootstrap
	// implementations (their bodies are covered in the bootstrap package).
	runChainMigrations = appbootstrap.RunMigrations
	runAutoMigrate     = appbootstrap.AutoMigrate
	runCreateIndexes   = appbootstrap.CreateIndexes
	// loadModelTables resolves MigrationModels() to their table-name set.
	loadModelTables = modelTableSet
	// postgresDialector builds the dialector for scratch connections; tests
	// inject sqlite to drive the connect-success path.
	postgresDialector = func(dsn string) gorm.Dialector { return postgres.Open(dsn) }
)

// scratchResetStatements returns the statements executed against the admin
// connection per scratch database; tests replace them with sqlite-executable
// statements (a func seam keeps the format strings constant for vet).
var scratchResetStatements = func(name string) []string {
	return []string{
		fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name),
		fmt.Sprintf(`CREATE DATABASE %s`, name),
	}
}

// postgresExtensions are created on the AutoMigrate side before AutoMigrate;
// data seam, tests may empty it to cover the no-extension success path.
var postgresExtensions = []string{"vector", "hstore"}

const postgresColumnsQuery = `
SELECT table_name, column_name, data_type, udt_name,
       character_maximum_length, is_nullable, column_default,
       numeric_precision, numeric_scale
FROM information_schema.columns
WHERE table_schema = 'public'`

// columnsQuery is the metadata query readColumns executes; data seam, tests
// swap in a sqlite-compatible query against a seeded table.
var columnsQuery = postgresColumnsQuery

func main() {
	dsn := flag.String("dsn", os.Getenv("SCHEMA_PARITY_DSN"), "maintenance postgres DSN (scratch databases are recreated by this tool)")
	flag.Parse()
	if err := run(*dsn, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "schema-parity: "+err.Error())
		os.Exit(1)
	}
}

func run(dsn string, out io.Writer) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("-dsn or SCHEMA_PARITY_DSN is required")
	}
	admin, err := openAdminDB(dsn)
	if err != nil {
		return fmt.Errorf("connect admin postgres: %w", err)
	}
	sqlAdmin, err := admin.DB()
	if err != nil {
		return err
	}
	defer sqlAdmin.Close()

	// Model table set bounds the comparison: the migrated side legitimately
	// carries extra tables (WeKnora compatibility, schema_migrations) and the
	// AutoMigrate side must not create them.
	modelTables, err := loadModelTables(appbootstrap.MigrationModels())
	if err != nil {
		return err
	}

	if err := resetScratchDBs(admin); err != nil {
		return err
	}

	// Side A: full versioned migration chain.
	migrated, err := openChildDB(dsn, scratchMigratedDB)
	if err != nil {
		return err
	}
	defer closeDB(migrated)
	if err := runChainMigrations(migrated); err != nil {
		return fmt.Errorf("migrate %s: %w", scratchMigratedDB, err)
	}

	// Side B: legacy AutoMigrate + CreateIndexes (same config as gen-baseline).
	automig, err := openChildDB(dsn, scratchAutomigDB)
	if err != nil {
		return err
	}
	defer closeDB(automig)
	if err := prepareExtensions(automig); err != nil {
		return err
	}
	if err := runAutoMigrate(automig); err != nil {
		return fmt.Errorf("automigrate %s: %w", scratchAutomigDB, err)
	}
	if err := runCreateIndexes(automig); err != nil {
		return fmt.Errorf("createindexes %s: %w", scratchAutomigDB, err)
	}

	migratedCols, err := readColumns(migrated, modelTables)
	if err != nil {
		return err
	}
	automigCols, err := readColumns(automig, modelTables)
	if err != nil {
		return err
	}

	diffs, skipped := 0, 0
	for key, a := range migratedCols {
		b, ok := automigCols[key]
		if !ok {
			diffs++
			fmt.Fprintf(out, "MISSING-IN-AUTOMIGRATE %s.%s migrated: %s\n", key.table, key.column, a.describe())
			continue
		}
		if a.differ(b) {
			if _, allowed := allowlist[key]; allowed {
				skipped++
				continue
			}
			diffs++
			fmt.Fprintf(out, "DRIFT %s.%s\n  migrated:    %s\n  automigrate: %s\n", key.table, key.column, a.describe(), b.describe())
		}
	}
	for key, b := range automigCols {
		if _, ok := migratedCols[key]; !ok {
			diffs++
			fmt.Fprintf(out, "MISSING-IN-MIGRATED %s.%s automigrate: %s\n", key.table, key.column, b.describe())
		}
	}
	fmt.Fprintf(out, "compared %d columns across %d model tables: %d diffs (%d allowlisted)\n", len(migratedCols), len(modelTables), diffs, skipped)
	if diffs > 0 {
		return fmt.Errorf("schema parity drift detected: %d diffs", diffs)
	}
	return nil
}

// modelTableSet resolves the table name of every model via GORM's schema
// parser (same naming strategy as the migrators).
func modelTableSet(models []interface{}) (map[string]bool, error) {
	modelTables := map[string]bool{}
	for _, model := range models {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			return nil, fmt.Errorf("parse model %T: %w", model, err)
		}
		modelTables[s.Table] = true
	}
	return modelTables, nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// readColumns loads information_schema.columns for the model tables.
func readColumns(db *gorm.DB, modelTables map[string]bool) (map[columnKey]columnShape, error) {
	rows, err := db.Raw(columnsQuery).Rows()
	if err != nil {
		return nil, fmt.Errorf("read information_schema.columns: %w", err)
	}
	defer rows.Close()
	cols := map[columnKey]columnShape{}
	for rows.Next() {
		var t, c string
		var shape columnShape
		if err := rows.Scan(&t, &c, &shape.dataType, &shape.udtName, &shape.charMax, &shape.nullable, &shape.def, &shape.numericPrec, &shape.numScale); err != nil {
			return nil, err
		}
		if !modelTables[t] {
			continue
		}
		cols[columnKey{t, c}] = shape
	}
	return cols, rows.Err()
}

// childDSN rewrites the admin DSN path onto the scratch database name.
func childDSN(adminDSN, name string) (string, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", fmt.Errorf("parse admin dsn: %w", err)
	}
	u.Path = "/" + name
	return u.String(), nil
}

func openPostgresAdmin(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
}

// dropCreateScratchDBs recreates both scratch databases from scratch so the
// run always starts from a clean slate.
func dropCreateScratchDBs(admin *gorm.DB) error {
	for _, name := range []string{scratchMigratedDB, scratchAutomigDB} {
		for _, stmt := range scratchResetStatements(name) {
			if err := admin.Exec(stmt).Error; err != nil {
				return fmt.Errorf("%s: %w", stmt, err)
			}
		}
	}
	return nil
}

func openPostgresChild(adminDSN, name string) (*gorm.DB, error) {
	dsn, err := childDSN(adminDSN, name)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(postgresDialector(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", name, err)
	}
	return db, nil
}

// createPostgresExtensions creates the extensions the AutoMigrate side
// depends on (the vector column type requires them to exist).
func createPostgresExtensions(db *gorm.DB) error {
	for _, ext := range postgresExtensions {
		if err := db.Exec("CREATE EXTENSION IF NOT EXISTS " + ext).Error; err != nil {
			return fmt.Errorf("create extension %s: %w", ext, err)
		}
	}
	return nil
}
