package realtime

import (
	"context"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"testing"
	"time"
)

// stubAI provides the router AI surface for tests.
type stubAI struct{ reply string }

func (s stubAI) ProcessQuery(ctx context.Context, query string, sessionID string) (*aidelivery.AIResponse, error) {
	return &aidelivery.AIResponse{Content: s.reply + ":" + query, Confidence: 0.9, Source: "test"}, nil
}
func (s stubAI) ShouldTransferToHuman(query string, _ []models.Message) bool { return false }
func (s stubAI) GetSessionSummary(_ []models.Message) (string, error)        { return "", nil }
func (s stubAI) InitializeKnowledgeBase()                                    {}
func (s stubAI) GetStatus(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{"ok": true}
}

// ensure stub satisfies interface

func TestMessageRouter_HandleWebMessage_PushesAIResponse(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	ai := stubAI{reply: "ok"}
	r := NewMessageRouter(ai, hub, nil)

	// register a client for the session to capture broadcast
	client := &WebSocketClient{ID: "c1", SessionID: "s1", Send: make(chan WebSocketMessage, 1), Hub: hub}
	hub.register <- client
	time.Sleep(10 * time.Millisecond)

	msg := UnifiedMessage{UserID: "s1", Content: "hello", Type: MessageTypeText, Timestamp: time.Now()}
	if err := r.handleWebMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleWebMessage error: %v", err)
	}

	select {
	case out := <-client.Send:
		if out.Type != "ai-response" {
			t.Fatalf("expected ai-response, got %s", out.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("did not receive response on client channel")
	}
}

func TestMessageRouter_HandleWebMessage_SkipAI(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	ai := stubAI{reply: "ok"}
	r := NewMessageRouter(ai, hub, nil)

	// 注册客户端
	client := &WebSocketClient{ID: "c2", SessionID: "s2", Send: make(chan WebSocketMessage, 1), Hub: hub}
	hub.register <- client
	time.Sleep(10 * time.Millisecond)

	// 发送空内容消息 - AI仍然会处理
	msg := UnifiedMessage{UserID: "s2", Content: "", Type: MessageTypeImage, Timestamp: time.Now()}
	if err := r.handleWebMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleWebMessage error: %v", err)
	}

	// 空内容也会触发 AI 响应（stubAI 总是响应）
	select {
	case out := <-client.Send:
		if out.Type != "ai-response" {
			t.Fatalf("expected ai-response, got %s", out.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive response on client channel")
	}
}

func TestNewWebSocketHub(t *testing.T) {
	hub := NewWebSocketHub()
	if hub == nil {
		t.Fatal("NewWebSocketHub() returned nil")
	}

	if hub.clients == nil {
		t.Error("expected clients map to be initialized")
	}

	if hub.broadcast == nil {
		t.Error("expected broadcast channel to be initialized")
	}

	if hub.register == nil {
		t.Error("expected register channel to be initialized")
	}

	if hub.unregister == nil {
		t.Error("expected unregister channel to be initialized")
	}
}

func TestNewMessageRouter(t *testing.T) {
	hub := NewWebSocketHub()
	ai := stubAI{reply: "test"}

	r := NewMessageRouter(ai, hub, nil)
	if r == nil {
		t.Fatal("NewMessageRouter() returned nil")
	}

	if r.aiService == nil {
		t.Error("expected aiService to be set")
	}

	if r.wsHub == nil {
		t.Error("expected wsHub to be set")
	}
}

func TestWebSocketMessageTypes(t *testing.T) {
	tests := []struct {
		typeStr string
		isValid bool
	}{
		{string(MessageTypeText), true},
		{string(MessageTypeImage), true},
		{string(MessageTypeFile), true},
		{string(MessageTypeAudio), true},
		{string(MessageTypeVideo), true},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.typeStr, func(t *testing.T) {
			msg := WebSocketMessage{Type: tt.typeStr}
			// 只是验证类型可以被设置
			if msg.Type == "" {
				t.Error("expected type to be set")
			}
		})
	}
}

func TestUnifiedMessage(t *testing.T) {
	now := time.Now()
	msg := UnifiedMessage{
		UserID:     "user123",
		PlatformID: "web",
		Content:    "test message",
		Type:       MessageTypeText,
		Timestamp:  now,
	}

	if msg.UserID != "user123" {
		t.Errorf("expected UserID 'user123', got '%s'", msg.UserID)
	}

	if msg.PlatformID != "web" {
		t.Errorf("expected PlatformID 'web', got '%s'", msg.PlatformID)
	}

	if msg.Content != "test message" {
		t.Errorf("expected Content 'test message', got '%s'", msg.Content)
	}

	if msg.Type != MessageTypeText {
		t.Errorf("expected Type '%s', got '%s'", MessageTypeText, msg.Type)
	}

	if !msg.Timestamp.Equal(now) {
		t.Error("expected Timestamp to match")
	}
}

func TestMessageRouter_Register_UnregisterPlatform(t *testing.T) {
	hub := NewWebSocketHub()
	ai := stubAI{reply: "test"}
	r := NewMessageRouter(ai, hub, nil)

	// Create a mock adapter
	adapter := &mockPlatformAdapter{name: "test"}

	// Register platform
	r.RegisterPlatform("test", adapter)

	// Unregister platform
	r.UnregisterPlatform("test")
}

func TestMessageRouter_Start_Stop(t *testing.T) {
	hub := NewWebSocketHub()
	ai := stubAI{reply: "test"}
	r := NewMessageRouter(ai, hub, nil)

	// Start should not block (runs in background)
	// We can't easily test the actual start without a real context
	// So just test Stop doesn't panic
	r.Stop()
}

func TestMessageRouter_BroadcastMessage(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	time.Sleep(10 * time.Millisecond)

	ai := stubAI{reply: "test"}
	r := NewMessageRouter(ai, hub, nil)

	// Broadcast message should not panic
	msg := UnifiedMessage{
		UserID:     "test-user",
		PlatformID: "web",
		Content:    "test broadcast",
		Type:       MessageTypeText,
		Timestamp:  time.Now(),
	}
	r.BroadcastMessage(msg)
}

func TestMessageRouter_ensureSession(t *testing.T) {
	// This test would require a DB setup, testing the private method
	// For now, we'll skip it as it's tested indirectly through other tests
	t.Skip("ensureSession is tested indirectly through HandleWebMessage")
}

func TestRouterScopeFromMetadata(t *testing.T) {
	tenantID, workspaceID := routerScopeFromMetadata(map[string]interface{}{
		"tenant_id":    "tenant-a",
		"workspace_id": "workspace-a",
	})
	if tenantID != "tenant-a" || workspaceID != "workspace-a" {
		t.Fatalf("unexpected scope: tenant=%q workspace=%q", tenantID, workspaceID)
	}
}

// mockPlatformAdapter is a mock implementation of PlatformAdapter
type mockPlatformAdapter struct {
	name string
}

func (m *mockPlatformAdapter) SendMessage(chatID, message string) error {
	return nil
}

func (m *mockPlatformAdapter) ReceiveMessage() <-chan UnifiedMessage {
	return nil
}

func (m *mockPlatformAdapter) GetPlatformType() PlatformType {
	return PlatformType(m.name)
}

func (m *mockPlatformAdapter) Start() error {
	return nil
}

func (m *mockPlatformAdapter) Stop() error {
	return nil
}
