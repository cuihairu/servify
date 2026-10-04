package bootstrap

// V1.0 收敛 B2-4：default scope 回填测试（sqlite 走 Go 路径；postgres
// versioned SQL 迁移的表清单同步由 TestScopeBackfillSQLCoversSameTables
// 把关）。

import (
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newScopeBackfillDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&models.Session{}, &models.Message{}, &models.ConversationEvent{},
		&models.Ticket{},
		&models.Customer{}, &models.KnowledgeDoc{},
		&models.TransferRecord{}, &models.WaitingRecord{}, &models.RoutingAssignment{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// 空库：天然 no-op，全部 0 行、无错误（验收闸"空库"态）。
func TestScopeBackfillEmptyDatabase(t *testing.T) {
	db := newScopeBackfillDB(t)

	counts, err := BackfillDefaultScope(db)
	if err != nil {
		t.Fatalf("BackfillDefaultScope() error = %v", err)
	}
	if len(counts) != len(ScopeBackfillTables) {
		t.Fatalf("counts size = %d want %d", len(counts), len(ScopeBackfillTables))
	}
	for table, n := range counts {
		if n != 0 {
			t.Fatalf("empty db touched %s: %d rows", table, n)
		}
	}
}

// 既有库：空 scope 行回填为 default、部分 scope 保留已有值、非空行不动；
// 幂等重跑零改动（验收闸"既有库"态 + 可逆性）。
func TestScopeBackfillExistingRows(t *testing.T) {
	db := newScopeBackfillDB(t)

	now := time.Now()
	seeds := []func() error{
		func() error { return db.Create(&models.Session{ID: "s1", Status: "active", StartedAt: now}).Error },
		func() error { return db.Create(&models.Message{SessionID: "s1", Content: "hi", Sender: "user"}).Error },
		func() error { return db.Create(&models.Ticket{Title: "t1", CustomerID: 1, Status: "open"}).Error },
		func() error { return db.Create(&models.Customer{UserID: 1, Company: "acme"}).Error },
		func() error { return db.Create(&models.TransferRecord{SessionID: "s1"}).Error },
		func() error { return db.Create(&models.WaitingRecord{SessionID: "s1", Status: "waiting"}).Error },
		func() error { return db.Create(&models.RoutingAssignment{SessionID: "s1", TotalScore: 1}).Error },
	}
	for i, seed := range seeds {
		if err := seed(); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	// 部分 scope：tenant 已带值，workspace 为空 → 只补 workspace。
	partial := &models.Session{ID: "s2", Status: "active", StartedAt: now, TenantID: "acme"}
	if err := db.Create(partial).Error; err != nil {
		t.Fatalf("seed partial: %v", err)
	}
	// 非 default 租户的行完全不动。
	scoped := &models.Ticket{Title: "t2", CustomerID: 1, Status: "open", TenantID: "acme", WorkspaceID: "ws1"}
	if err := db.Create(scoped).Error; err != nil {
		t.Fatalf("seed scoped: %v", err)
	}

	counts, err := BackfillDefaultScope(db)
	if err != nil {
		t.Fatalf("BackfillDefaultScope() error = %v", err)
	}
	// sessions 表：s1 两列 + s2 一列 = 3 行改动；完全带 scope 的行不计入。
	if counts["sessions"] != 3 {
		t.Fatalf("sessions backfill count = %d want 3", counts["sessions"])
	}

	var s1 models.Session
	if err := db.First(&s1, "id = ?", "s1").Error; err != nil {
		t.Fatalf("load s1: %v", err)
	}
	if s1.TenantID != DefaultScopeTenantID || s1.WorkspaceID != DefaultScopeWorkspaceID {
		t.Fatalf("s1 scope = %q/%q want default/default", s1.TenantID, s1.WorkspaceID)
	}
	var s2 models.Session
	if err := db.First(&s2, "id = ?", "s2").Error; err != nil {
		t.Fatalf("load s2: %v", err)
	}
	if s2.TenantID != "acme" || s2.WorkspaceID != DefaultScopeWorkspaceID {
		t.Fatalf("s2 scope = %q/%q want acme/default（已有租户必须保留）", s2.TenantID, s2.WorkspaceID)
	}
	var t2 models.Ticket
	if err := db.First(&t2, "title = ?", "t2").Error; err != nil {
		t.Fatalf("load t2: %v", err)
	}
	if t2.TenantID != "acme" || t2.WorkspaceID != "ws1" {
		t.Fatalf("t2 scope = %q/%q want acme/ws1（非空行必须不动）", t2.TenantID, t2.WorkspaceID)
	}

	// 幂等：第二次执行全部 0。
	second, err := BackfillDefaultScope(db)
	if err != nil {
		t.Fatalf("second BackfillDefaultScope() error = %v", err)
	}
	for table, n := range second {
		if n != 0 {
			t.Fatalf("idempotent rerun touched %s: %d rows", table, n)
		}
	}
}

// nil 防御：nil db 返回空结果不 panic。
func TestScopeBackfillNilDB(t *testing.T) {
	counts, err := BackfillDefaultScope(nil)
	if err != nil {
		t.Fatalf("BackfillDefaultScope(nil) error = %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty counts, got %v", counts)
	}
}

// TestScopeBackfillSQLCoversSameTables 把关 versioned SQL 迁移（postgres
// 路径）与 Go 回填（sqlite 路径）的表清单一致：SQL 文件必须为清单中每张表
// 提供两列的回填语句。
func TestScopeBackfillSQLCoversSameTables(t *testing.T) {
	sqlBytes, err := migrationsFS.ReadFile("migrations/000017_scope_backfill.up.sql")
	if err != nil {
		t.Fatalf("read scope backfill migration: %v", err)
	}
	sqlText := string(sqlBytes)
	for _, table := range ScopeBackfillTables {
		for _, column := range []string{"tenant_id", "workspace_id"} {
			statement := "UPDATE " + table + " SET " + column
			if !strings.Contains(sqlText, statement) {
				t.Fatalf("migration 000017 missing %q（表清单与 ScopeBackfillTables 不同步）", statement)
			}
		}
	}
}
