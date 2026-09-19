package realtime

import (
	"context"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// stub AI to avoid external calls
type stubAI2 struct{ reply string }

func (s stubAI2) ProcessQuery(ctx context.Context, query string, sessionID string) (*aidelivery.AIResponse, error) {
	return &aidelivery.AIResponse{Content: s.reply, Confidence: 0.9, Source: "test"}, nil
}
func (s stubAI2) ShouldTransferToHuman(query string, _ []models.Message) bool { return false }

// Implement the router AI surface used in tests.
func (s stubAI2) GetSessionSummary(_ []models.Message) (string, error) { return "", nil }
func (s stubAI2) InitializeKnowledgeBase()                             {}
func (s stubAI2) GetStatus(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{"ok": true}
}

// compile-time assert

func TestMessageRouter_RouteMessage_DBNil_FallbackAndContinue(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := stubAI2{reply: "ok"}
	r := NewMessageRouter(ai, hub, nil) // db = nil -> persist fallback (log only)

	// register a client to capture broadcast
	client := &WebSocketClient{ID: "c1", SessionID: "s1", Send: make(chan WebSocketMessage, 1), Hub: hub}
	hub.register <- client
	time.Sleep(10 * time.Millisecond)

	msg := UnifiedMessage{UserID: "s1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	if err := r.routeMessage(string(PlatformWeb), msg); err != nil {
		t.Fatalf("routeMessage error: %v", err)
	}

	select {
	case out := <-client.Send:
		if out.Type != "ai-response" {
			t.Fatalf("expected ai-response, got %s", out.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("no response delivered to client")
	}
}

func newRouterPersistTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestMessageRouter_PersistMessage_StoresScope(t *testing.T) {
	db := newRouterPersistTestDB(t)
	r := NewMessageRouter(stubAI2{reply: "ok"}, NewWebSocketHub(), db)

	msg := UnifiedMessage{
		UserID:     "session-a",
		PlatformID: "web",
		Content:    "hi",
		Type:       MessageTypeText,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"tenant_id":    "tenant-a",
			"workspace_id": "workspace-a",
		},
	}
	if err := r.persistMessage(msg); err != nil {
		t.Fatalf("persistMessage error: %v", err)
	}

	var session models.Session
	if err := db.First(&session, "id = ?", "session-a").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.TenantID != "tenant-a" || session.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected session scope: %+v", session)
	}

	var stored models.Message
	if err := db.First(&stored).Error; err != nil {
		t.Fatalf("load message: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected message scope: %+v", stored)
	}
}

func TestMessageRouter_PersistMessage_RejectsCrossWorkspaceReuse(t *testing.T) {
	db := newRouterPersistTestDB(t)
	r := NewMessageRouter(stubAI2{reply: "ok"}, NewWebSocketHub(), db)

	first := UnifiedMessage{
		UserID:     "session-shared",
		PlatformID: "web",
		Content:    "a",
		Type:       MessageTypeText,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"tenant_id":    "tenant-a",
			"workspace_id": "workspace-a",
		},
	}
	if err := r.persistMessage(first); err != nil {
		t.Fatalf("persist first message: %v", err)
	}

	second := UnifiedMessage{
		UserID:     "session-shared",
		PlatformID: "web",
		Content:    "b",
		Type:       MessageTypeText,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"tenant_id":    "tenant-a",
			"workspace_id": "workspace-b",
		},
	}
	if err := r.persistMessage(second); err == nil {
		t.Fatal("expected cross-workspace session reuse to fail")
	}

	var count int64
	if err := db.Model(&models.Message{}).Count(&count).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected only first message persisted, got %d", count)
	}
}

func TestMessageRouter_PersistMessage_InheritsScopeFromExistingSession(t *testing.T) {
	db := newRouterPersistTestDB(t)
	r := NewMessageRouter(stubAI2{reply: "ok"}, NewWebSocketHub(), db)

	first := UnifiedMessage{
		UserID:     "session-inherit",
		PlatformID: "web",
		Content:    "a",
		Type:       MessageTypeText,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"tenant_id":    "tenant-a",
			"workspace_id": "workspace-a",
		},
	}
	if err := r.persistMessage(first); err != nil {
		t.Fatalf("persist first message: %v", err)
	}

	second := UnifiedMessage{
		UserID:     "session-inherit",
		PlatformID: "web",
		Content:    "b",
		Type:       MessageTypeText,
		Timestamp:  time.Now(),
	}
	if err := r.persistMessage(second); err != nil {
		t.Fatalf("persist second message: %v", err)
	}

	var messages []models.Message
	if err := db.Order("id asc").Find(&messages).Error; err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[1].TenantID != "tenant-a" || messages[1].WorkspaceID != "workspace-a" {
		t.Fatalf("expected inherited scope on second message, got %+v", messages[1])
	}
}
