package services

import (
	"context"
	"testing"
	"time"
)

// TestStatisticsService_TimeRangeStatsError 覆盖区间统计底层扫描失败透传。
func TestStatisticsService_TimeRangeStatsError(t *testing.T) {
	svc := newStatisticsTestService(t)
	if err := svc.db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	now := time.Now()
	if _, err := svc.GetTimeRangeStats(context.Background(), now.Add(-24*time.Hour), now); err == nil {
		t.Fatal("expected time range stats error")
	}
}

// TestStatisticsService_RemoteAssistScanError 覆盖平均关闭时长聚合为 NULL 时的扫描失败。
func TestStatisticsService_RemoteAssistScanError(t *testing.T) {
	svc := newStatisticsTestService(t)
	// 无任何 remote assist 工单：AVG 聚合返回 NULL，Scan 到 float64 必然失败
	if _, err := svc.GetRemoteAssistTicketStats(context.Background()); err == nil {
		t.Fatal("expected NULL scan failure for empty remote assist set")
	}
}
