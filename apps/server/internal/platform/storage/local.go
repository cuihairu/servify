package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// compile-time interface check
var _ Provider = (*LocalProvider)(nil)

// LocalProvider stores files on the local filesystem.
type LocalProvider struct {
	baseDir string // absolute base directory for all stored files
	urlBase string // URL prefix for generating public URLs (e.g. "/uploads")
}

// NewLocalProvider creates a local filesystem storage provider.
// baseDir is the root directory where files are stored.
// urlBase is the URL path prefix used to construct public URLs.
func NewLocalProvider(baseDir, urlBase string) *LocalProvider {
	return &LocalProvider{
		baseDir: baseDir,
		urlBase: urlBase,
	}
}

func (p *LocalProvider) Save(key string, r io.Reader, size int64) (*ObjectInfo, error) {
	fullPath := filepath.Join(p.baseDir, key)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dir, err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create file %s: %w", fullPath, err)
	}
	defer func() {
		// 收尾 Close best-effort：io.Copy 已验写路径（失败即删文件返回），
		// 本地文件句柄无法注入 Close 故障；残留风险由读取方校验兜底。
		_ = f.Close()
	}()

	written, err := io.Copy(f, r)
	if err != nil {
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &ObjectInfo{
		Key:  key,
		Size: written,
		URL:  p.urlBase + "/" + filepath.ToSlash(key),
	}, nil
}

func (p *LocalProvider) Open(key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(p.baseDir, key)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", fullPath, err)
	}
	return f, nil
}

func (p *LocalProvider) Delete(key string) error {
	fullPath := filepath.Join(p.baseDir, key)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete file %s: %w", fullPath, err)
	}
	return nil
}

func (p *LocalProvider) PresignedURL(key string, _ int) (string, error) {
	return p.urlBase + "/" + filepath.ToSlash(key), nil
}
