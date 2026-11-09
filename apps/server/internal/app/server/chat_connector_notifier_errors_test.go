package server

// chatConnectorNotifier 的三条未覆盖守卫：未装配安装行仓储、仓储读失败、
// 请求体校验失败（空内容）。

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/channel"
)

func TestChatConnectorNotifierWithoutStoreErrors(t *testing.T) {
	var nilNotifier *chatConnectorNotifier
	if _, err := nilNotifier.Notify(context.Background(), "t", "w", "memex", "inst-1", channel.NotifyRequest{
		Target: "group:5", Reason: "maintenance", IdempotencyKey: "op-1",
		Content: channel.SendContent{Text: "down"},
	}); err == nil || !strings.Contains(err.Error(), "notifier") {
		t.Fatalf("nil receiver must fail with notifier error, got %v", err)
	}

	notifier := &chatConnectorNotifier{}
	if _, err := notifier.Notify(context.Background(), "t", "w", "memex", "inst-1", channel.NotifyRequest{
		Target: "group:5", Reason: "maintenance", IdempotencyKey: "op-1",
		Content: channel.SendContent{Text: "down"},
	}); err == nil || !strings.Contains(err.Error(), "installation store not configured") {
		t.Fatalf("missing installation store must fail, got %v", err)
	}
}

func TestChatConnectorNotifierStoreReadErrorSurfaces(t *testing.T) {
	tokenEnv := "SERVIFY_TEST_NOTIFY_DBERR_TOKEN"
	rt := newChatNotifierRuntime(t, "http://127.0.0.1:1", "memex", tokenEnv, true, true)
	t.Setenv(tokenEnv, "tok")

	sqlDB, err := rt.DB.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if _, err := rt.chatNotifier.Notify(context.Background(), "tenant-1", "workspace-1", "memex", "inst-1", channel.NotifyRequest{
		Target: "group:5", Reason: "maintenance", IdempotencyKey: "op-1",
		Content: channel.SendContent{Text: "down"},
	}); err == nil {
		t.Fatal("installation store failure must surface")
	}
}

func TestChatConnectorNotifierRejectsInvalidRequest(t *testing.T) {
	tokenEnv := "SERVIFY_TEST_NOTIFY_VALIDATE_TOKEN"
	rt := newChatNotifierRuntime(t, "http://127.0.0.1:1", "memex", tokenEnv, true, true)
	t.Setenv(tokenEnv, "tok")

	// 空内容（无 text/content parts）：目标钉行后仍过不了请求校验。
	_, err := rt.chatNotifier.Notify(context.Background(), "tenant-1", "workspace-1", "memex", "inst-1", channel.NotifyRequest{
		Target: "group:5", Reason: "maintenance", IdempotencyKey: "op-1",
	})
	if err == nil || !strings.Contains(err.Error(), "send content requires") {
		t.Fatalf("empty content must fail validation, got %v", err)
	}
}
