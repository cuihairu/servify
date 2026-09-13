package infra

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

// 覆盖 GetTimeRangeStats 中各聚合查询的错误分支（countInto 闭包的
// Scan 失败与 DailyStats/CSAT 查询失败）。
func TestGormRepositoryTimeRangeStatsScanErrorBranches(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start
	ctx := context.Background()

	expectErr := func(t *testing.T, mutate func(db *gorm.DB), ctx context.Context, wantFragment string) {
		t.Helper()
		db := newAnalyticsScopeTestDB(t)
		repo := NewGormRepository(db)
		if mutate != nil {
			mutate(db)
		}
		_, err := repo.GetTimeRangeStats(ctx, start, end)
		if err == nil {
			t.Fatal("expected query error")
		}
		if wantFragment != "" && !strings.Contains(err.Error(), wantFragment) {
			t.Fatalf("expected error containing %q, got %v", wantFragment, err)
		}
	}

	t.Run("tickets created_at scan fails", func(t *testing.T) {
		expectErr(t, func(db *gorm.DB) {
			if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
				t.Fatalf("drop tickets: %v", err)
			}
		}, ctx, "")
	})

	t.Run("sessions created_at scan fails", func(t *testing.T) {
		expectErr(t, func(db *gorm.DB) {
			if err := db.Migrator().DropTable(&models.Session{}); err != nil {
				t.Fatalf("drop sessions: %v", err)
			}
		}, ctx, "")
	})

	t.Run("messages created_at scan fails", func(t *testing.T) {
		expectErr(t, func(db *gorm.DB) {
			if err := db.Migrator().DropTable(&models.Message{}); err != nil {
				t.Fatalf("drop messages: %v", err)
			}
		}, ctx, "")
	})

	t.Run("tickets resolved_at scan fails", func(t *testing.T) {
		// 建一张只有 created_at 的替身表：created_at 聚合通过，resolved_at 聚合报错。
		expectErr(t, func(db *gorm.DB) {
			if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
				t.Fatalf("drop tickets: %v", err)
			}
			if err := db.Exec("CREATE TABLE tickets (id INTEGER PRIMARY KEY, created_at DATETIME, deleted_at DATETIME)").Error; err != nil {
				t.Fatalf("create stub tickets: %v", err)
			}
		}, ctx, "resolved_at")
	})

	t.Run("global daily stats query fails", func(t *testing.T) {
		expectErr(t, func(db *gorm.DB) {
			if err := db.Migrator().DropTable(&models.DailyStats{}); err != nil {
				t.Fatalf("drop daily_stats: %v", err)
			}
		}, ctx, "")
	})

	t.Run("scoped csat query fails", func(t *testing.T) {
		scoped := platformauth.ContextWithScope(context.Background(), "tenant-1", "ws-1")
		expectErr(t, func(db *gorm.DB) {
			if err := db.Migrator().DropTable(&models.CustomerSatisfaction{}); err != nil {
				t.Fatalf("drop customer_satisfactions: %v", err)
			}
		}, scoped, "")
	})
}

// renamedDialector 把底层 Dialector 的 Name() 换成非 sqlite，覆盖 dayExpr 的默认分支。
type renamedDialector struct {
	gorm.Dialector
}

func (d renamedDialector) Name() string { return "postgres" }

func TestDayExprDialectBranches(t *testing.T) {
	nonSQLite := &gorm.DB{Config: &gorm.Config{Dialector: renamedDialector{Dialector: sqlite.Open("file::memory:")}}}
	got := dayExpr(nonSQLite, "created_at")
	if got != "TO_CHAR(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD')" {
		t.Fatalf("unexpected non-sqlite day expr: %q", got)
	}

	sqliteDB := &gorm.DB{Config: &gorm.Config{Dialector: sqlite.Open("file::memory:")}}
	if got := dayExpr(sqliteDB, "created_at"); got != "strftime('%Y-%m-%d', created_at)" {
		t.Fatalf("unexpected sqlite day expr: %q", got)
	}
}
