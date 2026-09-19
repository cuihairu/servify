package application

// 测试 helper 复刻（services 侧同名 helper 不跨包引用）。

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

// unitScopedContext builds a scoped context for unit tests
// （自 services/coverage_helpers_test.go 复刻）。
func unitScopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

var satisfactionTestDBSeq uint32

// newSatisfactionTestDB 建独立 DSN 的内存 SQLite 并迁移指定模型
// （自 services/coverage_helpers_test.go 的 newServicesTestDB 复刻）。
func newSatisfactionTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	dsn := "file:satisfaction_unit_" + t.Name() + "_" + fmt.Sprint(atomic.AddUint32(&satisfactionTestDBSeq, 1)) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func timeNow() time.Time { return time.Now() }

func uintPtr(v uint) *uint { return &v }

func timePtr(v time.Time) *time.Time { return &v }

func intPtr(v int) *int { return &v }
