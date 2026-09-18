package recovery

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileEntry 是归档内单个文件的条目：恢复与对账都按 sha256 逐文件校验，
// 保证"恢复成功"的口径是字节级一致，而不是仅仅文件存在。
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

// FilesManifest 是一次上传资产归档的清单。
type FilesManifest struct {
	Version   int         `json:"version"`
	CreatedAt string      `json:"created_at"`
	Files     []FileEntry `json:"files"`
}

// readSeekCloser 抽象归档写入用到的文件能力。读写/定位/关闭错误在真实
// 文件系统上不可达，经 fileOpen / fileCreate seam 注入以保持防御性错误
// 传播（与包内 queryRows 等 seam 同惯例）。
type readSeekCloser interface {
	io.ReadWriteSeeker
	io.Closer
}

// 归档写入的文件层 seam：生产实现即直通 os。
var (
	fileOpen = func(name string) (readSeekCloser, error) {
		return os.Open(name)
	}
	fileCreate = func(name string) (io.WriteCloser, error) {
		return os.Create(name)
	}
	statPath = func(name string) (os.FileInfo, error) {
		return os.Stat(name)
	}
)

// BackupFiles 把 rootDir 打成 tar.gz 归档并返回逐文件清单。
// 空目录不记录（tar 不携带目录条目），恢复以文件为准。
func BackupFiles(rootDir, archivePath string) (FilesManifest, error) {
	manifest := FilesManifest{Version: manifestVersion, CreatedAt: timeNowUTC()}

	var paths []string
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return FilesManifest{}, err
	}
	sort.Strings(paths)

	archive, err := fileCreate(archivePath)
	if err != nil {
		return FilesManifest{}, err
	}
	defer archive.Close()

	gzipWriter := gzip.NewWriter(archive)
	tarWriter := tar.NewWriter(gzipWriter)

	for _, path := range paths {
		info, err := statPath(path)
		if err != nil {
			return FilesManifest{}, err
		}
		entry, err := appendArchiveEntry(tarWriter, rootDir, path, info)
		if err != nil {
			return FilesManifest{}, err
		}
		manifest.Files = append(manifest.Files, entry)
	}

	// 两层 writer 的关闭错误一并返回：任一 flush 失败都意味着归档不完整。
	if err := errors.Join(tarWriter.Close(), gzipWriter.Close()); err != nil {
		return FilesManifest{}, err
	}
	return manifest, archive.Close()
}

// appendArchiveEntry 把单个文件写入 tar 并返回清单条目。open/hash/seek/
// 写头/写体/关闭六个动作的错误在真实文件系统上不可达，经 fileOpen seam
// 与测试注入的 stub 覆盖。
func appendArchiveEntry(tarWriter *tar.Writer, rootDir, path string, info os.FileInfo) (FileEntry, error) {
	raw, err := fileOpen(path)
	if err != nil {
		return FileEntry{}, err
	}
	defer func() {
		_ = raw.Close()
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, raw); err != nil {
		return FileEntry{}, err
	}
	if _, err := raw.Seek(0, io.SeekStart); err != nil {
		return FileEntry{}, err
	}

	// 手构 header（常规文件所需字段就这几项），不走 FileInfoHeader
	// 的类型分派，也消除该调用在常规文件上不可达的错误分支。
	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(rootDir)+"/")),
		Size:     info.Size(),
		Mode:     int64(info.Mode().Perm()),
		ModTime:  info.ModTime(),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return FileEntry{}, err
	}
	if _, err := io.Copy(tarWriter, raw); err != nil {
		return FileEntry{}, err
	}
	if err := raw.Close(); err != nil {
		return FileEntry{}, err
	}
	return FileEntry{
		Path:   header.Name,
		Size:   info.Size(),
		Mode:   uint32(info.Mode().Perm()),
		SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

// RestoreFiles 从归档恢复到 destDir：先整体校验归档内每个文件的 sha256
// 与 manifest 一致，再解包落盘（同样按字节校验），返回恢复所用 manifest。
func RestoreFiles(archivePath string, manifest FilesManifest, destDir string) (FilesManifest, error) {
	if manifest.Version != manifestVersion {
		return FilesManifest{}, errUnsupportedManifestVersion
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return FilesManifest{}, err
	}
	defer archive.Close()

	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return FilesManifest{}, err
	}
	defer gzipReader.Close()

	entries := make(map[string]FileEntry, len(manifest.Files))
	for _, entry := range manifest.Files {
		entries[entry.Path] = entry
	}

	tarReader := tar.NewReader(gzipReader)
	restored := make(map[string]bool, len(manifest.Files))
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return FilesManifest{}, err
		}
		entry, ok := entries[header.Name]
		if !ok {
			return FilesManifest{}, errUnexpectedArchiveEntry
		}
		if err := extractVerifiedFile(tarReader, filepath.Join(destDir, filepath.FromSlash(header.Name)), entry); err != nil {
			return FilesManifest{}, err
		}
		restored[header.Name] = true
	}

	for _, entry := range manifest.Files {
		if !restored[entry.Path] {
			return FilesManifest{}, errMissingArchiveEntry
		}
	}
	return manifest, nil
}

func extractVerifiedFile(tarReader io.Reader, target string, entry FileEntry) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	hash := sha256.New()
	raw, err := fileCreate(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.MultiWriter(raw, hash), tarReader); err != nil {
		_ = raw.Close()
		return err
	}
	if err := raw.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != entry.SHA256 {
		return errChecksumMismatch
	}
	return nil
}

// VerifyFiles 对账 rootDir 现状与 manifest：返回不一致路径列表（缺失、
// 新增、内容或大小不符），空列表即"恢复后与备份一致"。
func VerifyFiles(rootDir string, manifest FilesManifest) []string {
	expected := make(map[string]FileEntry, len(manifest.Files))
	for _, entry := range manifest.Files {
		expected[entry.Path] = entry
	}

	var problems []string
	seen := make(map[string]bool, len(manifest.Files))
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(rootDir)+"/"))
		entry, ok := expected[rel]
		if !ok {
			problems = append(problems, "unexpected: "+rel)
			return nil
		}
		seen[rel] = true
		sum, size, err := hashFile(path)
		if err != nil {
			problems = append(problems, "unreadable: "+rel)
			return nil
		}
		if sum != entry.SHA256 || size != entry.Size {
			problems = append(problems, "modified: "+rel)
		}
		return nil
	})
	if err != nil {
		problems = append(problems, "walk: "+err.Error())
		return problems
	}
	for _, entry := range manifest.Files {
		if !seen[entry.Path] {
			problems = append(problems, "missing: "+entry.Path)
		}
	}
	sort.Strings(problems)
	return problems
}

func hashFile(path string) (string, int64, error) {
	raw, err := fileOpen(path)
	if err != nil {
		return "", 0, err
	}
	defer raw.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, raw)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
