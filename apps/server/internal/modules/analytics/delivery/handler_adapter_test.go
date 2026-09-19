package delivery

// HandlerServiceAdapter 的转发覆盖：module 返回类型即 contract 类型
//（别名），逐方法对账透传语义与错误传播。

import (
	"context"
	"errors"
	"testing"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
)

func TestHandlerServiceAdapter_AllMethods(t *testing.T) {
	repo := &runnerStubRepo{}
	adapter := NewHandlerServiceAdapter(analyticsapp.NewService(repo))
	if adapter == nil {
		t.Fatal("expected adapter instance")
	}
	ctx := context.Background()
	now := time.Now()

	dash, err := adapter.GetDashboardStats(ctx)
	if err != nil || dash == nil {
		t.Fatalf("GetDashboardStats: %v %+v", err, dash)
	}
	if tr, err := adapter.GetTimeRangeStats(ctx, now.Add(-time.Hour), now); err != nil || tr != nil {
		t.Fatalf("GetTimeRangeStats: %v %v", tr, err)
	}
	if ap, err := adapter.GetAgentPerformanceStats(ctx, now.Add(-time.Hour), now, 5); err != nil || ap != nil {
		t.Fatalf("GetAgentPerformanceStats: %v %v", ap, err)
	}
	if cs, err := adapter.GetTicketCategoryStats(ctx, now.Add(-time.Hour), now); err != nil || cs != nil {
		t.Fatalf("GetTicketCategoryStats: %v %v", cs, err)
	}
	if ps, err := adapter.GetTicketPriorityStats(ctx, now.Add(-time.Hour), now); err != nil || ps != nil {
		t.Fatalf("GetTicketPriorityStats: %v %v", ps, err)
	}
	if src, err := adapter.GetCustomerSourceStats(ctx); err != nil || src != nil {
		t.Fatalf("GetCustomerSourceStats: %v %v", src, err)
	}
	if ra, err := adapter.GetRemoteAssistTicketStats(ctx); err != nil || ra == nil {
		t.Fatalf("GetRemoteAssistTicketStats: %v %+v", err, ra)
	}
	if err := adapter.UpdateDailyStats(ctx, now); err != nil {
		t.Fatalf("UpdateDailyStats: %v", err)
	}
}

func TestHandlerServiceAdapter_ErrorPropagation(t *testing.T) {
	boom := errors.New("boom: remote assist")
	repoErr := &failingRemoteAssistRepo{err: boom}
	errAdapter := NewHandlerServiceAdapter(analyticsapp.NewService(repoErr))
	if _, err := errAdapter.GetRemoteAssistTicketStats(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("expected remote assist error propagation, got %v", err)
	}
}

// failingRemoteAssistRepo 让 GetRemoteAssistTicketStats 失败。
type failingRemoteAssistRepo struct {
	runnerStubRepo
	err error
}

func (r *failingRemoteAssistRepo) GetRemoteAssistTicketStats(ctx context.Context) (*analyticscontract.RemoteAssistTicketStats, error) {
	return nil, r.err
}
