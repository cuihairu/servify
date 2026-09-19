package delivery

// HandlerServiceAdapter 纯转发对账 + 错误传播。

import (
	"context"
	"errors"
	"testing"

	workspaceapp "servify/apps/server/internal/modules/workspace/application"
)

type adapterStubRepo struct {
	overview *workspaceapp.WorkspaceOverview
	err      error
}

func (r *adapterStubRepo) CountActiveSessions(ctx context.Context) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) CountWaitingSessions(ctx context.Context) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) AggregateChannels(ctx context.Context) ([]workspaceapp.ChannelRow, error) {
	return nil, r.err
}

func (r *adapterStubRepo) CountOnlineAgents(ctx context.Context) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) CountBusyAgents(ctx context.Context) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) RecentSessions(ctx context.Context, limit int) ([]workspaceapp.WorkspaceSession, error) {
	return nil, r.err
}

func (r *adapterStubRepo) AvgAgentResponseTime(ctx context.Context) float64 { return 0 }

func TestHandlerServiceAdapter(t *testing.T) {
	ctx := context.Background()
	want := errors.New("boom")
	repo := &adapterStubRepo{err: want}
	svc := workspaceapp.NewService(repo, nil)
	var handler HandlerService = NewHandlerServiceAdapter(svc)

	if _, err := handler.GetOverview(ctx, 10); !errors.Is(err, want) {
		t.Fatalf("GetOverview() err = %v, want %v", err, want)
	}

	repo.err = nil
	overview, err := handler.GetOverview(ctx, 10)
	if err != nil || overview == nil {
		t.Fatalf("GetOverview() = %+v, %v", overview, err)
	}
}
