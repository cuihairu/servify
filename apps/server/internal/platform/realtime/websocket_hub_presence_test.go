package realtime

import (
	"testing"
	"time"
)

// TestWebSocketHubIsSessionConnected 锚定推送下发的在线抑制判定：
// clients 键是连接 ID，IsSessionConnected 按 SessionID 遍历匹配。
func TestWebSocketHubIsSessionConnected(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	if hub.IsSessionConnected("sess-online") {
		t.Fatal("empty hub must report disconnected")
	}

	client := &WebSocketClient{
		ID:        "conn-1", // 连接 ID 与会话 ID 不同源（映射按 SessionID）
		SessionID: "sess-online",
		Send:      make(chan WebSocketMessage, 4),
		Hub:       hub,
	}
	hub.register <- client
	waitRegistered(t, hub, 1)

	if !hub.IsSessionConnected("sess-online") {
		t.Fatal("registered session must report connected")
	}
	if hub.IsSessionConnected("sess-other") {
		t.Fatal("other session must report disconnected")
	}

	hub.unregister <- client
	waitRegistered(t, hub, 0)
	if hub.IsSessionConnected("sess-online") {
		t.Fatal("unregistered session must report disconnected")
	}
}

func waitRegistered(t *testing.T, hub *WebSocketHub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.GetClientCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("client count stuck at %d, want %d", hub.GetClientCount(), want)
}
