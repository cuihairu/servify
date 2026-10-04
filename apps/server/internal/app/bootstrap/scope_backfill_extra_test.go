package bootstrap

// 覆盖补充：回填遇库错误原样上抛（带表与列定位），不吞错继续。

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestScopeBackfillUpdateErrorPropagates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 不建表：清单首表 sessions 缺失，回填的 UPDATE 报错并带表与列定位。
	_, err = BackfillDefaultScope(db)
	if err == nil {
		t.Fatal("expected error for missing table")
	}
	if !strings.Contains(err.Error(), "backfill sessions.tenant_id") {
		t.Fatalf("err = %v", err)
	}
}
