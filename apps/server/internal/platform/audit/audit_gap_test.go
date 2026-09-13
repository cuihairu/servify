package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// cancellationOnlyCtx 只向调用方暴露"已取消"状态,但 Done() 返回 nil,
// database/sql 因此把它当作不可取消的 context,查询照常执行;
// Cleanup 批间检查的 ctx.Err() 却返回 context.Canceled。
// (真实取消的 context 会让第一条 Pluck 直接失败,走不到批间检查。)
type cancellationOnlyCtx struct {
	context.Context
}

func (cancellationOnlyCtx) Done() <-chan struct{} { return nil }
func (cancellationOnlyCtx) Err() error            { return context.Canceled }

// TestMergeRequestJSONMetadataOnly 覆盖 raw 为空且 metadata 可正常序列化时
// 的纯 metadata 分支(输出为脱敏后的 JSON 对象)。
func TestMergeRequestJSONMetadataOnly(t *testing.T) {
	got := mergeRequestJSON("", map[string]interface{}{
		"token": "secret-value",
		"note":  "kept",
	})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("merged = %q, want a JSON object", got)
	}
	if payload["token"] != "[REDACTED]" || payload["note"] != "kept" {
		t.Fatalf("unexpected merged payload: %v", payload)
	}
}

// TestGormQueryServiceListFindError 覆盖 List 的 Count 成功后 Find 失败分支:
// 只对带 ORDER BY 的语句(Find)注入错误,Count 不带 ORDER BY 先正常完成。
func TestGormQueryServiceListFindError(t *testing.T) {
	db := openTestDB(t)
	if err := db.Create(&models.AuditLog{
		Action: "list-find-err", PrincipalKind: "admin",
		ResourceType: "ticket", Route: "/t", Method: "POST",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("gap_list_find_error", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Clauses["ORDER BY"]; ok {
			tx.Error = errors.New("find failed")
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	svc := NewGormQueryService(db)
	logs, total, err := svc.List(context.Background(), ListQuery{})
	if err == nil {
		t.Fatal("expected find failure")
	}
	if total != 0 || logs != nil {
		t.Fatalf("find failure must discard results: total=%d logs=%v", total, logs)
	}
}

// TestGormRetentionServiceDeleteError 覆盖 Cleanup 的批量删除失败分支:
// query 回调正常取回 id,delete 回调注入错误。
func TestGormRetentionServiceDeleteError(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	if err := db.Create(&models.AuditLog{
		Action: "del-err", PrincipalKind: "admin",
		ResourceType: "ticket", Route: "/t", Method: "POST",
		CreatedAt: now.Add(-400 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		tx.Error = errors.New("delete failed")
	})

	svc := NewGormRetentionService(db, 180*24*time.Hour, 1)
	deleted, err := svc.Cleanup(context.Background(), now)
	if err == nil {
		t.Fatal("expected delete failure")
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
}

// TestGormRetentionServiceCtxErrBetweenBatches 覆盖 Cleanup 删满一整批后
// 检查到 ctx 取消的返回分支。
func TestGormRetentionServiceCtxErrBetweenBatches(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()
	if err := db.Create(&models.AuditLog{
		Action: "ctx-batch", PrincipalKind: "admin",
		ResourceType: "ticket", Route: "/t", Method: "POST",
		CreatedAt: now.Add(-400 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormRetentionService(db, 180*24*time.Hour, 1)
	deleted, err := svc.Cleanup(cancellationOnlyCtx{Context: context.Background()}, now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (full batch deleted before ctx check)", deleted)
	}
}
