package application

import (
	"context"
	"fmt"
	"testing"

	"servify/apps/server/internal/modules/conversation/domain"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	platformauth "servify/apps/server/internal/platform/auth"
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

// TestServiceMetricsTenantAttribution 修正会话指标租户归因（todo 196）：
// 租户从可信作用域（auth ctx 盖章）读取——两个作用域各建会话各记各的；
// 缺失作用域按明确策略回落 default（单租户兜底口径，与事件盖章/仓储落列
// 同源），不再无条件记 default。
func TestServiceMetricsTenantAttribution(t *testing.T) {
	reg := svcmetrics.NewRegistry()
	svc := NewService(&stubConversationRepo{}, nil).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))

	scoped := map[string]context.Context{
		"tenant-a": platformauth.ContextWithScope(context.Background(), "tenant-a", ""),
		"tenant-b": platformauth.ContextWithScope(context.Background(), "tenant-b", ""),
	}
	for tenant, ctx := range scoped {
		if _, err := svc.CreateConversation(ctx, CreateConversationCommand{
			ConversationID: "conv-" + tenant,
			Channel:        domain.ChannelBinding{Channel: "web"},
		}); err != nil {
			t.Fatalf("CreateConversation %s: %v", tenant, err)
		}
	}
	// 缺失作用域：明确回落 default。
	if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
		ConversationID: "conv-noscope",
		Channel:        domain.ChannelBinding{Channel: "web"},
	}); err != nil {
		t.Fatalf("CreateConversation no scope: %v", err)
	}
	for _, tenant := range []string{"tenant-a", "tenant-b", "default"} {
		if got := convMetricValue(t, reg, "conversations_created_total", map[string]string{
			"tenant_id": tenant, "channel": "web",
		}); got != 1 {
			t.Fatalf("tenant %s conversations_created_total = %v, want 1", tenant, got)
		}
	}
}

// TestServiceMetricsChannelLabelBounded 渠道标签限幅（todo 196）：已知渠道
// 大小写归一放行，未知自由字符串收敛进 other——客户端撑不爆 Prometheus
// 标签基数（会话落库的渠道原值不受影响，仅限指标标签）。
func TestServiceMetricsChannelLabelBounded(t *testing.T) {
	reg := svcmetrics.NewRegistry()
	svc := NewService(&stubConversationRepo{}, nil).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))

	channels := []string{"WEB", "wechat", "whatsapp", "voice", "telegram", "email", "client-sent-arbitrary-\U0001F680"}
	for i, ch := range channels {
		if _, err := svc.CreateConversation(context.Background(), CreateConversationCommand{
			ConversationID: fmt.Sprintf("conv-c%d", i),
			Channel:        domain.ChannelBinding{Channel: ch},
		}); err != nil {
			t.Fatalf("CreateConversation %q: %v", ch, err)
		}
	}
	for _, label := range []string{"web", "wechat", "whatsapp", "voice", "telegram", "email", "other"} {
		if got := convMetricValue(t, reg, "conversations_created_total", map[string]string{
			"tenant_id": "default", "channel": label,
		}); got != 1 {
			t.Fatalf("channel label %s = %v, want 1", label, got)
		}
	}
	// 原始自由字符串不得成为标签。
	if got := convMetricValue(t, reg, "conversations_created_total", map[string]string{
		"tenant_id": "default", "channel": "client-sent-arbitrary-\U0001F680",
	}); got != 0 {
		t.Fatalf("raw channel leaked into labels: %v", got)
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
