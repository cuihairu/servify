package services

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// TestStartSLAMonitorTickErrors 覆盖 ticker 触发一次监控并吞掉监控错误后继续循环，
// 再由 ctx 取消退出。
func TestStartSLAMonitorTickErrors(t *testing.T) {
	db := newSLAUnitTestDB(t)
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	svc := NewSLAService(db, logrus.New())

	// 第一次查询（监控里的工单扫描）触发信号，证明 tick 已执行
	tickSeen := make(chan struct{}, 1)
	if err := db.Callback().Query().Before("gorm:query").Register("test:sla_tick_signal", func(tx *gorm.DB) {
		select {
		case tickSeen <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		svc.StartSLAMonitor(ctx, 5*time.Millisecond)
		close(done)
	}()

	select {
	case <-tickSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor tick never ran")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StartSLAMonitor did not stop after cancel")
	}
}
