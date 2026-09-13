package bootstrap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// ---- applyConnectionPoolSettings 的守卫分支（同包直调）----

// noopConnPool 实现了 gorm.ConnPool 但既不是 *sql.DB 也不提供 GetDBConn，
// 让 gorm.DB() 走到 ErrInvalidDB。
type noopConnPool struct{}

func (noopConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("noop pool")
}

func (noopConnPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("noop pool")
}

func (noopConnPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("noop pool")
}

func (noopConnPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row { return nil }

// invalidConnPoolDB 构造一个 gorm.DB() 必然返回 ErrInvalidDB 的 gorm.DB。
func invalidConnPoolDB() *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{ConnPool: noopConnPool{}}}
}

func TestApplyConnectionPoolSettingsGuards(t *testing.T) {
	// db.DB() 失败：直接返回，不 panic
	applyConnectionPoolSettings(invalidConnPoolDB(), nil)

	// cfg == nil：不做任何池设置
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	applyConnectionPoolSettings(db, nil)
}

// ---- RunMigrations 的错误分支 ----

func TestRunMigrationsGetSQLDBError(t *testing.T) {
	err := RunMigrations(invalidConnPoolDB())
	if err == nil {
		t.Fatal("expected error when db.DB() fails")
	}
	if !strings.Contains(err.Error(), "get sql.DB") {
		t.Fatalf("expected get sql.DB error, got %v", err)
	}
}

func TestRunMigrationsBuildDriverError(t *testing.T) {
	// sqlite 拨通后无法回答 postgres 的 CURRENT_DATABASE()：driver 构建必须失败
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	err = RunMigrations(db)
	if err == nil {
		t.Fatal("expected error building postgres driver against sqlite")
	}
	if !strings.Contains(err.Error(), "build driver") {
		t.Fatalf("expected build driver error, got %v", err)
	}
}

// ---- 假 postgres 驱动：驱动 golang-migrate 全流程（无需真实 PG）----
//
// 只需应答四类查询：CURRENT_DATABASE / CURRENT_SCHEMA / COUNT(1) 与版本行。
// 版本行返回已到顶的版本号时 Up 产生 ErrNoChange（成功路径）；
// 返回 dirty 行时 Up 失败（脏状态 fail-loud 路径）。

type stubMigrateConnector struct {
	versionRow []driver.Value // nil 表示版本表为空（ErrNoRows → NilVersion）
}

type stubMigrateConn struct {
	versionRow []driver.Value
}

func (c *stubMigrateConnector) Connect(context.Context) (driver.Conn, error) {
	return &stubMigrateConn{versionRow: c.versionRow}, nil
}

func (c *stubMigrateConnector) Driver() driver.Driver { return stubMigrateDrv{} }

type stubMigrateDrv struct{}

func (stubMigrateDrv) Open(string) (driver.Conn, error) { return nil, errors.New("not supported") }

func (c *stubMigrateConn) Prepare(query string) (driver.Stmt, error) {
	return &stubMigrateStmt{conn: c, query: query}, nil
}
func (c *stubMigrateConn) Close() error               { return nil }
func (c *stubMigrateConn) Begin() (driver.Tx, error)  { return stubMigrateTx{}, nil }
func (c *stubMigrateConn) Ping(context.Context) error { return nil }

type stubMigrateStmt struct {
	conn  *stubMigrateConn
	query string
}

func (s *stubMigrateStmt) Close() error  { return nil }
func (s *stubMigrateStmt) NumInput() int { return -1 }

func (s *stubMigrateStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

func (s *stubMigrateStmt) Query([]driver.Value) (driver.Rows, error) {
	switch {
	case strings.Contains(s.query, "CURRENT_DATABASE"):
		return newRowSet([]string{"current_database"}, [][]driver.Value{{"servify"}}), nil
	case strings.Contains(s.query, "CURRENT_SCHEMA"):
		return newRowSet([]string{"current_schema"}, [][]driver.Value{{"public"}}), nil
	case strings.Contains(s.query, "COUNT(1)"):
		return newRowSet([]string{"count"}, [][]driver.Value{{int64(1)}}), nil
	case strings.Contains(s.query, "version, dirty"):
		if s.conn.versionRow == nil {
			return newRowSet([]string{"version", "dirty"}, nil), nil
		}
		return newRowSet([]string{"version", "dirty"}, [][]driver.Value{s.conn.versionRow}), nil
	default:
		return newRowSet(nil, nil), nil
	}
}

func newRowSet(cols []string, vals [][]driver.Value) *stubMigrateRows {
	return &stubMigrateRows{cols: cols, vals: vals}
}

type stubMigrateRows struct {
	cols []string
	vals [][]driver.Value
	pos  int
}

func (r *stubMigrateRows) Columns() []string { return r.cols }
func (r *stubMigrateRows) Close() error      { return nil }
func (r *stubMigrateRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.vals) {
		return io.EOF
	}
	copy(dest, r.vals[r.pos])
	r.pos++
	return nil
}

type stubMigrateTx struct{}

func (stubMigrateTx) Commit() error   { return nil }
func (stubMigrateTx) Rollback() error { return nil }

// newStubMigrationsDB 用假驱动的 *sql.DB 作为 gorm 的 ConnPool，
// 让 RunMigrations 走完整的 golang-migrate 流程（RunMigrations 只经由
// db.DB() 访问连接，因此裸 Config 即可，无需 dialector）。
func newStubMigrationsDB(t *testing.T, versionRow []driver.Value) *gorm.DB {
	t.Helper()
	return &gorm.DB{Config: &gorm.Config{ConnPool: sql.OpenDB(&stubMigrateConnector{versionRow: versionRow})}}
}

func TestRunMigrationsUpToDateSucceeds(t *testing.T) {
	// 版本号与最新迁移一致：Up 产出 ErrNoChange，按成功处理
	if err := RunMigrations(newStubMigrationsDB(t, []driver.Value{int64(8), false})); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
}

func TestRunMigrationsDirtyStateFails(t *testing.T) {
	err := RunMigrations(newStubMigrationsDB(t, []driver.Value{int64(5), true}))
	if err == nil {
		t.Fatal("expected dirty migration state to fail loudly")
	}
	if !strings.Contains(err.Error(), "migrations:") {
		t.Fatalf("expected migrations error, got %v", err)
	}
}
