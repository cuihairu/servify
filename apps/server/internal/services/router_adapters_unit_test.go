package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

type scriptableAdapter struct {
	sendErr    error
	startErr   error
	stopErr    error
	msgChan    chan UnifiedMessage
	sendCalls  int
	stopCalls  int
	startCalls int
}

func (a *scriptableAdapter) SendMessage(chatID, message string) error {
	a.sendCalls++
	return a.sendErr
}

func (a *scriptableAdapter) ReceiveMessage() <-chan UnifiedMessage { return a.msgChan }

func (a *scriptableAdapter) GetPlatformType() PlatformType { return PlatformTelegram }

func (a *scriptableAdapter) Start() error {
	a.startCalls++
	return a.startErr
}

func (a *scriptableAdapter) Stop() error {
	a.stopCalls++
	return a.stopErr
}

func TestMessageRouter_StartStop(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(stubAI{reply: "ok"}, hub, nil)

	okAdapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage)}
	failAdapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage), startErr: errors.New("boom")}
	stopFail := &scriptableAdapter{msgChan: make(chan UnifiedMessage), stopErr: errors.New("stop boom")}

	r.RegisterPlatform("ok", okAdapter)
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if okAdapter.startCalls != 1 {
		t.Fatalf("expected adapter start, got %d", okAdapter.startCalls)
	}

	r.RegisterPlatform("fail", failAdapter)
	if err := r.Start(); err == nil {
		t.Fatal("expected start error propagation")
	}

	r.RegisterPlatform("stopfail", stopFail)
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop should not fail: %v", err)
	}
	if stopFail.stopCalls != 1 {
		t.Fatalf("expected stop call, got %d", stopFail.stopCalls)
	}

	r.UnregisterPlatform("ok")
	r.UnregisterPlatform("missing")
	if _, exists := r.platforms["ok"]; exists {
		t.Fatal("expected ok platform removed")
	}
}

func TestMessageRouter_HandlePlatformMessages(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(stubAI{reply: "ok"}, hub, nil)

	adapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage, 2)}
	r.RegisterPlatform("external", adapter)

	go r.handlePlatformMessages("external", adapter)

	adapter.msgChan <- UnifiedMessage{UserID: "u1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	adapter.msgChan <- UnifiedMessage{UserID: "u1", Content: "", Type: MessageTypeText, Timestamp: time.Now()}
	close(adapter.msgChan)
}

func TestMessageRouter_RouteMessage_External(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(stubAI{reply: "ok"}, hub, nil)

	msg := UnifiedMessage{UserID: "u1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	if err := r.routeMessage("telegram", msg); err == nil {
		t.Fatal("expected missing adapter error")
	}

	adapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage)}
	r.RegisterPlatform("telegram", adapter)
	if err := r.routeMessage("telegram", msg); err != nil {
		t.Fatalf("routeMessage external: %v", err)
	}
	if adapter.sendCalls != 1 {
		t.Fatalf("expected send, got %d", adapter.sendCalls)
	}

	adapter.sendErr = errors.New("send failed")
	if err := r.routeMessage("telegram", msg); err == nil {
		t.Fatal("expected send error propagation")
	}

	// web platform routes via websocket hub
	if err := r.routeMessage(string(PlatformWeb), msg); err != nil {
		t.Fatalf("routeMessage web: %v", err)
	}

	// AI failure surfaces error
	failing := NewMessageRouter(&failingRouterAI{}, hub, nil)
	if err := failing.routeMessage(string(PlatformWeb), msg); err == nil {
		t.Fatal("expected AI error")
	}
	if err := failing.routeMessage("telegram", msg); err == nil {
		t.Fatal("expected AI error for external route")
	}
}

type failingRouterAI struct{}

func (failingRouterAI) ProcessQuery(ctx context.Context, query string, sessionID string) (*AIResponse, error) {
	return nil, errors.New("ai down")
}
func (failingRouterAI) ShouldTransferToHuman(query string, _ []models.Message) bool { return false }
func (failingRouterAI) GetSessionSummary(_ []models.Message) (string, error)        { return "", nil }

func TestMessageRouter_BroadcastMessage_Errors(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(stubAI{reply: "ok"}, hub, nil)

	failing := &scriptableAdapter{sendErr: errors.New("nope"), msgChan: make(chan UnifiedMessage)}
	r.RegisterPlatform("bad", failing)
	if err := r.BroadcastMessage(UnifiedMessage{UserID: "u", Content: "c"}); err != nil {
		t.Fatalf("broadcast should swallow errors: %v", err)
	}
	if failing.sendCalls != 1 {
		t.Fatalf("expected send attempt, got %d", failing.sendCalls)
	}
}

