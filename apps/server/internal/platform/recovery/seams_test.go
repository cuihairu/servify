package recovery

// seams_test.go 注入包内 seam 的失败实现，覆盖真实 sqlite/文件系统上
// 不可达的防御性错误分支（生产路径不改行为，仅测试注入，同
// bootstrap 包迁移 seam 的惯例）。每个注入用例在清理回调里还原 seam。

import (
	"archive/tar"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var errInjected = errors.New("injected failure")

// stubFile 以可控错误实现 readSeekCloser / io.WriteCloser：Read 在无
// 注入错误时返回 EOF（对 io.Copy 表现为空文件）；Write 前 failAfter 次
// 成功，用于让 tar 写头与写体阶段分别失败；data 提供读出的内容。
type stubFile struct {
	data      []byte
	readErr   error
	seekErr   error
	writeErr  error
	closeErr  error
	failAfter int
	reads     int
	writes    int
}

func (f *stubFile) Read([]byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	if f.reads >= len(f.data) {
		return 0, io.EOF
	}
	n := len(f.data) - f.reads
	f.reads = len(f.data)
	return n, nil
}

func (f *stubFile) Write(p []byte) (int, error) {
	f.writes++
	if f.writeErr != nil && f.writes > f.failAfter {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *stubFile) Seek(offset int64, whence int) (int64, error) {
	if f.seekErr != nil {
		return 0, f.seekErr
	}
	// 归档流程 Seek(0) 回卷后重读；stub 只支持回卷到头。
	if offset == 0 && whence == io.SeekStart {
		f.reads = 0
	}
	return 0, nil
}

func (f *stubFile) Close() error { return f.closeErr }

// stubRows 以可控错误实现 rowScanner 遍历面。
type stubRows struct {
	scans    []func(dest ...any) error
	finalErr error
}

func (r *stubRows) Next() bool { return len(r.scans) > 0 }

func (r *stubRows) Scan(dest ...any) error {
	fn := r.scans[0]
	r.scans = r.scans[1:]
	return fn(dest...)
}

func (r *stubRows) Err() error { return r.finalErr }

func (r *stubRows) Close() error { return nil }

func swapFileOpen(t *testing.T, stub *stubFile, openErr error) {
	t.Helper()
	original := fileOpen
	fileOpen = func(string) (readSeekCloser, error) {
		if openErr != nil {
			return nil, openErr
		}
		return stub, nil
	}
	t.Cleanup(func() { fileOpen = original })
}

func swapFileCreate(t *testing.T, stub *stubFile, createErr error) {
	t.Helper()
	original := fileCreate
	fileCreate = func(string) (io.WriteCloser, error) {
		if createErr != nil {
			return nil, createErr
		}
		return stub, nil
	}
	t.Cleanup(func() { fileCreate = original })
}

func swapQueryRows(t *testing.T, rows rowScanner, queryErr error) {
	t.Helper()
	original := queryRows
	queryRows = func(*sql.DB, string) (rowScanner, error) {
		return rows, queryErr
	}
	t.Cleanup(func() { queryRows = original })
}

// swapQueryRowsFunc 按 query 内容分发注入：同一次 Backup 流程会经过
// sqlite_master / PRAGMA / 业务表 SELECT 三类查询，各用例只让目标类失败。
func swapQueryRowsFunc(t *testing.T, dispatch func(query string) (rowScanner, error)) {
	t.Helper()
	original := queryRows
	queryRows = func(_ *sql.DB, query string) (rowScanner, error) {
		return dispatch(query)
	}
	t.Cleanup(func() { queryRows = original })
}

func swapProbeRow(t *testing.T, scanner singleScanner, queryErr error) {
	t.Helper()
	original := probeRow
	probeRow = func(*sql.DB, string) (singleScanner, error) {
		return scanner, queryErr
	}
	t.Cleanup(func() { probeRow = original })
}

func scanErrRow() func(dest ...any) error {
	return func(dest ...any) error { return errInjected }
}

// stubFileInfo 提供手控的 os.FileInfo。
type stubFileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (f stubFileInfo) Name() string       { return f.name }
func (f stubFileInfo) Size() int64        { return f.size }
func (f stubFileInfo) Mode() os.FileMode  { return f.mode }
func (f stubFileInfo) ModTime() time.Time { return time.Time{} }
func (f stubFileInfo) IsDir() bool        { return false }
func (f stubFileInfo) Sys() any           { return nil }

// TestAppendArchiveEntryErrors 逐项覆盖归档单文件动作的错误分支：
// open/hash 读/seek/写头/写体/关闭六处，均经 seam 或 failAfter 精确触发。
func TestAppendArchiveEntryErrors(t *testing.T) {
	info := stubFileInfo{name: "a.txt", size: 5, mode: 0o644}

	cases := []struct {
		name string
		open func(*testing.T)
	}{
		{"open error", func(t *testing.T) { swapFileOpen(t, nil, errInjected) }},
		{"hash read error", func(t *testing.T) { swapFileOpen(t, &stubFile{readErr: errInjected}, nil) }},
		{"seek error", func(t *testing.T) {
			// data 非空让 hash 拷贝先成功，失败落在 Seek。
			swapFileOpen(t, &stubFile{data: []byte("hello"), seekErr: errInjected}, nil)
		}},
		{"close error", func(t *testing.T) { swapFileOpen(t, &stubFile{closeErr: errInjected}, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.open(t)
			_, err := appendArchiveEntry(tar.NewWriter(&stubFile{}), "/root", "/root/a.txt", info)
			assert.ErrorIs(t, err, errInjected)
		})
	}

	t.Run("write header error", func(t *testing.T) {
		swapFileOpen(t, &stubFile{}, nil)
		// go1.25 tar.Writer 的 WriteHeader 会向底层发 3 次 Write
		//（两次 0 字节 + 一次 512 字节头块），failAfter=0 时第一次即失败。
		_, err := appendArchiveEntry(tar.NewWriter(&stubFile{writeErr: errInjected}), "/root", "/root/a.txt", info)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("body copy error", func(t *testing.T) {
		// raw 带数据：写体阶段才有内容可写。WriteHeader 占前 3 次 Write，
		// 第 4 次（文件体直通写入）失败，错误落在 body copy 分支。
		swapFileOpen(t, &stubFile{data: []byte("hello")}, nil)
		_, err := appendArchiveEntry(tar.NewWriter(&stubFile{writeErr: errInjected, failAfter: 3}),
			"/root", "/root/a.txt", info)
		assert.ErrorIs(t, err, errInjected)
	})
}

// TestBackupFilesCreateAndCloseErrors 覆盖归档创建与关闭失败分支。
func TestBackupFilesCreateAndCloseErrors(t *testing.T) {
	root := t.TempDir()

	t.Run("create archive error", func(t *testing.T) {
		swapFileCreate(t, nil, errInjected)
		_, err := BackupFiles(root, filepath.Join(root, "out.tar.gz"))
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("writer close error", func(t *testing.T) {
		// 空目录：循环不执行，tar 关闭写 footer 时 flush 到坏 writer 失败。
		empty := filepath.Join(root, "empty")
		require.NoError(t, os.MkdirAll(empty, 0o755))
		swapFileCreate(t, &stubFile{writeErr: errInjected}, nil)
		_, err := BackupFiles(empty, filepath.Join(root, "out.tar.gz"))
		assert.ErrorIs(t, err, errInjected)
	})
}

// TestHashFileReadError 覆盖对账读取失败分支。
func TestHashFileReadError(t *testing.T) {
	swapFileOpen(t, &stubFile{readErr: errInjected}, nil)

	_, _, err := hashFile("whatever")
	assert.ErrorIs(t, err, errInjected)
}

// errReader 读取即失败。
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// TestExtractVerifiedFileErrors 覆盖落盘阶段的创建/拷贝/关闭错误。
func TestExtractVerifiedFileErrors(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "x")

	swapFileCreate(t, nil, errInjected)
	err := extractVerifiedFile(trailingEOFReader{}, target, FileEntry{})
	assert.ErrorIs(t, err, errInjected)

	swapFileCreate(t, &stubFile{writeErr: errInjected}, nil)
	err = extractVerifiedFile(errReader{err: errInjected}, target, FileEntry{})
	assert.ErrorIs(t, err, errInjected)

	// 读端立即 EOF：拷贝与校验输入为空，失败精确落在 Close。
	swapFileCreate(t, &stubFile{closeErr: errInjected}, nil)
	err = extractVerifiedFile(trailingEOFReader{}, target, FileEntry{})
	assert.ErrorIs(t, err, errInjected)
}

// trailingEOFReader 是立即 EOF 的 io.Reader。
type trailingEOFReader struct{}

func (trailingEOFReader) Read([]byte) (int, error) { return 0, io.EOF }

// TestQueryRowSeams 覆盖 sqlite_master / PRAGMA 遍历的扫描与迭代错误分支。
func TestQueryRowSeams(t *testing.T) {
	db := openRawSQLite(t, filepath.Join(t.TempDir(), "seams.db"))
	// 自增表让 sqlite_sequence 存在，listSequences 才会走到 queryRows seam。
	_, err := db.Exec("CREATE TABLE auto_t (id INTEGER PRIMARY KEY AUTOINCREMENT)")
	require.NoError(t, err)

	t.Run("listUserTables query error", func(t *testing.T) {
		swapQueryRows(t, nil, errInjected)
		_, err := listUserTables(db)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("listUserTables scan error", func(t *testing.T) {
		swapQueryRows(t, &stubRows{scans: []func(dest ...any) error{scanErrRow()}}, nil)
		_, err := listUserTables(db)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("listUserTables rows error", func(t *testing.T) {
		swapQueryRows(t, &stubRows{finalErr: errInjected}, nil)
		_, err := listUserTables(db)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("listSequences query error", func(t *testing.T) {
		swapQueryRows(t, nil, errInjected)
		_, err := listSequences(db)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("listSequences scan error", func(t *testing.T) {
		swapQueryRows(t, &stubRows{scans: []func(dest ...any) error{scanErrRow()}}, nil)
		_, err := listSequences(db)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("listSequences rows error", func(t *testing.T) {
		swapQueryRows(t, &stubRows{finalErr: errInjected}, nil)
		_, err := listSequences(db)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("tableColumns query error", func(t *testing.T) {
		swapQueryRows(t, nil, errInjected)
		_, err := tableColumns(db, "t")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("tableColumns scan error", func(t *testing.T) {
		swapQueryRows(t, &stubRows{scans: []func(dest ...any) error{scanErrRow()}}, nil)
		_, err := tableColumns(db, "t")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("tableColumns rows error", func(t *testing.T) {
		swapQueryRows(t, &stubRows{finalErr: errInjected}, nil)
		_, err := tableColumns(db, "t")
		assert.ErrorIs(t, err, errInjected)
	})
}

// TestRestoreTxSeams 覆盖恢复事务的开启/提交失败分支。
func TestRestoreTxSeams(t *testing.T) {
	db := openGormSQLite(t, filepath.Join(t.TempDir(), "tx-seam.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
	backupDir := t.TempDir()

	manifest := DatabaseManifest{Version: manifestVersion, Dialect: "sqlite", Tables: []TableEntry{
		{Name: "t", Columns: []string{"id", "name"}, Rows: 1,
			File: "tables/t.jsonl", SHA256: writeSeamDump(t, backupDir, "[1, \"a\"]\n")},
	}}
	writeManifestFile(t, backupDir, manifest)

	t.Run("begin tx error", func(t *testing.T) {
		original := beginRestoreTx
		beginRestoreTx = func(context.Context, *sql.Conn) (*sql.Tx, error) { return nil, errInjected }
		t.Cleanup(func() { beginRestoreTx = original })

		_, err := RestoreDatabase(db, backupDir)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("commit tx error", func(t *testing.T) {
		original := commitRestoreTx
		commitRestoreTx = func(*sql.Tx) error { return errInjected }
		t.Cleanup(func() { commitRestoreTx = original })

		_, err := RestoreDatabase(db, backupDir)
		assert.ErrorIs(t, err, errInjected)
	})
}

// TestRestoreSequencesError 覆盖序列恢复阶段的错误分支：已回滚的失效
// 事务上探测第一步即失败。
func TestRestoreSequencesError(t *testing.T) {
	db := openRawSQLite(t, filepath.Join(t.TempDir(), "seq.db"))
	_, err := db.Exec("CREATE TABLE auto_t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)")
	require.NoError(t, err)

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	err = restoreSequences(ctx, tx, []SequenceEntry{{Name: "auto_t", Seq: 1}})
	assert.Error(t, err)
}

// writeSeamDump 写一个最小 dump 文件并返回其 sha256。
func writeSeamDump(t *testing.T, backupDir, content string) string {
	t.Helper()
	dir := filepath.Join(backupDir, "tables")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "t.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return mustSHA256(t, path)
}

// okRows 返回依次成功 scan 出给定字符串值、随后正常结束的 stubRows。
func okRows(values ...string) *stubRows {
	scans := make([]func(dest ...any) error, 0, len(values))
	for _, value := range values {
		value := value
		scans = append(scans, func(dest ...any) error {
			*(dest[0].(*string)) = value
			return nil
		})
	}
	return &stubRows{scans: scans}
}

// pragmaRows 返回一行合法的 PRAGMA table_info 扫描结果（列名 id）。
func pragmaRows() *stubRows {
	return &stubRows{scans: []func(dest ...any) error{
		func(dest ...any) error {
			*(dest[0].(*int)) = 0
			*(dest[1].(*string)) = "id"
			*(dest[2].(*string)) = "INTEGER"
			*(dest[3].(*int)) = 0
			*(dest[4].(*any)) = nil
			*(dest[5].(*int)) = 1
			return nil
		},
	}}
}

// unmarshalableRows 返回一列 JSON 无法编码的值（func），驱动导出阶段
// 的编码错误分支。
func unmarshalableRows() *stubRows {
	return &stubRows{scans: []func(dest ...any) error{
		func(dest ...any) error {
			for _, dest := range dest {
				*(dest.(*any)) = func() {}
			}
			return nil
		},
	}}
}

// TestBackupDatabaseDumpSeamErrors 经 queryRows 分发覆盖 dumpTable 的
// 列检查 / SELECT / 扫描 / 编码 / 迭代错误分支。
func TestBackupDatabaseDumpSeamErrors(t *testing.T) {
	db := openGormSQLite(t, filepath.Join(t.TempDir(), "dump.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)

	cases := []struct {
		name     string
		dump     rowScanner
		dumpErr  error
		pragmaE  error
		want     string
		injected bool
	}{
		{"tableColumns error", nil, nil, errInjected, "inspect table", true},
		{"select error", nil, errInjected, nil, "read table", true},
		{"scan error", &stubRows{scans: []func(dest ...any) error{scanErrRow()}}, nil, nil, "scan table", true},
		{"encode error", unmarshalableRows(), nil, nil, "encode row", false},
		{"iterate error", &stubRows{finalErr: errInjected}, nil, nil, "iterate table", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			swapQueryRowsFunc(t, func(query string) (rowScanner, error) {
				switch {
				case strings.Contains(query, "sqlite_master"):
					return okRows("t"), nil
				case strings.HasPrefix(query, "PRAGMA"):
					return pragmaRows(), tc.pragmaE
				default:
					return tc.dump, tc.dumpErr
				}
			})
			_, err := BackupDatabase(db, filepath.Join(t.TempDir(), "out"))
			assert.ErrorContains(t, err, tc.want)
			if tc.injected {
				assert.ErrorIs(t, err, errInjected)
			}
		})
	}
}

// TestBackupDatabaseFileSeamErrors 覆盖备份输出侧的文件系统与序列化错误。
func TestBackupDatabaseFileSeamErrors(t *testing.T) {
	t.Run("get sql.DB error", func(t *testing.T) {
		original := getSQLDB
		getSQLDB = func(*gorm.DB) (*sql.DB, error) { return nil, errInjected }
		t.Cleanup(func() { getSQLDB = original })

		db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
		_, err := BackupDatabase(db, filepath.Join(t.TempDir(), "out"))
		assert.ErrorContains(t, err, "get sql.DB")
	})

	t.Run("encode manifest error", func(t *testing.T) {
		original := jsonMarshalIndent
		jsonMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errInjected }
		t.Cleanup(func() { jsonMarshalIndent = original })

		db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
		require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error)
		_, err := BackupDatabase(db, filepath.Join(t.TempDir(), "out"))
		assert.ErrorContains(t, err, "encode manifest")
	})

	t.Run("write manifest error", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
		require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error)
		_, err := BackupDatabase(db, outDir)
		require.NoError(t, err)

		// 已有 dump 文件可原位覆写；新 manifest.json 在只读目录里创建失败。
		require.NoError(t, os.Remove(filepath.Join(outDir, "manifest.json")))
		require.NoError(t, os.Chmod(outDir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

		_, err = BackupDatabase(db, outDir)
		assert.ErrorContains(t, err, "write manifest")
	})

	t.Run("create dump file error", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.MkdirAll(filepath.Join(outDir, "tables"), 0o755))
		require.NoError(t, os.Chmod(filepath.Join(outDir, "tables"), 0o500))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(outDir, "tables"), 0o755) })

		db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
		require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error)
		_, err := BackupDatabase(db, outDir)
		assert.ErrorContains(t, err, "create dump file")
	})

	t.Run("sync dump file error", func(t *testing.T) {
		original := syncFile
		syncFile = func(*os.File) error { return errInjected }
		t.Cleanup(func() { syncFile = original })

		db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
		require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error)
		_, err := BackupDatabase(db, filepath.Join(t.TempDir(), "out"))
		assert.ErrorContains(t, err, "flush dump file")
	})
}

// TestRestoreDatabaseSeamErrors 覆盖恢复主流程的连接、PRAGMA、清表与
// 解码错误分支。
func TestRestoreDatabaseSeamErrors(t *testing.T) {
	// 先用真实备份生成一份合法 fixture，再对各环节注入失败。
	source := openGormSQLite(t, filepath.Join(t.TempDir(), "source.db"))
	require.NoError(t, source.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
	require.NoError(t, source.Exec("INSERT INTO t (id, name) VALUES (1, 'a')").Error)
	backupDir := filepath.Join(t.TempDir(), "backup")
	_, err := BackupDatabase(source, backupDir)
	require.NoError(t, err)

	newTarget := func(t *testing.T) *gorm.DB {
		target := openGormSQLite(t, filepath.Join(t.TempDir(), "target.db"))
		require.NoError(t, target.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
		return target
	}

	t.Run("get sql.DB error", func(t *testing.T) {
		original := getSQLDB
		getSQLDB = func(*gorm.DB) (*sql.DB, error) { return nil, errInjected }
		t.Cleanup(func() { getSQLDB = original })

		_, err := RestoreDatabase(newTarget(t), backupDir)
		assert.ErrorContains(t, err, "get sql.DB")
	})

	t.Run("grab connection error", func(t *testing.T) {
		target := newTarget(t)
		sqlDB, rawErr := target.DB()
		require.NoError(t, rawErr)
		require.NoError(t, sqlDB.Close())

		_, err := RestoreDatabase(target, backupDir)
		assert.ErrorContains(t, err, "grab connection")
	})

	t.Run("read pragma error", func(t *testing.T) {
		original := connQueryRow
		connQueryRow = func(context.Context, *sql.Conn, string) (singleScanner, error) { return nil, errInjected }
		t.Cleanup(func() { connQueryRow = original })

		_, err := RestoreDatabase(newTarget(t), backupDir)
		assert.ErrorContains(t, err, "read foreign_keys pragma")
	})

	t.Run("disable pragma error", func(t *testing.T) {
		original := connExec
		connExec = func(context.Context, *sql.Conn, string, ...any) (sql.Result, error) { return nil, errInjected }
		t.Cleanup(func() { connExec = original })

		_, err := RestoreDatabase(newTarget(t), backupDir)
		assert.ErrorContains(t, err, "disable foreign keys")
	})

	t.Run("clear table error", func(t *testing.T) {
		// restoreTable 的清表语句经 txExec 注入失败；不构造真实死事务，
		// 避免与恢复专用连接在驱动层互锁。
		original := txExec
		txExec = func(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
			if strings.HasPrefix(query, `DELETE FROM "`) {
				return nil, errInjected
			}
			return tx.ExecContext(ctx, query, args...)
		}
		t.Cleanup(func() { txExec = original })

		_, err := RestoreDatabase(newTarget(t), backupDir)
		assert.ErrorContains(t, err, "clear table")
	})

	t.Run("decode dump error", func(t *testing.T) {
		// dump 内容损坏且校验和同步更新：失败必须落在解码而非校验。
		dumpPath := filepath.Join(backupDir, "tables", "t.jsonl")
		require.NoError(t, os.WriteFile(dumpPath, []byte("{not json"), 0o644))
		manifest, err := readDatabaseManifest(backupDir)
		require.NoError(t, err)
		entry := manifest.Tables[0]
		entry.SHA256 = mustSHA256(t, dumpPath)
		manifest.Tables[0] = entry
		writeManifestFile(t, backupDir, manifest)

		_, err = RestoreDatabase(newTarget(t), backupDir)
		assert.ErrorContains(t, err, "decode dump")
	})
}

// TestRestoreSequencesTxErrors 经 txExec 注入覆盖 sqlite_sequence 的
// 清空与回插错误分支（探测在真实表上通过，失败精确落在目标语句）。
func TestRestoreSequencesTxErrors(t *testing.T) {
	cases := []struct {
		name  string
		match string
		want  string
	}{
		{"clear error", "DELETE FROM sqlite_sequence", "clear sqlite_sequence"},
		{"insert error", "INSERT INTO sqlite_sequence", "restore sequence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := openGormSQLite(t, filepath.Join(t.TempDir(), "seq-target.db"))
			require.NoError(t, target.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
			require.NoError(t, target.Exec("CREATE TABLE auto_t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)").Error)

			source := openGormSQLite(t, filepath.Join(t.TempDir(), "seq-source.db"))
			require.NoError(t, source.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)").Error)
			backupDir := filepath.Join(t.TempDir(), "backup")
			manifest, err := BackupDatabase(source, backupDir)
			require.NoError(t, err)
			manifest.Sequence = []SequenceEntry{{Name: "auto_t", Seq: 2}}
			writeManifestFile(t, backupDir, manifest)

			original := txExec
			txExec = func(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
				if strings.Contains(query, tc.match) {
					return nil, errInjected
				}
				return tx.ExecContext(ctx, query, args...)
			}
			t.Cleanup(func() { txExec = original })

			_, err = RestoreDatabase(target, backupDir)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestEncodeCellValues 直接覆盖导出单元的类型分派。
func TestEncodeCellValues(t *testing.T) {
	assert.Nil(t, encodeCell(nil))
	assert.Equal(t, "blob", encodeCell([]byte("blob")))
	assert.Equal(t, "2026-09-17T00:00:00Z",
		encodeCell(time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, 42, encodeCell(42))
}

// TestManifestTableLookup 覆盖清单查表的命中与未命中。
func TestManifestTableLookup(t *testing.T) {
	manifest := DatabaseManifest{Tables: []TableEntry{{Name: "users"}}}

	entry, ok := ManifestTable(manifest, "users")
	assert.True(t, ok)
	assert.Equal(t, "users", entry.Name)

	_, ok = ManifestTable(manifest, "tickets")
	assert.False(t, ok)
}

// TestBackupDatabaseProbeSequenceError 经 probeRow seam 覆盖备份阶段的
// sqlite_sequence 探测错误分支。
func TestBackupDatabaseProbeSequenceError(t *testing.T) {
	db := openGormSQLite(t, filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)").Error)
	swapProbeRow(t, nil, errInjected)

	_, err := BackupDatabase(db, filepath.Join(t.TempDir(), "out"))
	assert.ErrorContains(t, err, "probe sqlite_sequence")
}

// TestListSequencesNilProbeRow 覆盖 probeRow 返回空扫描器的防御分支
// （生产实现不会返回 nil，仅测试注入）。
func TestListSequencesNilProbeRow(t *testing.T) {
	db := openRawSQLite(t, filepath.Join(t.TempDir(), "x.db"))
	swapProbeRow(t, nil, nil)

	sequences, err := listSequences(db)
	require.NoError(t, err)
	assert.Empty(t, sequences)
}

// TestOpenVerifiedDumpsClosesOnFailure 锁定整体校验语义：第二个 dump
// 校验失败时，已打开的第一个文件必须被关闭，不留泄漏句柄。
func TestOpenVerifiedDumpsClosesOnFailure(t *testing.T) {
	backupDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(backupDir, "tables"), 0o755))
	first := filepath.Join(backupDir, "tables", "t.jsonl")
	second := filepath.Join(backupDir, "tables", "u.jsonl")
	require.NoError(t, os.WriteFile(first, []byte("[1, \"a\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(second, []byte("[2, \"b\"]\n"), 0o644))

	manifest := DatabaseManifest{Version: manifestVersion, Dialect: "sqlite", Tables: []TableEntry{
		{Name: "t", Columns: []string{"id", "name"}, File: "tables/t.jsonl", SHA256: mustSHA256(t, first)},
		{Name: "u", Columns: []string{"id", "name"}, File: "tables/u.jsonl", SHA256: "deadbeef"},
	}}

	_, err := openVerifiedDumps(backupDir, manifest.Tables)
	assert.ErrorIs(t, err, errChecksumMismatch)
}

// TestOpenVerifiedFileErrors 覆盖打开/哈希/回卷三个失败分支：打开失败
// 用缺失路径，哈希失败用目录路径（os.Open 成功但读取返回 EISDIR），
// 回卷失败经 seam 注入。
func TestOpenVerifiedFileErrors(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload.jsonl")
	require.NoError(t, os.WriteFile(payload, []byte("[1]\n"), 0o644))

	_, err := openVerifiedFile(filepath.Join(dir, "missing.jsonl"), mustSHA256(t, payload))
	assert.ErrorContains(t, err, "open")

	_, err = openVerifiedFile(dir, mustSHA256(t, payload))
	assert.ErrorContains(t, err, "hash")

	original := rewindFile
	rewindFile = func(*os.File) error { return errInjected }
	t.Cleanup(func() { rewindFile = original })
	_, err = openVerifiedFile(payload, mustSHA256(t, payload))
	assert.ErrorContains(t, err, "rewind")
}
