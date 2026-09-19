package application

// 测试 helper 复刻（services 侧同名 helper 不跨包引用；指针/上下文 helper
// 与 satisfaction/application 的 test_helpers_test.go 同源）。

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var slaTestDBSeq uint32

// newSLATestDB 建独立 DSN 的内存 SQLite 并迁移指定模型。
func newSLATestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	dsn := "file:sla_unit_" + t.Name() + "_" + fmt.Sprint(atomic.AddUint32(&slaTestDBSeq, 1)) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// unitScopedContext builds a scoped context for unit tests.
func unitScopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func timeNow() time.Time             { return time.Now() }
func uintPtr(v uint) *uint           { return &v }
func intPtr(v int) *int              { return &v }
func boolPtr(v bool) *bool           { return &v }
func timePtr(v time.Time) *time.Time { return &v }
func stringPtr(v string) *string     { return &v }
