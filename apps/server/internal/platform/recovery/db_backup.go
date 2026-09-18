// Package recovery 提供 P2-3"数据恢复、备份与迁移演练"的通用恢复原语：
// sqlite 方言的全库逻辑备份/恢复，以及上传资产目录的归档/恢复/对账。
//
// 定位（docs/backup-and-recovery.md 双轨口径）：本包面向本地开发与
// sqlite 单机部署形态；生产 PostgreSQL 的一致性备份走 pg_dump/pg_restore
// SOP，迁移回滚不提供 down migration——恢复备份是唯一回滚口径
// （见 bootstrap/migrate_runner.go 的 embed 注释）。
package recovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

// manifestVersion 是备份清单格式版本；恢复时校验，未来格式演进时据此拒绝旧备份。
const manifestVersion = 1

// rowScanner 抽象 *sql.Rows 的遍历面：sqlite_master / PRAGMA 查询的
// scan、迭代错误分支在真实 sqlite 上不可达，经 seam 注入以保持
// 防御性错误传播（与 bootstrap 包的迁移 seam 同惯例）。
type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// singleScanner 抽象单行查询（sqlite_sequence 探测、连接级 PRAGMA）的扫描面。
type singleScanner interface {
	Scan(dest ...any) error
}

// 恢复主流程的底层动作 seam：生产实现即直通 gorm / database/sql，
// 对应错误分支在真实 sqlite 上不可达，仅测试注入。
var (
	queryRows = func(db *sql.DB, query string) (rowScanner, error) {
		return db.Query(query)
	}
	probeRow = func(db *sql.DB, query string) (singleScanner, error) {
		return db.QueryRow(query), nil
	}
	getSQLDB = func(db *gorm.DB) (*sql.DB, error) {
		return db.DB()
	}
	jsonMarshalIndent = json.MarshalIndent
	syncFile          = func(file *os.File) error { return file.Sync() }
	rewindFile        = func(file *os.File) error {
		_, err := file.Seek(0, io.SeekStart)
		return err
	}
	connQueryRow = func(ctx context.Context, conn *sql.Conn, query string) (singleScanner, error) {
		return conn.QueryRowContext(ctx, query), nil
	}
	connExec = func(ctx context.Context, conn *sql.Conn, query string, args ...any) (sql.Result, error) {
		return conn.ExecContext(ctx, query, args...)
	}
	txExec = func(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
		return tx.ExecContext(ctx, query, args...)
	}
	beginRestoreTx = func(ctx context.Context, conn *sql.Conn) (*sql.Tx, error) {
		return conn.BeginTx(ctx, nil)
	}
	commitRestoreTx = func(tx *sql.Tx) error {
		return tx.Commit()
	}
)

// excludedTables 是逻辑备份跳过的非业务表：sqlite 内部表按前缀排除，
// schema_migrations 是 golang-migrate 的版本表（sqlite AutoMigrate 路径不存在，
// 防御性排除避免 pg 思维下误把它当业务数据）。
const excludedTablesPredicate = `name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name != 'schema_migrations'`

// TableEntry 是单张表的备份条目：行数据落在 tables/<name>.jsonl，
// 每行一个 JSON 数组（与 Columns 一一对应），sha256 供恢复前完整性校验。
type TableEntry struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    int      `json:"rows"`
	File    string   `json:"file"`
	SHA256  string   `json:"sha256"`
}

// SequenceEntry 记录 sqlite_sequence 的一行：AUTOINCREMENT 表名与其当前值。
// 不恢复它的话，还原库的自增主键会从头重发，撞上已恢复的旧行。
type SequenceEntry struct {
	Name string `json:"name"`
	Seq  int64  `json:"seq"`
}

// DatabaseManifest 是一次数据库备份的清单：恢复前逐文件校验 sha256，
// 演练/验收用它对账关键表行数。
type DatabaseManifest struct {
	Version   int             `json:"version"`
	Dialect   string          `json:"dialect"`
	CreatedAt time.Time       `json:"created_at"`
	Tables    []TableEntry    `json:"tables"`
	TotalRows int             `json:"total_rows"`
	Sequence  []SequenceEntry `json:"sequence,omitempty"`
}

