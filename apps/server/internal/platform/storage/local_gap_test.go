package storage

import (
	"os"
	"testing"
)

func TestLocalProviderDeleteErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	// 非空目录无法被 os.Remove 删除,触发非 NotExist 的错误分支
	if err := os.MkdirAll(dir+"/occupant/inner", 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(dir+"/occupant/inner/f.txt", []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := NewLocalProvider(dir, "/uploads")
	if err := p.Delete("occupant"); err == nil {
		t.Fatal("Delete() expected error when removing a non-empty directory")
	}
}
