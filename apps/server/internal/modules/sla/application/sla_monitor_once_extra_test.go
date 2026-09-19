package application

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestRunMonitorOnceError 覆盖单轮监控的错误透传：tickets 表缺失让工单
// 扫描失败，RunMonitorOnce 原样返回错误（周期循环由 app/worker 驱动）。
func TestRunMonitorOnceError(t *testing.T) {
	db := newSLAUnitTestDB(t)
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	svc := NewService(db, logrus.New())

	if err := svc.RunMonitorOnce(context.Background()); err == nil {
		t.Fatal("want error when tickets table is missing")
	}
}
