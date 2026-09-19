package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/pion/webrtc/v4"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func execTrigger(t *testing.T, db *gorm.DB, stmt string) {
	t.Helper()
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

// ---- insert-error branches via BEFORE INSERT triggers ----

func TestSLA_InsertTriggerErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// CheckSLAViolation: violation insert fails
	db := newSLAErrorDB(t)
	svc := NewSLAService(db, logrus.New())
	cfg := &models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	late := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := db.Create(late).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_sla_ins BEFORE INSERT ON sla_violations BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if _, err := svc.CheckSLAViolation(ctx, late); err == nil {
		t.Fatal("expected violation insert error")
	}

	// CreateSLAViolation: insert fails after validations
	db2 := newSLAErrorDB(t)
	svc2 := NewSLAService(db2, logrus.New())
	ticket := &models.Ticket{Title: "T", CreatedAt: now, UpdatedAt: now}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	cfg2 := &models.SLAConfig{
		Name: "C2", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db2.Create(cfg2).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	execTrigger(t, db2, "CREATE TRIGGER blk_sla2 BEFORE INSERT ON sla_violations BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if err := svc2.CreateSLAViolation(ctx, &models.SLAViolation{
		TicketID: ticket.ID, SLAConfigID: cfg2.ID, ViolationType: "first_response",
		Deadline: now, ViolatedAt: now,
	}); err == nil {
		t.Fatal("expected violation create error")
	}
}

func TestSLA_ResolveTicketNonNotFound(t *testing.T) {
	db := newSLAErrorDB(t)
	svc := NewSLAService(db, logrus.New())
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if err := svc.ResolveViolationsByTicket(context.Background(), 1, nil); err == nil || err.Error() == "ticket not found" {
		t.Fatalf("expected non-notfound ticket error, got %v", err)
	}
}

// ---- webrtc additional branches ----

func TestWebRTC_HandleAnswerStateError(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	conn, err := s.CreatePeerConnection("sess-state")
	if err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	defer s.CloseConnection("sess-state") //nolint:errcheck

	// fresh connection is in stable state; applying an answer is an invalid transition
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	defer peer.Close() //nolint:errcheck
	svcOffer, err := conn.PeerConnection.CreateOffer(nil)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := peer.SetRemoteDescription(svcOffer); err == nil {
		// SDP may lack ice-ufrag; the goal is only to produce some answer payload
		answer, aerr := peer.CreateAnswer(nil)
		if aerr == nil {
			if err := s.HandleAnswer("sess-state", answer); err == nil {
				t.Fatal("expected invalid state transition error")
			}
			return
		}
	}
	// fallback: craft a syntactically valid answer that still violates the state machine
	if err := s.HandleAnswer("sess-state", webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n",
	}); err == nil {
		t.Fatal("expected SetRemoteDescription error on fresh connection")
	}
}

func TestWebRTC_HandleICECandidateWithoutRemote(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	if _, err := s.CreatePeerConnection("sess-ice"); err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	defer s.CloseConnection("sess-ice") //nolint:errcheck

	err := s.HandleICECandidate("sess-ice", webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 UDP 2130706431 192.168.1.1 5000 typ host",
	})
	if err == nil {
		t.Fatal("expected add candidate error without remote description")
	}
}

func TestWebRTC_SendDataChannelAfterClose(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	conn, err := s.CreatePeerConnection("sess-send")
	if err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	dc, err := conn.PeerConnection.CreateDataChannel("chan", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	conn.DataChannel = dc

	if err := conn.PeerConnection.Close(); err != nil {
		t.Fatalf("close pc: %v", err)
	}
	if err := s.SendDataChannelMessage("sess-send", "hello"); err == nil {
		t.Fatal("expected send error on closed channel")
	}
}

// ---- router misc ----

func TestMessageRouter_HandlePlatformMessages_AIError(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(&failingRouterAI{}, hub, nil)

	adapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage, 1)}
	r.RegisterPlatform(string(PlatformWeb), adapter)
	go r.handlePlatformMessages(string(PlatformWeb), adapter)

	adapter.msgChan <- UnifiedMessage{UserID: "u1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	time.Sleep(100 * time.Millisecond)
	close(adapter.msgChan)
	time.Sleep(20 * time.Millisecond)
}

func bcryptHash(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	return string(b), err
}

func TestSLA_UpdateConfigSaveTriggerError(t *testing.T) {
	db := newSLAErrorDB(t)
	svc := NewSLAService(db, logrus.New())
	ctx := context.Background()

	cfg, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_cfg_upd BEFORE UPDATE ON sla_configs BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc.UpdateSLAConfig(ctx, cfg.ID, &SLAConfigUpdateRequest{Name: stringPtr("B")}); err == nil ||
		!strings.Contains(err.Error(), "failed to update SLA config") {
		t.Fatalf("expected save error, got %v", err)
	}
}