func TestMessageRouter_EnsureSession_EdgeCases(t *testing.T) {
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)

	// nil db / empty session id short circuit
	if _, _, err := r.ensureSession("", "web", "t", "w"); err != nil {
		t.Fatalf("empty session shortcut: %v", err)
	}
	nilDBRouter := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), nil)
	if _, _, err := nilDBRouter.ensureSession("s", "web", "t", "w"); err != nil {
		t.Fatalf("nil db shortcut: %v", err)
	}

	// scope mismatch on tenant
	seed := &models.Session{ID: "mismatch", TenantID: "t1", WorkspaceID: "w1", Status: "active", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, _, err := r.ensureSession("mismatch", "web", "t2", "w1"); err == nil {
		t.Fatal("expected tenant mismatch error")
	}
	if _, _, err := r.ensureSession("mismatch", "web", "t1", "w2"); err == nil {
		t.Fatal("expected workspace mismatch error")
	}

	// existing session without scope is upgraded
	blank := &models.Session{ID: "blank", Status: "active", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(blank).Error; err != nil {
		t.Fatalf("seed blank session: %v", err)
	}
	tenant, ws, err := r.ensureSession("blank", "web", "t9", "w9")
	if err != nil {
		t.Fatalf("scope upgrade: %v", err)
	}
	if tenant != "t9" || ws != "w9" {
		t.Fatalf("unexpected upgraded scope: %q/%q", tenant, ws)
	}

	// tenant-only upgrade path
	blank2 := &models.Session{ID: "blank2", TenantID: "t-keep", Status: "active", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(blank2).Error; err != nil {
		t.Fatalf("seed blank2: %v", err)
	}
	if _, _, err := r.ensureSession("blank2", "web", "", "w8"); err != nil {
		t.Fatalf("workspace upgrade: %v", err)
	}

	// fresh session creation
	if _, _, err := r.ensureSession("fresh", "web", "", ""); err != nil {
		t.Fatalf("fresh session: %v", err)
	}
}

func TestMessageRouter_PersistMessage_NoUser(t *testing.T) {
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)

	msg := UnifiedMessage{UserID: "", PlatformID: "web", Content: "anon", Type: MessageTypeText, Timestamp: time.Now()}
	if err := r.persistMessage(msg); err != nil {
		t.Fatalf("persistMessage anonymous: %v", err)
	}
	var count int64
	if err := db.Model(&models.Message{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected 1 message, got %d (%v)", count, err)
	}

	// message insert error (drop table)
	if err := db.Migrator().DropTable("messages"); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	if err := r.persistMessage(msg); err == nil {
		t.Fatal("expected persist error after dropping messages table")
	}
}

func TestMetadataStringVariants(t *testing.T) {
	if got := metadataString(nil, "k"); got != "" {
		t.Fatalf("nil metadata: %q", got)
	}
	m := map[string]interface{}{
		"s":        "str",
		"nil":      nil,
		"stringer": stringerValue("sv"),
		"num":      42,
	}
	if got := metadataString(m, "s"); got != "str" {
		t.Fatalf("string: %q", got)
	}
	if got := metadataString(m, "nil"); got != "" {
		t.Fatalf("nil value: %q", got)
	}
	if got := metadataString(m, "stringer"); got != "sv" {
		t.Fatalf("stringer: %q", got)
	}
	if got := metadataString(m, "num"); got != "42" {
		t.Fatalf("number: %q", got)
	}
	if got := metadataString(m, "missing"); got != "" {
		t.Fatalf("missing: %q", got)
	}
	if tenant, ws := routerScopeFromMetadata(nil); tenant != "" || ws != "" {
		t.Fatalf("nil scope metadata: %q/%q", tenant, ws)
	}
}

type stringerValue string

func (s stringerValue) String() string { return string(s) }

func TestTelegramAdapter_Methods(t *testing.T) {
	adapter := NewTelegramAdapter("token", "chat")
	if adapter.GetPlatformType() != PlatformTelegram {
		t.Fatalf("unexpected platform: %s", adapter.GetPlatformType())
	}
	if err := adapter.SendMessage("chat", "msg"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if adapter.ReceiveMessage() == nil {
		t.Fatal("expected message channel")
	}
	if err := adapter.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := adapter.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestWeChatAdapter_Methods(t *testing.T) {
	adapter := NewWeChatAdapter("appID", "secret")
	if adapter.GetPlatformType() != PlatformWeChat {
		t.Fatalf("unexpected platform: %s", adapter.GetPlatformType())
	}
	if err := adapter.SendMessage("chat", "msg"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if adapter.ReceiveMessage() == nil {
		t.Fatal("expected message channel")
	}
	if err := adapter.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := adapter.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

var _ = fmt.Sprintf // keep fmt available for future assertions
