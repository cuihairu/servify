package application

import (
	"context"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"
)

// 事件租户与仓储写入同源（auth ctx）：有租户 ctx 必盖章，无租户 ctx
// （系统 worker）留空且不得误填。
func TestNewTicketEventStampsTenantFromContext(t *testing.T) {
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	event := NewTicketEvent(ctx, TicketCreatedEventName, TicketDTO{ID: 7})
	if got := event.TenantID(); got != "tenant-a" {
		t.Fatalf("expected tenant-a, got %q", got)
	}

	bare := NewTicketEvent(context.Background(), TicketCreatedEventName, TicketDTO{ID: 7})
	if got := bare.TenantID(); got != "" {
		t.Fatalf("expected empty tenant without scoped ctx, got %q", got)
	}
}
