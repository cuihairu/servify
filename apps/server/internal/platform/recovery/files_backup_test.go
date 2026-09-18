package recovery

// files_backup_test.go 覆盖上传资产归档/恢复/对账的错误分支。
// 演练主路径（篡改注入 + 字节级恢复）在 drill_test.go。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedArchiveFixture(t *testing.T) (string, string, FilesManifest) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "uploads")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "b.txt"), []byte("beta"), 0o644))

	archivePath := filepath.Join(t.TempDir(), "uploads.tar.gz")
	manifest, err := BackupFiles(root, archivePath)
	require.NoError(t, err)
	return root, archivePath, manifest
}

func TestBackupFilesWalkError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := BackupFiles(missing, filepath.Join(t.TempDir(), "out.tar.gz"))
	assert.Error(t, err)
}

func TestBackupFilesArchivePathNotCreatable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker.txt")
	require.NoError(t, os.WriteFile(blocker, []byte("occupied"), 0o644))

	root := filepath.Join(t.TempDir(), "uploads")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644))

	_, err := BackupFiles(root, filepath.Join(blocker, "inner.tar.gz"))
	assert.Error(t, err)
}

func TestRestoreFilesManifestVersionMismatch(t *testing.T) {
	_, archivePath, manifest := seedArchiveFixture(t)
	manifest.Version = 99
	_, err := RestoreFiles(archivePath, manifest, t.TempDir())
	assert.ErrorIs(t, err, errUnsupportedManifestVersion)
}

func TestRestoreFilesArchiveMissing(t *testing.T) {
	_, _, manifest := seedArchiveFixture(t)
	_, err := RestoreFiles(filepath.Join(t.TempDir(), "nope.tar.gz"), manifest, t.TempDir())
	assert.Error(t, err)
}

func TestRestoreFilesNotGzip(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "junk.tar.gz")
	require.NoError(t, os.WriteFile(junk, []byte("definitely not gzip"), 0o644))
	_, _, manifest := seedArchiveFixture(t)

	_, err := RestoreFiles(junk, manifest, t.TempDir())
	assert.Error(t, err)
}

// TestRestoreFilesUnexpectedEntry 锁定"归档内多出 manifest 之外的文件"
// 的防御：清单与归档必须互为闭包，缺一侧都算备份损坏。
func TestRestoreFilesUnexpectedEntry(t *testing.T) {
	root, _, manifest := seedArchiveFixture(t)
	archivePath := filepath.Join(t.TempDir(), "extra.tar.gz")
	_, err := BackupFiles(root, archivePath)
	require.NoError(t, err)
	manifest.Files = manifest.Files[:1] // 丢掉一个条目，归档里却还在

	_, err = RestoreFiles(archivePath, manifest, t.TempDir())
	assert.ErrorIs(t, err, errUnexpectedArchiveEntry)
}

func TestRestoreFilesMissingEntry(t *testing.T) {
	_, archivePath, manifest := seedArchiveFixture(t)
	manifest.Files = append(manifest.Files, FileEntry{Path: "ghost.txt", SHA256: "deadbeef"})

	_, err := RestoreFiles(archivePath, manifest, t.TempDir())
	assert.ErrorIs(t, err, errMissingArchiveEntry)
}

// TestRestoreFilesChecksumMismatch 锁定字节级校验：归档内容与清单不符
// 时恢复必须拒绝。
func TestRestoreFilesChecksumMismatch(t *testing.T) {
	_, _, manifest := seedArchiveFixture(t)

	// 归档条目名与清单一致、内容不同——校验和必然不匹配。
	tampered := mustWriteTamperedArchive(t, manifest)
	_, err := RestoreFiles(tampered, manifest, t.TempDir())
	assert.ErrorIs(t, err, errChecksumMismatch)
}

func TestRestoreFilesDestNotCreatable(t *testing.T) {
	_, archivePath, manifest := seedArchiveFixture(t)

	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o644))

	_, err := RestoreFiles(archivePath, manifest, filepath.Join(blocker, "dest"))
	assert.Error(t, err)
}

