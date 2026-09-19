package application

// GetOverview 编排与错误包裹语义（自 services/workspace_service_unit_test.go 下沉）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
)

type stubRepo struct {
	totalActive int64
	waiting     int64
	channels    []ChannelRow
	online      int64
	busy        int64
	recent      []WorkspaceSession
	avg         float64
	errIdx      int // -1 不注入；否则第 N 个方法返回错误
	err         error
}

func (r *stubRepo) fail(step int) error {
	if r.errIdx == step {
		return r.err
	}
	return nil
}

func (r *stubRepo) CountActiveSessions(ctx context.Context) (int64, error) {
	return r.totalActive, r.fail(0)
}
func (r *stubRepo) CountWaitingSessions(ctx context.Context) (int64, error) {
	return r.waiting, r.fail(1)
}
func (r *stubRepo) AggregateChannels(ctx context.Context) ([]ChannelRow, error) {
	return r.channels, r.fail(2)
}
func (r *stubRepo) CountOnlineAgents(ctx context.Context) (int64, error) { return r.online, r.fail(3) }
func (r *stubRepo) CountBusyAgents(ctx context.Context) (int64, error)   { return r.busy, r.fail(4) }
func (r *stubRepo) RecentSessions(ctx context.Context, limit int) ([]WorkspaceSession, error) {
	return r.recent, r.fail(5)
}
func (r *stubRepo) AvgAgentResponseTime(ctx context.Context) float64 { return r.avg }

type stubAgentReader struct {
	agents []*agentdelivery.AgentInfo
}

func (r *stubAgentReader) GetOnlineAgents(ctx context.Context) []*agentdelivery.AgentInfo {
	return r.agents
}

func TestServiceGetOverviewAssembles(t *testing.T) {
	repo := &stubRepo{
		totalActive: 3, waiting: 1, online: 2, busy: 1, avg: 12.5,
		channels: []ChannelRow{{Platform: "web", Active: 3, Waiting: 1}},
		recent:   []WorkspaceSession{{ID: "s1", Platform: "web"}},
	}
	svc := NewService(repo, &stubAgentReader{agents: []*agentdelivery.AgentInfo{
		{UserID: 7, Name: " Agent One "},
		{UserID: 8, Username: "agent-two"},
		nil,
	}})

	overview, err := svc.GetOverview(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.TotalActiveSessions != 3 || overview.WaitingQueue != 1 || overview.OnlineAgents != 2 || overview.BusyAgents != 1 {
		t.Fatalf("unexpected counters: %+v", overview)
	}
	if len(overview.Channels) != 1 || overview.Channels[0].Platform != "web" || overview.Channels[0].AvgResponseTime != 12.5 {
		t.Fatalf("unexpected channels: %+v", overview.Channels)
	}
	if len(overview.RecentSessions) != 1 || overview.RecentSessions[0].ID != "s1" {
		t.Fatalf("unexpected recent sessions: %+v", overview.RecentSessions)
	}
	if overview.AgentStats == nil || len(overview.AgentStats.AvailableAgents) != 2 {
		t.Fatalf("unexpected agent stats: %+v", overview.AgentStats)
	}
	if overview.AgentStats.AvailableAgents[0].Name != " Agent One " || overview.AgentStats.AvailableAgents[1].Name != "agent-two" {
		t.Fatalf("unexpected available agents: %+v", overview.AgentStats.AvailableAgents)
	}
}

func TestServiceGetOverviewAgentReaderBranches(t *testing.T) {
	repo := &stubRepo{}
	// nil reader：AgentStats 保持 nil
	overview, err := NewService(repo, nil).GetOverview(context.Background(), 10)
	if err != nil || overview.AgentStats != nil {
		t.Fatalf("nil reader: %+v, %v", overview.AgentStats, err)
	}
	// 空在线列表：同样 nil
	overview, err = NewService(repo, &stubAgentReader{}).GetOverview(context.Background(), 10)
	if err != nil || overview.AgentStats != nil {
		t.Fatalf("empty reader: %+v, %v", overview.AgentStats, err)
	}
}

func TestServiceGetOverviewErrorWrapping(t *testing.T) {
	boom := errors.New("boom")
	steps := []struct {
		idx  int
		want string
	}{
		{0, "count active sessions: boom"},
		{1, "count waiting sessions: boom"},
		{2, "aggregate channels: boom"},
		{3, "count online agents: boom"},
		{4, "count busy agents: boom"},
		{5, "load recent sessions: boom"},
	}
	for _, step := range steps {
		svc := NewService(&stubRepo{errIdx: step.idx, err: boom}, nil)
		_, err := svc.GetOverview(context.Background(), 10)
		if err == nil || err.Error() != step.want {
			t.Fatalf("step %d err = %v, want %q", step.idx, err, step.want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty(" ", "b", "c"); got != "b" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonEmpty("", " "); got != "" {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains("x", "x") {
		t.Fatal("unreachable")
	}
}
