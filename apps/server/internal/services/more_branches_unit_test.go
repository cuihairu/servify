package services

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	mockllm "servify/apps/server/internal/platform/llm/mock"

	"errors"
	"github.com/pion/webrtc/v4"
)

func TestAIService_NewRequestError(t *testing.T) {
	svc := NewAIService("key", "http://127.0.0.1:1\x7f")
	if _, err := svc.callOpenAI(context.Background(), "p"); err == nil {
		t.Fatal("expected request creation error")
	}
}

func TestOrchestratedAI_FallbackErrorBranch(t *testing.T) {
	server := httptest.NewServer(nil)
	url := server.URL
	server.Close()

	base := NewAIService("key", url) // fallback path also fails
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: errors.New("llm down")},
		&mockkp.Provider{},
		"",
		nil,
		"kb",
		nil,
	)
	if _, err := svc.ProcessQueryEnhanced(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected fallback error propagation")
	}
}

func TestRouter_EnsureSessionTriggerErrors(t *testing.T) {
	// create-session insert fails while lookup succeeds
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	execTrigger(t, db, "CREATE TRIGGER blk_sess_ins BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)
	if _, _, err := r.ensureSession("fresh", "web", "t", "w"); err == nil {
		t.Fatal("expected session create trigger error")
	}

	// scope upgrade update fails
	db2 := newServicesTestDB(t, &models.Session{}, &models.Message{})
	if err := db2.Create(&models.Session{ID: "blank", Status: "active", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed blank session: %v", err)
	}
	execTrigger(t, db2, "CREATE TRIGGER blk_sess_upd BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	r2 := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db2)
	if _, _, err := r2.ensureSession("blank", "web", "t1", "w1"); err == nil {
		t.Fatal("expected session update trigger error")
	}
}

func TestWebRTC_HandleOfferCreateError(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	s := NewWebRTCService("", hub) // invalid STUN config
	if _, err := s.HandleOffer("sess", webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0"}); err == nil {
		t.Fatal("expected peer connection creation error")
	}
}
