package delivery

import (
	"context"
	"testing"

	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	svcmetrics "servify/apps/server/internal/observability/metrics"
)

func ticketMetricValue(t *testing.T, reg *svcmetrics.Registry, name string, want map[string]string) float64 {
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

func TestHandlerAdapterMetricsTicketLifecycle(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	reg := svcmetrics.NewRegistry()
	adapter := newTicketDeliveryAdapter(t, db, bus).AttachBusinessMetrics(svcmetrics.NewBusinessMetrics(reg))
	ctx := context.Background()
	seedTicketUser(t, db, 1, "alice")

	created, err := adapter.CreateTicket(ctx, &ticketcontract.CreateTicketRequest{
		Title:      "metric ticket",
		CustomerID: 1,
		Priority:   "high",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got := ticketMetricValue(t, reg, "tickets_created_total", map[string]string{
		"tenant_id": "default", "priority": "high",
	}); got != 1 {
		t.Fatalf("tickets_created_total = %v, want 1", got)
	}

	// 状态迁到 resolved 计一次；其后 resolved→closed 不再计。
	resolved := "resolved"
	if _, err := adapter.UpdateTicket(ctx, created.ID, &ticketcontract.UpdateTicketRequest{Status: &resolved}, 1); err != nil {
		t.Fatalf("UpdateTicket(resolved): %v", err)
	}
	if got := ticketMetricValue(t, reg, "tickets_resolved_total", map[string]string{
		"tenant_id": "default", "outcome": "resolved",
	}); got != 1 {
		t.Fatalf("tickets_resolved_total = %v, want 1", got)
	}

	closed := "closed"
	if _, err := adapter.UpdateTicket(ctx, created.ID, &ticketcontract.UpdateTicketRequest{Status: &closed}, 1); err != nil {
		t.Fatalf("UpdateTicket(closed): %v", err)
	}
	if got := ticketMetricValue(t, reg, "tickets_resolved_total", map[string]string{
		"tenant_id": "default", "outcome": "resolved",
	}); got != 1 {
		t.Fatalf("closed 不应再计 resolved，got %v", got)
	}
	if got := ticketMetricValue(t, reg, "tickets_created_total", map[string]string{
		"tenant_id": "default", "priority": "high",
	}); got != 1 {
		t.Fatalf("状态变更不应重复计 created，got %v", got)
	}
}

func TestHandlerAdapterMetricsNilReceiverAttach(t *testing.T) {
	var a *HandlerServiceAdapter
	if got := a.AttachBusinessMetrics(nil); got != nil {
		t.Fatalf("nil receiver Attach 应返回 nil，got %v", got)
	}
}

func TestHandlerAdapterMetricsNilMeterNoop(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)
	seedTicketUser(t, db, 1, "bob")

	if _, err := adapter.CreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
		Title:      "nil meter ticket",
		CustomerID: 1,
	}); err != nil {
		t.Fatalf("nil meter CreateTicket: %v", err)
	}
}
