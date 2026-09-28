package audit

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

func fixedClock(ts time.Time) func() time.Time { return func() time.Time { return ts } }

func TestNewFileArchiveWriterConfig(t *testing.T) {
	for _, dir := range []string{"", "   ", "."} {
		if _, err := NewFileArchiveWriter(dir); err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("NewFileArchiveWriter(%q) error = %v, want not configured", dir, err)
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, "nested", "archive")
	w, err := NewFileArchiveWriter(dir)
	if err != nil {
		t.Fatalf("NewFileArchiveWriter() error = %v", err)
	}
	if w == nil || w.dir != dir {
		t.Fatalf("writer = %+v, want dir %q", w, dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("archive dir not created: %v", err)
	}
}

func TestNewFileArchiveWriterMkdirAllFailure(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	if _, err := NewFileArchiveWriter(filepath.Join(blocker, "archive")); err == nil || !strings.Contains(err.Error(), "create audit archive dir") {
		t.Fatalf("NewFileArchiveWriter() error = %v, want create failure", err)
	}
}

func TestFileArchiveWriterWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 9, 28, 8, 30, 0, 0, time.UTC)
	w := &FileArchiveWriter{dir: dir, now: fixedClock(ts)}
	rows := []models.AuditLog{
		{ID: 7, PrincipalKind: "admin", Action: "ticket.update", ResourceType: "ticket", Route: "/api/tickets/7", Method: "PATCH", PrevHash: "prev-hash-7", EntryHash: "entry-hash-7"},
		{ID: 8, PrincipalKind: "admin", Action: "ticket.delete", ResourceType: "ticket", Route: "/api/tickets/8", Method: "DELETE", PrevHash: "entry-hash-7", EntryHash: "entry-hash-8"},
	}
	if err := w.Write(context.Background(), rows); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	name := fmt.Sprintf("audit-archive-%s-7-8.json.gz", ts.Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	zr, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if err := zr.Close(); err != nil {
		t.Fatalf("close gzip reader: %v", err)
	}
	var got []models.AuditLog
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal archive: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("archived rows = %d want 2", len(got))
	}
	// 哈希链字段必须原样进出冷层（归档后仍可链式对账的前提）。
	for i, want := range rows {
		if got[i].ID != want.ID || got[i].Action != want.Action {
			t.Fatalf("archived[%d] = (%d, %s), want (%d, %s)", i, got[i].ID, got[i].Action, want.ID, want.Action)
		}
		if got[i].PrevHash != want.PrevHash || got[i].EntryHash != want.EntryHash {
			t.Fatalf("archived[%d] hash chain = (%s, %s), want (%s, %s)", i, got[i].PrevHash, got[i].EntryHash, want.PrevHash, want.EntryHash)
		}
	}
	// 原子落盘：不留 .tmp 半截文件。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("archive dir entries = %d want 1 (no tmp leftovers)", len(entries))
	}
}

