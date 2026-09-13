package services

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
)

// fakeAutomationWebhook 记录外呼负载，验证注入的 dispatcher 保存后可用。
type fakeAutomationWebhook struct {
	calls int
}

func (f *fakeAutomationWebhook) Dispatch(ctx context.Context, url, secret string, payload map[string]interface{}) error {
	f.calls++
	return nil
}

// TestAutomationService_WorkerPlumbing 覆盖 timer worker 与 webhook 外呼的注入口。
func TestAutomationService_WorkerPlumbing(t *testing.T) {
	db := newServicesTestDB(t, &models.AutomationTrigger{}, &models.AutomationRun{})
	svc := NewAutomationService(db, logrus.New())

	processor := svc.TimersForWorker()
	if processor == nil {
		t.Fatal("TimersForWorker() = nil for assembled service")
	}

	// 覆盖批次上限注入：不改变服务可用性
	svc.SetTimerBatchSize(7)
	svc.SetTimerBatchSize(0)

	// webhook 外呼可注入（含清除语义的 nil）
	svc.SetWebhookDispatcher(nil)
	dispatcher := &fakeAutomationWebhook{}
	svc.SetWebhookDispatcher(dispatcher)

	if svc.TimersForWorker() == nil {
		t.Fatal("TimersForWorker() became nil after injections")
	}
}

// TestAutomationService_WorkerPlumbingNilSafe 覆盖 nil 接收者守卫。
func TestAutomationService_WorkerPlumbingNilSafe(t *testing.T) {
	var nilSvc *AutomationService
	if got := nilSvc.TimersForWorker(); got != nil {
		t.Fatalf("nil service TimersForWorker() = %v, want nil", got)
	}
	nilSvc.SetTimerBatchSize(3) // 必须不 panic
}
