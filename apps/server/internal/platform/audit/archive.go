package audit

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"servify/apps/server/internal/models"
)

// 冷热分层归档（T3 收尾）：retention 清理删除过期审计记录前，先把整批行
// 序列化为 gzip 压缩 JSON 落到归档目录（冷层，每批一个快照文件）。行内
// 保留 prev_hash / entry_hash——冷层数据与热层同一条哈希链，归档后仍可
// 用链式校验对账。归档失败即中断本轮清理且不删除：热层清库永不出现
// 「未归档先删除」的数据丢失窗口。

// ArchiveWriter 在清理删除前接收整批过期审计记录（冷层落盘契约）。
type ArchiveWriter interface {
	Write(ctx context.Context, rows []models.AuditLog) error
}

// marshalRows func→var seam：JSON 编码失败属防御分支（AuditLog 全字段可
// 序列化、正常不可达），留 seam 供测试注入覆盖（同 perfbench 先例）。
var marshalRows = func(rows []models.AuditLog) ([]byte, error) {
	return json.Marshal(rows)
}

// FileArchiveWriter 把审计记录按批落为 gzip JSON 快照文件：每批一个文件
// （文件名含时间与首尾 id，天然按时间排序、可对账），临时文件 + rename
// 落盘——进程中断不会留下半截归档被误当完整冷层数据。
type FileArchiveWriter struct {
	dir string
	// now 便于测试固定文件名（生产恒为 time.Now）。
	now func() time.Time
}

func NewFileArchiveWriter(dir string) (*FileArchiveWriter, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "." {
		return nil, fmt.Errorf("audit archive dir is not configured")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create audit archive dir: %w", err)
	}
	return &FileArchiveWriter{dir: dir, now: time.Now}, nil
}

func (w *FileArchiveWriter) Write(ctx context.Context, rows []models.AuditLog) error {
	if w == nil || len(rows) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := marshalRows(rows)
	if err != nil {
		return fmt.Errorf("encode audit archive: %w", err)
	}
	name := fmt.Sprintf("audit-archive-%s-%d-%d.json.gz",
		w.now().UTC().Format("20060102T150405Z"), rows[0].ID, rows[len(rows)-1].ID)
	path := filepath.Join(w.dir, name)

	// gzip 底层是 bytes.Buffer（Write/Close 的错误分支静态不可达），
	// 数据完整性由 Close 后 buffer 内容即完整 gzip 流保证。
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write audit archive: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("finalize audit archive: %w", err)
	}
	return nil
}
