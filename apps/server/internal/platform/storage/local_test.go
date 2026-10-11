package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProviderSaveAndOpen(t *testing.T) {
	dir := t.TempDir()
	p := NewLocalProvider(dir, "/uploads")

	info, err := p.Save("2024/01/15/test.txt", strings.NewReader("hello world"), 11)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if info.Key != "2024/01/15/test.txt" {
		t.Errorf("Key = %q, want %q", info.Key, "2024/01/15/test.txt")
	}
	if info.Size != 11 {
		t.Errorf("Size = %d, want 11", info.Size)
	}
	if info.URL != "/uploads/2024/01/15/test.txt" {
		t.Errorf("URL = %q, want %q", info.URL, "/uploads/2024/01/15/test.txt")
	}

	rc, err := p.Open("2024/01/15/test.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }() // 测试收尾
	buf := make([]byte, 20)
	n, _ := rc.Read(buf)
	if string(buf[:n]) != "hello world" {
		t.Errorf("content = %q, want %q", string(buf[:n]), "hello world")
	}
}

func TestLocalProviderDelete(t *testing.T) {
	dir := t.TempDir()
	p := NewLocalProvider(dir, "/uploads")

	_, _ = p.Save("test.txt", strings.NewReader("data"), 4)
	if err := p.Delete("test.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify file is gone
	fullPath := filepath.Join(dir, "test.txt")
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}

	// Delete nonexistent should not error
	if err := p.Delete("nonexistent.txt"); err != nil {
		t.Fatalf("Delete nonexistent: %v", err)
	}
}

func TestLocalProviderPresignedURL(t *testing.T) {
	dir := t.TempDir()
	p := NewLocalProvider(dir, "/uploads")

	url, err := p.PresignedURL("some/key.txt", 3600)
	if err != nil {
		t.Fatalf("PresignedURL: %v", err)
	}
	if url != "/uploads/some/key.txt" {
		t.Errorf("URL = %q, want %q", url, "/uploads/some/key.txt")
	}
}

func TestLocalProviderSaveCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	p := NewLocalProvider(dir, "/uploads")

	_, err := p.Save("a/b/c/deep.txt", strings.NewReader("deep"), 4)
	if err != nil {
		t.Fatalf("Save nested: %v", err)
	}

	rc, err := p.Open("a/b/c/deep.txt")
	if err != nil {
		t.Fatalf("Open nested: %v", err)
	}
	_ = rc.Close() // 测试收尾
}