// BackupDatabase 把 db 指向的 sqlite 库全量逻辑备份到 outDir：
// 枚举 sqlite_master 用户表，逐表导出 JSONL 并计算 sha256，
// 最后写 manifest.json。pg 方言不支持——生产备份走 pg_dump SOP。
func BackupDatabase(db *gorm.DB, outDir string) (DatabaseManifest, error) {
	// 方言检查先于连接获取：pg 备份走 pg_dump SOP，不应在误用时建立连接。
	dialect := db.Dialector.Name()
	if dialect != "sqlite" {
		return DatabaseManifest{}, fmt.Errorf("recovery: dialect %q is not supported; use the pg_dump runbook in docs/backup-and-recovery.md", dialect)
	}
	sqlDB, err := getSQLDB(db)
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: get sql.DB: %w", err)
	}

	tables, err := listUserTables(sqlDB)
	if err != nil {
		return DatabaseManifest{}, err
	}
	sequences, err := listSequences(sqlDB)
	if err != nil {
		return DatabaseManifest{}, err
	}

	if err := os.MkdirAll(filepath.Join(outDir, "tables"), 0o755); err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: create backup dir: %w", err)
	}

	manifest := DatabaseManifest{
		Version:   manifestVersion,
		Dialect:   dialect,
		CreatedAt: time.Now().UTC(),
		Sequence:  sequences,
	}
	for _, table := range tables {
		entry, err := dumpTable(sqlDB, outDir, table)
		if err != nil {
			return DatabaseManifest{}, err
		}
		manifest.Tables = append(manifest.Tables, entry)
		manifest.TotalRows += entry.Rows
	}

	raw, err := jsonMarshalIndent(manifest, "", "  ")
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), raw, 0o644); err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: write manifest: %w", err)
	}
	return manifest, nil
}

// RestoreDatabase 从 backupDir 恢复到 db：先逐文件校验 sha256（校验通过
// 的文件保持打开，供回写直接复用），再在单连接事务里清空并回写全部表，
// 最后恢复 sqlite_sequence。返回恢复所用的 manifest 供调用方对账。
func RestoreDatabase(db *gorm.DB, backupDir string) (DatabaseManifest, error) {
	if dialect := db.Dialector.Name(); dialect != "sqlite" {
		return DatabaseManifest{}, fmt.Errorf("recovery: dialect %q is not supported; use the pg_restore runbook in docs/backup-and-recovery.md", dialect)
	}
	sqlDB, err := getSQLDB(db)
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: get sql.DB: %w", err)
	}

	manifest, err := readDatabaseManifest(backupDir)
	if err != nil {
		return DatabaseManifest{}, err
	}
	if manifest.Version != manifestVersion {
		return DatabaseManifest{}, fmt.Errorf("%w: backup manifest version %d (expected %d)", errUnsupportedManifestVersion, manifest.Version, manifestVersion)
	}
	dumps, err := openVerifiedDumps(backupDir, manifest.Tables)
	if err != nil {
		return DatabaseManifest{}, err
	}
	defer func() {
		for _, dump := range dumps {
			_ = dump.file.Close()
		}
	}()

	// PRAGMA 在事务内是 no-op，且连接池可能分发到不同连接：
	// 整个恢复固定在一条专用连接上执行，恢复期间关闭外键约束
	//（回写顺序与外键拓扑无关），结束后恢复原状。
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: grab connection: %w", err)
	}
	defer conn.Close()

	tx, err := beginRestoreTx(ctx, conn)
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// foreign_keys 是连接级 PRAGMA：记录进入时的值，恢复结束后原样设回，
	// 不把状态变化泄漏进连接池。
	var foreignKeysWereOn int
	row, err := connQueryRow(ctx, conn, "PRAGMA foreign_keys")
	if err == nil {
		err = row.Scan(&foreignKeysWereOn)
	}
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: read foreign_keys pragma: %w", err)
	}
	// 注意这里必须赋值给外层 err：deferred 回滚依赖它判断是否需要
	// 回收事务，短声明遮蔽会让失败路径漏掉 Rollback 并卡死 conn.Close。
	_, err = connExec(ctx, conn, "PRAGMA foreign_keys = OFF")
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: disable foreign keys: %w", err)
	}
	defer func() {
		_, _ = connExec(ctx, conn, fmt.Sprintf("PRAGMA foreign_keys = %d", foreignKeysWereOn))
	}()

	for i := range manifest.Tables {
		if err = restoreTable(ctx, tx, dumps[i]); err != nil {
			return DatabaseManifest{}, err
		}
	}
	if err = restoreSequences(ctx, tx, manifest.Sequence); err != nil {
		return DatabaseManifest{}, err
	}
	if err = commitRestoreTx(tx); err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: commit restore: %w", err)
	}
	return manifest, nil
}

