package infra

// 远程协助工单统计的行为对账与错误分支（自 services facade 下沉）。

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
)

// failNthAnalyticsQuery registers gorm callbacks that force the n-th SELECT
// (including raw Scan/Row reads) on this connection to fail.
func failNthAnalyticsQuery(db *gorm.DB, n int32) {
	var calls int32
	bump := func(tx *gorm.DB) {
		if atomic.AddInt32(&calls, 1) == n {
			_ = tx.AddError(errors.New("forced nth query failure"))
		}
	}
	_ = db.Callback().Query().Before("gorm:query").Register("fail_nth", bump)
	_ = db.Callback().Row().Before("gorm:row").Register("fail_nth_row", bump)
}

func TestGormRepositoryRemoteAssistTicketStats(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	ctx := scopedAnalyticsContext("t1", "w1")

	closedAt := time.Now().Add(-time.Hour)
	created := time.Now().Add(-2 * time.Hour)
	tickets := []models.Ticket{
		{Title: "ra-open", Source: "remote_assist", Status: "open", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created},
		{Title: "ra-resolved", Category: "remote-assist", Status: "resolved", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created, ResolvedAt: &closedAt},
		{Title: "ra-closed", Tags: "remote_assist", Status: "closed", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created, ClosedAt: &closedAt},
		{Title: "other", Status: "open", TenantID: "t1", WorkspaceID: "w1", CreatedAt: created, UpdatedAt: created},
	}
	for i := range tickets {
		if err := db.Create(&tickets[i]).Error; err != nil {
			t.Fatalf("seed ticket: %v", err)
		}
	}

	stats, err := repo.GetRemoteAssistTicketStats(ctx)
	if err != nil {
		t.Fatalf("GetRemoteAssistTicketStats: %v", err)
	}
	if stats.Total != 3 || stats.Open != 1 || stats.Resolved != 1 || stats.Closed != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ResolvedRate != 1.0/3.0 {
		t.Fatalf("unexpected resolved rate: %v", stats.ResolvedRate)
	}
	if stats.AvgCloseHours <= 0 {
		t.Fatalf("expected avg close hours > 0, got %v", stats.AvgCloseHours)
	}

	// unscoped context exercises the no-tenant/workspace branch
	if _, err := repo.GetRemoteAssistTicketStats(context.Background()); err != nil {
		t.Fatalf("unscoped GetRemoteAssistTicketStats: %v", err)
	}
}

// 空集（无任何 remote assist 工单）必须返回零值统计而非 NULL 扫描错误。
func TestGormRepositoryRemoteAssistTicketStatsEmpty(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)

	stats, err := repo.GetRemoteAssistTicketStats(context.Background())
	if err != nil {
		t.Fatalf("empty set should not error: %v", err)
	}
	if stats.Total != 0 || stats.AvgCloseHours != 0 || stats.ResolvedRate != 0 {
		t.Fatalf("expected zeroed stats, got %+v", stats)
	}
}

// n 遍历全部五次查询（4 个 Count + avg-close Scan）：avg-close 分支失败
// 必须包装为 "failed to scan remote assist close duration" 而非裸错。
// （历史注释曾因 Row() 无 nil guard 跳过 n=5——源码改用 Scan 后已安全。）
func TestGormRepositoryRemoteAssistSequentialErrors(t *testing.T) {
	for n := int32(2); n <= 5; n++ {
		db := newAnalyticsScopeTestDB(t)
		failNthAnalyticsQuery(db, n)
		repo := NewGormRepository(db)
		if _, err := repo.GetRemoteAssistTicketStats(context.Background()); err == nil {
			t.Fatalf("n=%d: expected error", n)
		}
	}
}

func TestGormRepositoryRemoteAssistDroppedTableError(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	repo := NewGormRepository(db)
	if _, err := repo.GetRemoteAssistTicketStats(context.Background()); err == nil {
		t.Fatal("expected remote assist error on dropped tickets table")
	}
}