func TestVerifyFilesProblems(t *testing.T) {
	root, _, manifest := seedArchiveFixture(t)
	assert.Empty(t, VerifyFiles(root, manifest))

	// 缺失 + 篡改 + 新增三类问题都能被对账抓到。
	require.NoError(t, os.Remove(filepath.Join(root, "a.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "b.txt"), []byte("changed"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "extra.txt"), []byte("rogue"), 0o644))
	problems := VerifyFiles(root, manifest)
	require.Len(t, problems, 3)
	assert.Equal(t, []string{
		"missing: a.txt",
		"modified: nested/b.txt",
		"unexpected: extra.txt",
	}, problems)
}

func TestVerifyFilesWalkError(t *testing.T) {
	_, _, manifest := seedArchiveFixture(t)
	problems := VerifyFiles(filepath.Join(t.TempDir(), "missing-root"), manifest)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "walk:")
}

func TestHashFileError(t *testing.T) {
	_, _, err := hashFile(filepath.Join(t.TempDir(), "nope.txt"))
	assert.Error(t, err)
}

// TestBackupFilesStatError 经 statPath seam 覆盖归档循环里的 Stat 错误
// 分支（WalkDir 与 Stat 之间文件消失在单线程下不可复现）。
func TestBackupFilesStatError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644))

	original := statPath
	statPath = func(string) (os.FileInfo, error) { return nil, errInjected }
	t.Cleanup(func() { statPath = original })

	_, err := BackupFiles(root, filepath.Join(t.TempDir(), "out.tar.gz"))
	assert.ErrorIs(t, err, errInjected)
}

// TestBackupFilesAppendEntryError 经 fileOpen seam 覆盖归档循环里单文件
// 追加失败的分支。
func TestBackupFilesAppendEntryError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644))

	original := fileOpen
	fileOpen = func(string) (readSeekCloser, error) { return nil, errInjected }
	t.Cleanup(func() { fileOpen = original })

	_, err := BackupFiles(root, filepath.Join(t.TempDir(), "out.tar.gz"))
	assert.ErrorIs(t, err, errInjected)
}

// TestRestoreFilesTarStreamError 覆盖 gzip 可解但 tar 流损坏的分支：
// Next 返回非 EOF 错误时恢复必须失败。
func TestRestoreFilesTarStreamError(t *testing.T) {
	_, _, manifest := seedArchiveFixture(t)

	archivePath := filepath.Join(t.TempDir(), "corrupt.tar.gz")
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	_, err := gzipWriter.Write([]byte("valid gzip header, garbage tar payload"))
	require.NoError(t, err)
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0o644))

	_, err = RestoreFiles(archivePath, manifest, t.TempDir())
	assert.Error(t, err)
	assert.NotErrorIs(t, err, io.EOF)
}

// TestVerifyFilesUnreadable 经 fileOpen seam 覆盖对账时文件不可读的分支。
func TestVerifyFilesUnreadable(t *testing.T) {
	root, _, manifest := seedArchiveFixture(t)

	original := fileOpen
	fileOpen = func(string) (readSeekCloser, error) { return nil, errInjected }
	t.Cleanup(func() { fileOpen = original })

	problems := VerifyFiles(root, manifest)
	require.Len(t, problems, len(manifest.Files))
	for _, problem := range problems {
		assert.Contains(t, problem, "unreadable: ")
	}
}

// mustWriteTamperedArchive 生成一个条目名与 manifest 一致、内容不同的归档。
func mustWriteTamperedArchive(t *testing.T, manifest FilesManifest) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tampered.tar.gz")

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range manifest.Files {
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name: entry.Path,
			Size: int64(len("tampered-content")),
			Mode: 0o644,
		}))
		_, err := tarWriter.Write([]byte("tampered-content"))
		require.NoError(t, err)
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}