// verifiedDump 是通过校验、保持打开的 dump 文件与其条目。
type verifiedDump struct {
	entry TableEntry
	file  *os.File
}

// openVerifiedDumps 逐条目打开 dump 文件并校验 sha256；任一失败则关闭
// 已打开的文件整体返回错误，恢复流程不做半套校验。
func openVerifiedDumps(backupDir string, entries []TableEntry) ([]verifiedDump, error) {
	dumps := make([]verifiedDump, 0, len(entries))
	for _, entry := range entries {
		file, err := openVerifiedFile(filepath.Join(backupDir, entry.File), entry.SHA256)
		if err != nil {
			for _, opened := range dumps {
				_ = opened.file.Close()
			}
			return nil, err
		}
		dumps = append(dumps, verifiedDump{entry: entry, file: file})
	}
	return dumps, nil
}

// openVerifiedFile 打开文件、整体校验 sha256 后回卷到头返回；内容与
// 预期不符时拒绝——恢复的口径是"只回写验证过的备份"。
func openVerifiedFile(path, expected string) (*os.File, error) {
	raw, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("recovery: open %s: %w", filepath.Base(path), err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, raw); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("recovery: hash %s: %w", filepath.Base(path), err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != expected {
		_ = raw.Close()
		return nil, fmt.Errorf("%w: %s (got %s want %s)", errChecksumMismatch, filepath.Base(path), got, expected)
	}
	if err := rewindFile(raw); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("recovery: rewind %s: %w", filepath.Base(path), err)
	}
	return raw, nil
}

// ManifestTable 返回清单中指定表的条目；演练与验收用它对账关键表行数。
func ManifestTable(manifest DatabaseManifest, name string) (TableEntry, bool) {
	for _, entry := range manifest.Tables {
		if entry.Name == name {
			return entry, true
		}
	}
	return TableEntry{}, false
}

