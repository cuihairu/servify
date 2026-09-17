package application

import (
	"context"
	"testing"

	"servify/apps/server/internal/modules/conversation/domain"
	svcmetrics "servify/apps/server/internal/observability/metrics"
)

func convMetricValue(t *testing.T, reg *svcmetrics.Registry, name string, want map[string]string) float64 {
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

func TestServiceMetricsConversationCreated(t *testing.T) {
	reg := svcmetrics.NewRegistry()
	svc := NewService(&stubConversationRepo{}, nil).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))

	if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
		ConversationID: "conv-m1",
		Channel:        domain.ChannelBinding{Channel: "web"},
	}); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if got := convMetricValue(t, reg, "conversations_created_total", map[string]string{
		"tenant_id": "default", "channel": "web",
	}); got != 1 {
		t.Fatalf("conversations_created_total = %v, want 1", got)
	}
}

func TestServiceMetricsConversationCreatedFailureNotCounted(t *testing.T) {
	reg := svcmetrics.NewRegistry()
	repo := &stubConversationRepo{err: context.Canceled}
	svc := NewService(repo, nil).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))

	if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
		ConversationID: "conv-m2",
		Channel:        domain.ChannelBinding{Channel: "web"},
	}); err == nil {
		t.Fatal("expected repo error")
	}
	if got := convMetricValue(t, reg, "conversations_created_total", map[string]string{
		"tenant_id": "default", "channel": "web",
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
	svc := NewService(&stubConversationRepo{}, nil)
	if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
		ConversationID: "conv-m3",
		Channel:        domain.ChannelBinding{Channel: "email"},
	}); err != nil {
		t.Fatalf("nil meter CreateConversation: %v", err)
	}
}
