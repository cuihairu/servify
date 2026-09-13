package usersecurity

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// 死分支说明(service.go GetUsers 内 byID[userID] 未命中分支,123 行):
// 该分支逻辑上不可达——Find 前已检查 len(users)==len(orderedIDs) 且
// users 的 id 集合 ⊆ orderedIDs(WHERE id IN ? 过滤)、主键无重复,
// 故 byID 必然覆盖 orderedIDs 全集;而 userIDs 的去重集合恰为 orderedIDs,
// 循环中的 byID[userID] 恒命中。除非数据库对同一主键返回两行(违反约束),
// 否则 `ok == false` 不可能成立。防御性代码,无法确定性测试。

// cancellationOnlyCtx 只向调用方暴露"已取消"状态,但 Done() 返回 nil,
// database/sql 因此把它当作不可取消的 context,查询照常执行;
// Cleanup 批间检查的 ctx.Err() 却返回 context.Canceled。
// 用于确定性覆盖 retention.go 批间取消分支(真实取消会让第一条
// Pluck 直接失败,永远走不到批间检查)。
type cancellationOnlyCtx struct {
	context.Context
}

func (cancellationOnlyCtx) Done() <-chan struct{} { return nil }
func (cancellationOnlyCtx) Err() error            { return context.Canceled }

// TestRetentionCleanupCtxErrBetweenBatches 覆盖 Cleanup 在删满一整批后
// 检查到 ctx 取消的返回分支。
func TestRetentionCleanupCtxErrBetweenBatches(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	if err := db.Create(&models.RevokedToken{JTI: "ctx-batch-1", ExpiresAt: &past, RevokedAt: now}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewGormRevokedTokenRetentionService(db, 1)
	deleted, err := svc.Cleanup(cancellationOnlyCtx{Context: context.Background()}, now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (full batch deleted before ctx check)", deleted)
	}
}

// TestListRevokedTokensCountError 覆盖 Count 查询失败的传播分支。
func TestListRevokedTokensCountError(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	if err := db.Migrator().DropTable(&models.RevokedToken{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	svc := NewService(db, nil)
	if _, _, err := svc.ListRevokedTokens(context.Background(), RevokedTokenListQuery{}); err == nil {
		t.Fatal("expected count failure on missing table")
	}
}

// TestListRevokedTokensFindError 覆盖 Count 成功后 Find 失败的传播分支:
// 注册在 gorm:query 之后的回调只对带 ORDER BY 的语句(Find)注入错误,
// Count 不带 ORDER BY,因此先正常完成。
func TestListRevokedTokensFindError(t *testing.T) {
	db := openScopeTestDB(t, &models.RevokedToken{})
	if err := db.Create(&models.RevokedToken{JTI: "find-err-1", RevokedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("gap_find_error", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Clauses["ORDER BY"]; ok {
			tx.Error = errors.New("find failed")
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	svc := NewService(db, nil)
	items, total, err := svc.ListRevokedTokens(context.Background(), RevokedTokenListQuery{})
	if err == nil {
		t.Fatal("expected find failure")
	}
	// 失败路径整体返回 (nil, 0, err),Count 的结果不外泄。
	if total != 0 || items != nil {
		t.Fatalf("find failure must discard results: total=%d items=%v", total, items)
	}
}

// TestServiceGetUsersFindError 覆盖 GetUsers 的 Find 失败分支
// (无请求 scope 时 Find 是唯一查询,直接替换 query 回调注入错误)。
func TestServiceGetUsersFindError(t *testing.T) {
	db := openScopeTestDB(t, &models.User{})
	_ = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		tx.Error = errors.New("users find failed")
	})
	svc := NewService(db, nil)
	if _, err := svc.GetUsers(context.Background(), []uint{5}); err == nil {
		t.Fatal("expected GetUsers find failure")
	}
}

// TestServiceRevokeSessionReloadError 覆盖 RevokeSession 更新成功后
// 重新加载会话失败的分支:Update 回调正常执行,Query 回调注入错误。
func TestServiceRevokeSessionReloadError(t *testing.T) {
	db := openScopeTestDB(t, &models.UserAuthSession{})
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&models.User{ID: 141, Username: "u141", Email: "u141@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "sess-141", UserID: 141, Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_ = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		tx.Error = errors.New("reload failed")
	})

	svc := NewService(db, nil)
	if _, err := svc.RevokeSession(context.Background(), 141, "sess-141"); err == nil {
		t.Fatal("expected reload failure after successful update")
	}
}

// TestServiceRevokeAllSessionsUpdateError 覆盖批量吊销 UPDATE 失败分支。
func TestServiceRevokeAllSessionsUpdateError(t *testing.T) {
	db := openScopeTestDB(t, &models.UserAuthSession{})
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&models.User{ID: 151, Username: "u151", Email: "u151@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "sess-151", UserID: 151, Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_ = db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.Error = errors.New("bulk update failed")
	})

	svc := NewService(db, nil)
	if _, err := svc.RevokeAllSessions(context.Background(), 151, ""); err == nil {
		t.Fatal("expected bulk update failure")
	}
}

// TestServiceRevokeAllSessionsAfterFindError 覆盖吊销后回查会话失败的分支:
// 按 query 回调调用序号注入错误(第 1 次是吊销前的列表查询,第 2 次是吊销后
// 的回查),两段查询均确定性执行。
func TestServiceRevokeAllSessionsAfterFindError(t *testing.T) {
	db := openScopeTestDB(t, &models.UserAuthSession{})
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&models.User{ID: 161, Username: "u161", Email: "u161@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "sess-161", UserID: 161, Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	calls := 0
	if err := db.Callback().Query().After("gorm:query").Register("gap_after_find_error", func(tx *gorm.DB) {
		calls++
		if calls >= 2 {
			tx.Error = errors.New("after find failed")
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	svc := NewService(db, nil)
	if _, err := svc.RevokeAllSessions(context.Background(), 161, ""); err == nil {
		t.Fatal("expected after-revoke find failure")
	}
}