func listUserTables(sqlDB *sql.DB) ([]string, error) {
	rows, err := queryRows(sqlDB, "SELECT name FROM sqlite_master WHERE type = 'table' AND "+excludedTablesPredicate+" ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("recovery: list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("recovery: scan table name: %w", err)
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// listSequences 读取 sqlite_sequence（仅当库里有 AUTOINCREMENT 表时才存在）。
func listSequences(sqlDB *sql.DB) ([]SequenceEntry, error) {
	var present int
	row, err := probeRow(sqlDB, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'")
	if err == nil && row != nil {
		err = row.Scan(&present)
	}
	if err != nil {
		return nil, fmt.Errorf("recovery: probe sqlite_sequence: %w", err)
	}
	if present == 0 {
		return nil, nil
	}
	rows, err := queryRows(sqlDB, "SELECT name, seq FROM sqlite_sequence ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("recovery: read sqlite_sequence: %w", err)
	}
	defer rows.Close()

	var sequences []SequenceEntry
	for rows.Next() {
		var entry SequenceEntry
		if err := rows.Scan(&entry.Name, &entry.Seq); err != nil {
			return nil, fmt.Errorf("recovery: scan sqlite_sequence: %w", err)
		}
		sequences = append(sequences, entry)
	}
	return sequences, rows.Err()
}

// restoreSequences 把备份时的自增位置写回 sqlite_sequence。当前库若无
// AUTOINCREMENT 表（sqlite_sequence 不存在）则跳过——与备份时形态不一致
// 属异常，但恢复主流程不应因此失败。探测与回写都走同一事务，事务失效
// 时第一步即失败。
func restoreSequences(ctx context.Context, tx *sql.Tx, sequences []SequenceEntry) error {
	if len(sequences) == 0 {
		return nil
	}
	var present int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'").Scan(&present); err != nil {
		return fmt.Errorf("recovery: probe sqlite_sequence: %w", err)
	}
	if present == 0 {
		return nil
	}
	if _, err := txExec(ctx, tx, "DELETE FROM sqlite_sequence"); err != nil {
		return fmt.Errorf("recovery: clear sqlite_sequence: %w", err)
	}
	for _, entry := range sequences {
		if _, err := txExec(ctx, tx, "INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)", entry.Name, entry.Seq); err != nil {
			return fmt.Errorf("recovery: restore sequence %s: %w", entry.Name, err)
		}
	}
	return nil
}

func dumpTable(sqlDB *sql.DB, outDir, table string) (TableEntry, error) {
	// 列名经 PRAGMA 取得、表名来自 sqlite_master，二者都不是用户输入；
	// 标识符仍用双引号包裹，防御保留字与特殊字符。
	columns, err := tableColumns(sqlDB, table)
	if err != nil {
		return TableEntry{}, err
	}
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = `"` + column + `"`
	}

	rows, err := queryRows(sqlDB, fmt.Sprintf("SELECT %s FROM \"%s\"", strings.Join(quoted, ", "), table))
	if err != nil {
		return TableEntry{}, fmt.Errorf("recovery: read table %s: %w", table, err)
	}
	defer rows.Close()

	file := filepath.Join("tables", table+".jsonl")
	entry := TableEntry{Name: table, Columns: columns, File: file}
	hash := sha256.New()

	out, err := os.Create(filepath.Join(outDir, file))
	if err != nil {
		return TableEntry{}, fmt.Errorf("recovery: create dump file: %w", err)
	}
	defer out.Close()
	writer := io.MultiWriter(out, hash)

	encoder := json.NewEncoder(writer)
	scanValues := make([]any, len(columns))
	for rows.Next() {
		for i := range scanValues {
			scanValues[i] = new(any)
		}
		if err := rows.Scan(scanValues...); err != nil {
			return TableEntry{}, fmt.Errorf("recovery: scan table %s: %w", table, err)
		}
		row := make([]any, len(columns))
		for i, cell := range scanValues {
			row[i] = encodeCell(*(cell.(*any)))
		}
		if err := encoder.Encode(row); err != nil {
			return TableEntry{}, fmt.Errorf("recovery: encode row of %s: %w", table, err)
		}
		entry.Rows++
	}
	if err := rows.Err(); err != nil {
		return TableEntry{}, fmt.Errorf("recovery: iterate table %s: %w", table, err)
	}
	if err := syncFile(out); err != nil {
		return TableEntry{}, fmt.Errorf("recovery: flush dump file: %w", err)
	}
	entry.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return entry, nil
}

func tableColumns(sqlDB *sql.DB, table string) ([]string, error) {
	rows, err := queryRows(sqlDB, fmt.Sprintf("PRAGMA table_info(\"%s\")", table))
	if err != nil {
		return nil, fmt.Errorf("recovery: inspect table %s: %w", table, err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("recovery: scan column of %s: %w", table, err)
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

// encodeCell 统一导出类型：[]byte（BLOB/驱动变体）与 time.Time 转字符串，
// 其余交给 json 原生编码；恢复端按 sqlite 类型亲和写回，无需类型标记。
func encodeCell(cell any) any {
	switch value := cell.(type) {
	case nil:
		return nil
	case []byte:
		return string(value)
	case time.Time:
		return value.Format(time.RFC3339Nano)
	default:
		return value
	}
}

func restoreTable(ctx context.Context, tx *sql.Tx, dump verifiedDump) error {
	entry := dump.entry
	quoted := make([]string, len(entry.Columns))
	placeholders := make([]string, len(entry.Columns))
	for i, column := range entry.Columns {
		quoted[i] = `"` + column + `"`
		placeholders[i] = "?"
	}
	insert := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s)",
		entry.Name, strings.Join(quoted, ", "), strings.Join(placeholders, ", "))

	if _, err := txExec(ctx, tx, fmt.Sprintf("DELETE FROM \"%s\"", entry.Name)); err != nil {
		return fmt.Errorf("recovery: clear table %s: %w", entry.Name, err)
	}

	decoder := json.NewDecoder(dump.file)
	for {
		var row []any
		if err := decoder.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("recovery: decode dump %s: %w", entry.File, err)
		}
		if len(row) != len(entry.Columns) {
			return fmt.Errorf("recovery: dump %s row width %d does not match %d columns", entry.File, len(row), len(entry.Columns))
		}
		if _, err := txExec(ctx, tx, insert, row...); err != nil {
			return fmt.Errorf("recovery: insert into %s: %w", entry.Name, err)
		}
	}
	return nil
}

func readDatabaseManifest(backupDir string) (DatabaseManifest, error) {
	raw, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: read manifest: %w", err)
	}
	var manifest DatabaseManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return DatabaseManifest{}, fmt.Errorf("recovery: parse manifest: %w", err)
	}
	return manifest, nil
}
