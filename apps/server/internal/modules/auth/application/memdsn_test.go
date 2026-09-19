package application

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var memSQLiteDBSeq atomic.Uint32

// uniqueMemDSN 给命名内存库 DSN 追加全局唯一序号，
// 避免 -count 重跑、-run 反复执行或并行测试命中同一命名库。
func uniqueMemDSN(base string) string {
	return base + "_" + strconv.FormatUint(uint64(memSQLiteDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
}

var authTestDBSeq uint32

// newServicesTestDB 按需建表的最小内存库（自 services/coverage_helpers_test.go 复刻，
// OIDC provision 等只迁移部分模型的用例使用）。
func newServicesTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	dsn := "file:svc_unit_" + t.Name() + "_" + strconv.Itoa(int(atomic.AddUint32(&authTestDBSeq, 1))) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}
