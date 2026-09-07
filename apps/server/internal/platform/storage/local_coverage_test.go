package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingReader struct{}

var errFailingReader = errors.New("read failure")

func (failingReader) Read(p []byte) (int, error) { return 0, errFailingReader }

func TestLocalProviderSaveFailingReader(t *testing.T) {
	dir := t.TempDir()
	p := NewLocalProvider(dir, "/uploads")

	info, err := p.Save("fail.txt", failingReader{}, 10)
	if err == nil {
		t.Fatal("Save() expected error for failing reader")
	}
	if info != nil {
		t.Fatalf("Save() info = %+v, want nil", info)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "fail.txt")); !os.IsNotExist(statErr) {
		t.Fatal("partial file should be removed after write failure")
	}
}

func TestLocalProviderSaveDirCreationFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := NewLocalProvider(file, "/uploads")
	if _, err := p.Save("a/b.txt", strings.NewReader("data"), 4); err == nil {
		t.Fatal("Save() expected error when base dir cannot be created")
	}
}

func TestLocalProviderSaveFileCreateFailure(t *testing.T) {
	dir := t.TempDir()
	// "key.txt" already exists as a directory, so os.Create must fail.
	if err := os.Mkdir(filepath.Join(dir, "key.txt"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := NewLocalProvider(dir, "/uploads")
	if _, err := p.Save("key.txt", strings.NewReader("data"), 4); err == nil {
		t.Fatal("Save() expected error when file path is a directory")
	}
}

func TestLocalProviderOpenMissing(t *testing.T) {
	p := NewLocalProvider(t.TempDir(), "/uploads")
	if _, err := p.Open("missing.txt"); err == nil {
		t.Fatal("Open() expected error for missing file")
	}
}