func TestFileArchiveWriterWriteNoop(t *testing.T) {
	dir := t.TempDir()
	w := &FileArchiveWriter{dir: dir, now: time.Now}
	if err := w.Write(context.Background(), nil); err != nil {
		t.Fatalf("Write(nil rows) error = %v", err)
	}
	var nilWriter *FileArchiveWriter
	if err := nilWriter.Write(context.Background(), []models.AuditLog{{ID: 1}}); err != nil {
		t.Fatalf("nil receiver Write() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("archive dir entries = %d want 0", len(entries))
	}
}

func TestFileArchiveWriterWriteCtxCanceled(t *testing.T) {
	w := &FileArchiveWriter{dir: t.TempDir(), now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Write(ctx, []models.AuditLog{{ID: 1}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Write() error = %v, want context.Canceled", err)
	}
}

func TestFileArchiveWriterWriteMarshalError(t *testing.T) {
	orig := marshalRows
	marshalRows = func([]models.AuditLog) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { marshalRows = orig }()

	w := &FileArchiveWriter{dir: t.TempDir(), now: time.Now}
	err := w.Write(context.Background(), []models.AuditLog{{ID: 1}})
	if err == nil || !strings.Contains(err.Error(), "encode audit archive") {
		t.Fatalf("Write() error = %v, want encode failure", err)
	}
}

func TestFileArchiveWriterWriteFileError(t *testing.T) {
	dir := t.TempDir()
	w := &FileArchiveWriter{dir: dir, now: time.Now}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	err := w.Write(context.Background(), []models.AuditLog{{ID: 1}})
	if err == nil || !strings.Contains(err.Error(), "write audit archive") {
		t.Fatalf("Write() error = %v, want write failure", err)
	}
}

func TestFileArchiveWriterWriteRenameFailure(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 9, 28, 8, 30, 0, 0, time.UTC)
	w := &FileArchiveWriter{dir: dir, now: fixedClock(ts)}
	// 同名目标预建为目录：WriteFile 成功、rename 失败，且必须清理 tmp。
	target := filepath.Join(dir, fmt.Sprintf("audit-archive-%s-1-1.json.gz", ts.Format("20060102T150405Z")))
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("seed target dir: %v", err)
	}
	err := w.Write(context.Background(), []models.AuditLog{{ID: 1}})
	if err == nil || !strings.Contains(err.Error(), "finalize audit archive") {
		t.Fatalf("Write() error = %v, want finalize failure", err)
	}
	if _, statErr := os.Stat(target + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("tmp file not cleaned after rename failure: %v", statErr)
	}
}

func TestGormRetentionServiceArchiveBeforeDelete(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	logs := []models.AuditLog{
		{Action: "old-1", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/1", Method: "POST", PrevHash: "", EntryHash: "hash-1", CreatedAt: now.Add(-200 * 24 * time.Hour)},
		{Action: "old-2", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/2", Method: "POST", PrevHash: "hash-1", EntryHash: "hash-2", CreatedAt: now.Add(-190 * 24 * time.Hour)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	dir := t.TempDir()
	writer, err := NewFileArchiveWriter(dir)
	if err != nil {
		t.Fatalf("NewFileArchiveWriter() error = %v", err)
	}
	// batchSize=1：两批各归档一个文件，覆盖循环中多次归档的路径。
	svc := NewGormRetentionService(db, 180*24*time.Hour, 1).WithArchive(writer)
	deleted, err := svc.Cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d want 2", deleted)
	}
	var count int64
	if err := db.Model(&models.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if count != 0 {
		t.Fatalf("remaining rows = %d want 0", count)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("archive files = %d want 2", len(entries))
	}
	// 归档内容可回读且哈希链字段完整。
	zr, err := gzip.NewReader(strings.NewReader(readArchiveFile(t, dir, entries[0].Name())))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	var archived []models.AuditLog
	if err := json.Unmarshal(raw, &archived); err != nil {
		t.Fatalf("unmarshal archive: %v", err)
	}
	if len(archived) != 1 {
		t.Fatalf("archived rows = %d want 1", len(archived))
	}
	want := logs[0]
	if archived[0].Action != want.Action || archived[0].EntryHash != want.EntryHash {
		t.Fatalf("archived = (%s, %s), want (%s, %s)", archived[0].Action, archived[0].EntryHash, want.Action, want.EntryHash)
	}
}

func TestGormRetentionServiceArchiveFailureKeepsRows(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	logs := []models.AuditLog{
		{Action: "old-1", PrincipalKind: "admin", ResourceType: "ticket", Route: "/api/tickets/1", Method: "POST", CreatedAt: now.Add(-200 * 24 * time.Hour)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormRetentionService(db, 180*24*time.Hour, 10).WithArchive(failArchiveWriter{})
	deleted, err := svc.Cleanup(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "archive expired audit logs before delete") {
		t.Fatalf("Cleanup() error = %v, want archive failure", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d want 0（归档失败不得产生删除）", deleted)
	}
	var count int64
	if err := db.Model(&models.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if count != 1 {
		t.Fatalf("remaining rows = %d want 1（行必须原样保留）", count)
	}
}

type failArchiveWriter struct{}

func (failArchiveWriter) Write(context.Context, []models.AuditLog) error {
	return errors.New("cold storage unavailable")
}

func TestWithArchiveNilReceiver(t *testing.T) {
	var nilSvc *GormRetentionService
	if got := nilSvc.WithArchive(nil); got != nil {
		t.Fatalf("nil receiver WithArchive() = %+v, want nil (no panic)", got)
	}
	db := openTestDB(t)
	svc := NewGormRetentionService(db, time.Hour, 1)
	if got := svc.WithArchive(failArchiveWriter{}); got != svc || got.archiver == nil {
		t.Fatalf("WithArchive() chain broken: %+v", got)
	}
}

func readArchiveFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read archive %s: %v", name, err)
	}
	return string(data)
}
