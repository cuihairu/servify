// Command gen-baseline regenerates the versioned-migration baseline SQL
// (internal/app/bootstrap/migrations/000001_init.up.sql) by replaying
// bootstrap.MigrationModels + the additional runtime indexes against a
// scratch PostgreSQL instance and capturing the emitted DDL in execution
// order.
//
// GORM's DryRun cannot be used here: the migrator resolves HasTable /
// HasColumn / existing indexes with real queries while it builds DDL, so the
// statements must be captured from an actual run. A capture logger records
// every executed statement; SELECT metadata probes are dropped and only
// CREATE / ALTER / COMMENT statements are written out, in order.
//
// Usage:
//
//	GOTOOLCHAIN=go1.25.7 go -C apps/server run ./cmd/gen-baseline \
//	  -dsn "postgres://postgres:postgres@localhost:55432/servify?sslmode=disable" \
//	  -out internal/app/bootstrap/migrations/000001_init.up.sql
//
// The target database must start EMPTY (fresh pgvector/pgvector:pg15
// container); the generator asserts the schema is empty before running.
// After generation, the draft is hand-finished: IF NOT EXISTS idempotency,
// the legacy AddColumn patches, the non-GORM compatibility tables from
// scripts/init-db.sql, and the ivfflat index. See the header of
// 000001_init.up.sql for the finishing checklist.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// captureLogger records every SQL statement GORM executes, in order.
type captureLogger struct {
	mu      sync.Mutex
	batches []string
}

func (l *captureLogger) LogMode(logger.LogLevel) logger.Interface      { return l }
func (l *captureLogger) Info(context.Context, string, ...interface{})  {}
func (l *captureLogger) Warn(context.Context, string, ...interface{})  {}
func (l *captureLogger) Error(context.Context, string, ...interface{}) {}

func (l *captureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), err error) {
	sqlStr, _ := fc()
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		log.Printf("captured statement FAILED: %s\n  err=%v", sqlStr, err)
	}
	l.batches = append(l.batches, sqlStr)
}

// isDDL reports whether the statement is schema-defining (as opposed to the
// metadata SELECTs the migrator uses while deciding what to change).
func isDDL(stmt string) bool {
	s := strings.TrimSpace(stmt)
	for _, prefix := range []string{"CREATE", "ALTER", "COMMENT", "DROP"} {
		if strings.HasPrefix(strings.ToUpper(s), prefix) {
			return true
		}
	}
	return false
}

func main() {
	dsn := flag.String("dsn", os.Getenv("GEN_BASELINE_DSN"), "scratch postgres DSN (empty database)")
	out := flag.String("out", "", "output file (default stdout)")
	flag.Parse()
	if strings.TrimSpace(*dsn) == "" {
		log.Fatal("-dsn or GEN_BASELINE_DSN is required")
	}

	capture := &captureLogger{}
	db, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{
		Logger:                                   capture,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		log.Fatalf("connect scratch postgres: %v", err)
	}

	// The baseline must be generated against an empty schema, otherwise the
	// capture would miss tables/columns that already exist.
	var existing int
	if err := db.Raw(`SELECT count(*) FROM pg_tables WHERE schemaname = 'public'`).Scan(&existing).Error; err != nil {
		log.Fatalf("probe existing tables: %v", err)
	}
	if existing != 0 {
		log.Fatalf("scratch database is not empty (%d public tables); start from a fresh container", existing)
	}

	// Extensions must exist before the vector(1536) column is created.
	for _, ext := range []string{"vector", "hstore"} {
		if err := db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", ext)).Error; err != nil {
			log.Fatalf("create extension %s: %v", ext, err)
		}
	}

	if err := appbootstrap.AutoMigrate(db); err != nil {
		log.Fatalf("AutoMigrate: %v", err)
	}
	if err := appbootstrap.CreateIndexes(db); err != nil {
		log.Fatalf("CreateIndexes: %v", err)
	}

	capture.mu.Lock()
	statements := make([]string, len(capture.batches))
	copy(statements, capture.batches)
	capture.mu.Unlock()

	var ddl, skipped []string
	for _, stmt := range statements {
		if isDDL(stmt) {
			ddl = append(ddl, strings.TrimRight(strings.TrimSpace(stmt), ";"))
		} else {
			skipped = append(skipped, stmt)
		}
	}

	var b strings.Builder
	for _, stmt := range ddl {
		b.WriteString(stmt)
		b.WriteString(";\n")
	}

	if len(skipped) > 0 {
		log.Printf("captured %d DDL statements, skipped %d non-DDL (metadata probes):", len(ddl), len(skipped))
		seen := map[string]bool{}
		for _, s := range skipped {
			key := s
			if len(key) > 120 {
				key = key[:120] + "..."
			}
			if !seen[key] {
				seen[key] = true
				log.Printf("  SKIP: %s", key)
			}
		}
	}

	if *out == "" {
		fmt.Print(b.String())
		return
	}
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		log.Fatalf("write %s: %v", *out, err)
	}
	log.Printf("wrote %d DDL statements to %s", len(ddl), *out)
}
