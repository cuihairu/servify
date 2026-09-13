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
	"errors"
	"flag"
	"fmt"
	"io"
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

// 测试 seam：run() 经由这三个包级变量访问 postgres 专属的连接、空库探测
// 与扩展创建步骤，默认分别指向下方 *Postgres 实现，生产行为不变。测试注入
// sqlite 实现后即可在无 postgres 的环境下驱动 run() 的全流程。
var (
	openBaselineDB    = openPostgresBaseline
	probePublicTables = probePostgresPublicTables
	prepareExtensions = createPostgresExtensions
)

func openPostgresBaseline(dsn string, cfg *gorm.Config) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), cfg)
}

// probePostgresPublicTables counts tables in the public schema; a scratch
// database must report 0 before generation starts.
func probePostgresPublicTables(db *gorm.DB) (int, error) {
	var existing int
	err := db.Raw(`SELECT count(*) FROM pg_tables WHERE schemaname = 'public'`).Scan(&existing).Error
	return existing, err
}

// baselineExtensions 是 baseline 依赖的 postgres 扩展列表；数据 seam，测试
// 置空以走 createPostgresExtensions 的无扩展成功路径，生产默认不变。
var baselineExtensions = []string{"vector", "hstore"}

// createPostgresExtensions creates the extensions the baseline depends on;
// the vector(1536) column requires them to exist before AutoMigrate.
func createPostgresExtensions(db *gorm.DB) error {
	for _, ext := range baselineExtensions {
		if err := db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", ext)).Error; err != nil {
			return fmt.Errorf("create extension %s: %w", ext, err)
		}
	}
	return nil
}

func main() {
	dsn := flag.String("dsn", os.Getenv("GEN_BASELINE_DSN"), "scratch postgres DSN (empty database)")
	out := flag.String("out", "", "output file (default stdout)")
	flag.Parse()
	if err := run(*dsn, *out, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run 执行 baseline 生成的完整流程。错误沿返回值上抛，由 main 以 log.Fatal
// 终止；错误文案与拆分前 log.Fatalf 的输出逐字一致，退出码同为 1。
func run(dsn, out string, stdout io.Writer) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("-dsn or GEN_BASELINE_DSN is required")
	}

	capture := &captureLogger{}
	db, err := openBaselineDB(dsn, &gorm.Config{
		Logger:                                   capture,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("connect scratch postgres: %w", err)
	}

	// The baseline must be generated against an empty schema, otherwise the
	// capture would miss tables/columns that already exist.
	existing, err := probePublicTables(db)
	if err != nil {
		return fmt.Errorf("probe existing tables: %w", err)
	}
	if existing != 0 {
		return fmt.Errorf("scratch database is not empty (%d public tables); start from a fresh container", existing)
	}

	if err := prepareExtensions(db); err != nil {
		return err
	}

	if err := appbootstrap.AutoMigrate(db); err != nil {
		return fmt.Errorf("AutoMigrate: %w", err)
	}
	if err := appbootstrap.CreateIndexes(db); err != nil {
		return fmt.Errorf("CreateIndexes: %w", err)
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
		summarizeSkipped(skipped)
	}

	if out == "" {
		_, _ = fmt.Fprint(stdout, b.String())
		return nil
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	log.Printf("wrote %d DDL statements to %s", len(ddl), out)
	return nil
}

// summarizeSkipped 打印去重后的非 DDL 语句，超长语句截断到 120 字节加省略号。
func summarizeSkipped(skipped []string) {
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
