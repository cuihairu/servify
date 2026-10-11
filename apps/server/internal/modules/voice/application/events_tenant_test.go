package application

import (
	"context"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"
)

// 事件租户与仓储写入同源（auth ctx）：有租户 ctx 必盖章，无租户 ctx
// （系统 worker）留空且不得误填。
func TestNewVoiceEventStampsTenantFromContext(t *testing.T) {
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	event := NewVoiceEvent(ctx, CallStartedEventName, "voice:call-1", nil)
	if got := event.TenantID(); got != "tenant-a" {
		t.Fatalf("expected tenant-a, got %q", got)
	}

	bare := NewVoiceEvent(context.Background(), CallStartedEventName, "voice:call-1", nil)
	if got := bare.TenantID(); got != "" {
		t.Fatalf("expected empty tenant without scoped ctx, got %q", got)
	}
}
