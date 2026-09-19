package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// ---- constructor nil-logger branches ----

func TestConstructors_NilLogger(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Customer{})
	if NewSLAService(db, nil) == nil {
		t.Fatal("expected SLA service")
	}
}

// ---- ai extras ----

func TestAIService_ProcessQuery_Error(t *testing.T) {
	svc := NewAIService("key", "https://invalid.example.invalid")
	if _, err := svc.ProcessQuery(context.Background(), "q", "s"); err == nil {
		t.Fatal("expected ProcessQuery error from failed OpenAI call")
	}
}

func TestAIService_ShTransferToHuman_HistoryLength(t *testing.T) {
	svc := NewAIService("", "")
	history := make([]models.Message, 6)
	if !svc.ShouldTransferToHuman("plain question", history) {
		t.Fatal("expected long history to trigger transfer")
	}
}

// ---- app integration error branches ----

// ---- custom field error branches ----

// ---- shift error branches ----

// ---- satisfaction error branches ----

func TestMessageRouter_RouteMessage_PersistErrorContinues(t *testing.T) {
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	if err := db.Migrator().DropTable("messages"); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(stubAI{reply: "ok"}, hub, db)
	r.RegisterPlatform("telegram", &scriptableAdapter{msgChan: make(chan UnifiedMessage)})

	msg := UnifiedMessage{UserID: "u1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	if err := r.routeMessage("telegram", msg); err != nil {
		t.Fatalf("routeMessage should continue after persist error: %v", err)
	}
}

func TestMessageRouter_EnsureSession_CreateError(t *testing.T) {
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	if err := db.Migrator().DropTable("sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)
	if _, _, err := r.ensureSession("fresh", "web", "t", "w"); err == nil {
		t.Fatal("expected session create error")
	}
}

func TestMessageRouter_HandlePlatformMessages_Waits(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(stubAI{reply: "ok"}, hub, nil)

	adapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage, 1)}
	r.RegisterPlatform("external", adapter)
	go r.handlePlatformMessages("external", adapter)
	adapter.msgChan <- UnifiedMessage{UserID: "u1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	time.Sleep(50 * time.Millisecond)
	close(adapter.msgChan)
	time.Sleep(20 * time.Millisecond)
}
