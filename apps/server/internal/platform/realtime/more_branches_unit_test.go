package realtime

import (
	"servify/apps/server/internal/platform/iceturn"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/pion/webrtc/v4"
)

func TestRouter_EnsureSessionTriggerErrors(t *testing.T) {
	// create-session insert fails while lookup succeeds
	db := newRealtimeTestDB(t, &models.Session{}, &models.Message{})
	execTrigger(t, db, "CREATE TRIGGER blk_sess_ins BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)
	if _, _, err := r.ensureSession("fresh", "web", "t", "w"); err == nil {
		t.Fatal("expected session create trigger error")
	}

	// scope upgrade update fails
	db2 := newRealtimeTestDB(t, &models.Session{}, &models.Message{})
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
	s := NewWebRTCService(iceturn.ICEConfig{}, hub) // invalid STUN config
	if _, err := s.HandleOffer("sess", webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0"}); err == nil {
		t.Fatal("expected peer connection creation error")
	}
}
