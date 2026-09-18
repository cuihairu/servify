package bootstrap

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"strings"

	mgpostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/driver/postgres"

	"github.com/golang-migrate/migrate/v4"
	"gorm.io/gorm"
)

// migrationsFS embeds the versioned SQL migrations applied to PostgreSQL
// databases. Files follow the golang-migrate naming convention
// NNNNNN_name.up.sql; down migrations are intentionally not shipped — the
// baseline is not reversible, rollbacks restore from backup.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// SchemaManagementMode selects how a boot manages the database schema.
type SchemaManagementMode int

const (
	// SchemaModeSkip leaves the schema untouched (DBA-managed deployments).
	SchemaModeSkip SchemaManagementMode = iota
	// SchemaModeVersioned applies the embedded versioned SQL migrations.
	SchemaModeVersioned
	// SchemaModeAutoMigrate uses the legacy GORM AutoMigrate (sqlite, or the
	// SERVIFY_AUTO_MIGRATE escape hatch on PostgreSQL).
	SchemaModeAutoMigrate
)

// MigrationsEnabled reports whether automatic schema management should run at
// all. Setting MIGRATIONS_ENABLED to a falsy value ("0", "false", "no", "off")
// hands the schema over to external management; the default is enabled.
func MigrationsEnabled() bool {
	return !falsyEnv("MIGRATIONS_ENABLED")
}

// AutoMigrateRequested reports whether the legacy GORM AutoMigrate escape
// hatch was requested on PostgreSQL. SERVIFY_AUTO_MIGRATE used to default to
// on; versioned migrations are now the default and the variable flips to an
// explicit opt-in.
func AutoMigrateRequested() bool {
	return truthyEnv("SERVIFY_AUTO_MIGRATE")
}

// ResolveSchemaMode decides how the given driver should get its schema.
// PostgreSQL runs versioned migrations by default; sqlite keeps AutoMigrate
// (the SQL baseline is postgres-dialect). MIGRATIONS_ENABLED=off wins over
// everything, and SERVIFY_AUTO_MIGRATE=on opts back into the legacy path.
func ResolveSchemaMode(driver string) SchemaManagementMode {
	if !MigrationsEnabled() {
		return SchemaModeSkip
	}
	if normalizedDatabaseDriver(DatabaseOptions{Driver: driver}) != "postgres" {
		return SchemaModeAutoMigrate
	}
	if AutoMigrateRequested() {
		return SchemaModeAutoMigrate
	}
	return SchemaModeVersioned
}

// UsesVersionedMigrations reports whether the driver dialect is managed by
// the versioned SQL migrations. Only postgres qualifies; sqlite keeps the
// legacy AutoMigrate path (the baseline is postgres-dialect SQL).
func UsesVersionedMigrations(driver string) bool {
	return normalizedDatabaseDriver(DatabaseOptions{Driver: driver}) == "postgres"
}

// newMigrationsSource / newSchemaMigrator 是包级 seam（默认即生产实现）。
// 嵌入式 FS 编译期保证有效、两处构造参数恒非空，生产路径这两个错误分支
// 不可达，仅测试注入以保持防御性错误传播。
var (
	newMigrationsSource = iofs.New
	newSchemaMigrator   = migrate.NewWithInstance
)

// RunMigrations applies the embedded versioned migrations to the database
// behind db. It is idempotent: an up-to-date database yields migrate.ErrNoChange,
// which is treated as success. A dirty migration state fails loudly so a
// broken rollout is never silently ignored.
//
// 迁移在专用连接上执行：golang-migrate 的 postgres driver.Close() 会连带关闭
// 传入的 *sql.DB——若直接传主连接池，迁移一结束服务主池就被关掉（表现为
// "sql: database is closed"）。专用连接从 dialector 里的 DSN 重建，跑完即弃；
// dialector 不携带 DSN 时（测试桩/非 postgres 方言）保持直接使用 db 的旧路径。
func RunMigrations(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("migrations: get sql.DB: %w", err)
	}
	target := sqlDB
	if dsn := postgresDSNFromDialector(db); dsn != "" {
		dedicated, err := openDedicatedMigrationsConn(dsn)
		if err != nil {
			return fmt.Errorf("migrations: open dedicated connection: %w", err)
		}
		defer dedicated.Close()
		target = dedicated
	}
	driver, err := mgpostgres.WithInstance(target, &mgpostgres.Config{})
	if err != nil {
		return fmt.Errorf("migrations: build driver: %w", err)
	}
	defer driver.Close()

	src, err := newMigrationsSource(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations: load embedded source: %w", err)
	}
	m, err := newSchemaMigrator("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrations: build migrator: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}

// postgresDSNFromDialector 从 gorm postgres dialector 提取原始 DSN；方言不
// 匹配时返回空（调用方回退到直接使用传入的连接池）。
func postgresDSNFromDialector(db *gorm.DB) string {
	if db == nil || db.Config == nil || db.Config.Dialector == nil {
		return ""
	}
	if pg, ok := db.Config.Dialector.(*postgres.Dialector); ok {
		return pg.DSN
	}
	return ""
}

// openDedicatedMigrationsConn 是 seam（默认即生产实现），测试注入以覆盖
// 专用连接的错误分支而不依赖真实 PG。DisableAutomaticPing 跳过 Open 时的
// 探活——紧随其后的迁移首条 SQL 自会建立连接，连不上会在 migrate.Up 报错。
var openDedicatedMigrationsConn = func(dsn string) (*sql.DB, error) {
	dedicated, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		return nil, err
	}
	return dedicated.DB()
}

func truthyEnv(key string) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func falsyEnv(key string) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}
