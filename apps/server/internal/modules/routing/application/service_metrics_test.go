package application

import (
	"context"
	"testing"

	"servify/apps/server/internal/modules/routing/domain"
	svcmetrics "servify/apps/server/internal/observability/metrics"
)

func routeMetricValue(t *testing.T, reg *svcmetrics.Registry, name string, want map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if len(labels) != len(want) {
				continue
			}
			matched := true
			for k, v := range want {
				if labels[k] != v {
					matched = false
					break
				}
			}
			if matched && m.GetCounter() != nil {
				return m.GetCounter().GetValue()
			}
		}
	}
	// 无样本视为 0（counter vec 惰性创建，未打点时 family 不出现）。
	return 0
}

func TestServiceMetricsRoutingDecisions(t *testing.T) {
	reg := svcmetrics.NewRegistry()
	repo := &stubRoutingRepo{queue: map[string]*domain.QueueEntry{}}
	svc := NewService(repo, nil).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))
	ctx := context.Background()

	if _, err := svc.AddToWaitingQueue(ctx, AddToWaitingQueueCommand{SessionID: "s1"}); err != nil {
		t.Fatalf("AddToWaitingQueue: %v", err)
	}
	if _, err := svc.AssignAgent(ctx, AssignAgentCommand{SessionID: "s1", AgentID: 9}); err != nil {
		t.Fatalf("AssignAgent: %v", err)
	}
	if _, err := svc.MarkWaitingTransferred(ctx, MarkWaitingTransferredCommand{SessionID: "s1", AssignedTo: 9}); err != nil {
		t.Fatalf("MarkWaitingTransferred: %v", err)
	}

	for strategy, outcome := range map[string]string{
		"handoff": "queued", "assign": "success", "transfer": "transferred",
	} {
		if got := routeMetricValue(t, reg, "routing_decisions_total", map[string]string{
			"tenant_id": "default", "strategy": strategy, "outcome": outcome,
		}); got != 1 {
			t.Fatalf("routing_decisions_total{strategy=%s} = %v, want 1", strategy, got)
		}
	}
}

func TestServiceMetricsRoutingFailureNotCounted(t *testing.T) {
	reg := svcmetrics.NewRegistry()
	repo := &stubRoutingRepo{err: context.Canceled}
	svc := NewService(repo, nil).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))

	if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: "s2"}); err == nil {
		t.Fatal("expected repo error")
	}
	if got := routeMetricValue(t, reg, "routing_decisions_total", map[string]string{
		"tenant_id": "default", "strategy": "handoff", "outcome": "queued",
	}); got != 0 {
		t.Fatalf("失败不应计数，got %v", got)
	}
}

func TestServiceMetricsNilReceiverAttach(t *testing.T) {
	var svc *Service
	if got := svc.AttachBusinessMetrics(nil); got != nil {
		t.Fatalf("nil receiver Attach 应返回 nil，got %v", got)
	}
}

func TestServiceMetricsNilMeterNoop(t *testing.T) {
	repo := &stubRoutingRepo{queue: map[string]*domain.QueueEntry{}}
	svc := NewService(repo, nil)
	if _, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{SessionID: "s3"}); err != nil {
		t.Fatalf("nil meter AddToWaitingQueue: %v", err)
	}
	if _, err := svc.AssignAgent(context.Background(), AssignAgentCommand{SessionID: "s3", AgentID: 4}); err != nil {
		t.Fatalf("nil meter AssignAgent: %v", err)
	}
}
