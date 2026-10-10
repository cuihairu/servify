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
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("connect admin postgres: %w", err)
	}
	sqlAdmin, err := admin.DB()
	if err != nil {
		return err
	}
	defer sqlAdmin.Close()

	const (
		migratedDB = "servify_parity_migrated"
		automigDB  = "servify_parity_automigrate"
	)
	// Model table set bounds the comparison: the migrated side legitimately
	// carries extra tables (WeKnora compatibility, schema_migrations) and the
	// AutoMigrate side must not create them.
	modelTables := map[string]bool{}
	for _, model := range appbootstrap.MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			return fmt.Errorf("parse model %T: %w", model, err)
		}
		modelTables[s.Table] = true
	}

	for _, name := range []string{migratedDB, automigDB} {
		for _, stmt := range []string{
			fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name),
			fmt.Sprintf(`CREATE DATABASE %s`, name),
		} {
			if err := admin.Exec(stmt).Error; err != nil {
				return fmt.Errorf("%s: %w", stmt, err)
			}
		}
	}

	// Side A: full versioned migration chain.
	migrated, err := openChild(dsn, migratedDB)
	if err != nil {
		return err
	}
	defer closeDB(migrated)
	if err := appbootstrap.RunMigrations(migrated); err != nil {
		return fmt.Errorf("migrate %s: %w", migratedDB, err)
	}

	// Side B: legacy AutoMigrate + CreateIndexes (same config as gen-baseline).
	automig, err := openChild(dsn, automigDB)
	if err != nil {
		return err
	}
	defer closeDB(automig)
	for _, ext := range []string{"vector", "hstore"} {
		if err := automig.Exec("CREATE EXTENSION IF NOT EXISTS " + ext).Error; err != nil {
			return fmt.Errorf("create extension %s: %w", ext, err)
		}
	}
	if err := appbootstrap.AutoMigrate(automig); err != nil {
		return fmt.Errorf("automigrate %s: %w", automigDB, err)
	}
	if err := appbootstrap.CreateIndexes(automig); err != nil {
		return fmt.Errorf("createindexes %s: %w", automigDB, err)
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

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// readColumns loads information_schema.columns for the model tables.
func readColumns(db *gorm.DB, modelTables map[string]bool) (map[columnKey]columnShape, error) {
	rows, err := db.Raw(`
SELECT table_name, column_name, data_type, udt_name,
       character_maximum_length, is_nullable, column_default,
       numeric_precision, numeric_scale
FROM information_schema.columns
WHERE table_schema = 'public'`).Rows()
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

func openChild(adminDSN, name string) (*gorm.DB, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return nil, fmt.Errorf("parse admin dsn: %w", err)
	}
	u.Path = "/" + name
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", name, err)
	}
	return db, nil
}
