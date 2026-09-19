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

func TestShift_InsertTriggerError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.ShiftSchedule{})
	svc := NewShiftService(db, logrus.New())
	ctx := context.Background()

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_shift BEFORE INSERT ON shift_schedules BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	start := time.Now().Add(time.Hour)
	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{
		AgentID: user.ID, ShiftType: "morning", StartTime: start, EndTime: start.Add(time.Hour),
	}); err == nil {
		t.Fatal("expected shift insert error")
	}
}

func TestSatisfaction_InsertUpdateTriggerErrors(t *testing.T) {
	ctx := context.Background()

	// survey create fails after reuse-check
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, nil)
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_survey BEFORE INSERT ON satisfaction_surveys BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if _, err := svc.ScheduleSurvey(ctx, ticket); err == nil {
		t.Fatal("expected survey insert error")
	}

	// RespondSurvey: satisfaction ok but survey status update fails
	db2 := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc2 := NewSatisfactionService(db2, nil)
	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer"}
	if err := db2.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db2.Create(&models.Customer{UserID: customer.ID}).Error; err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ticket2 := &models.Ticket{Title: "T2", CustomerID: customer.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db2.Create(ticket2).Error; err != nil {
		t.Fatalf("seed ticket2: %v", err)
	}
	future := time.Now().Add(time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: ticket2.ID, CustomerID: customer.ID, Status: "sent", SurveyToken: "tok-trig",
		SentAt: &future, ExpiresAt: &future, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db2.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	execTrigger(t, db2, "CREATE TRIGGER blk_survey_upd BEFORE UPDATE ON satisfaction_surveys BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	sat, err := svc2.RespondSurvey(ctx, "tok-trig", 4, "ok")
	if err != nil {
		t.Fatalf("RespondSurvey should tolerate status update failure: %v", err)
	}
	if sat == nil || sat.Rating != 4 {
		t.Fatalf("unexpected satisfaction: %+v", sat)
	}

	// ResendSurvey save failure
	db3 := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc3 := NewSatisfactionService(db3, nil)
	survey3 := &models.SatisfactionSurvey{
		TicketID: 1, CustomerID: 1, Status: "sent", SurveyToken: "tok-trig3",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db3.Create(survey3).Error; err != nil {
		t.Fatalf("seed survey3: %v", err)
	}
	execTrigger(t, db3, "CREATE TRIGGER blk_survey3 BEFORE UPDATE ON satisfaction_surveys BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc3.ResendSurvey(ctx, survey3.ID); err == nil {
		t.Fatal("expected resend save error")
	}

	// UpdateSatisfaction save failure + preload miss
	db4 := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc4 := NewSatisfactionService(db4, nil)
	row := &models.CustomerSatisfaction{TicketID: 9, CustomerID: 9, Rating: 5, CreatedAt: time.Now()}
	if err := db4.Create(row).Error; err != nil {
		t.Fatalf("seed satisfaction: %v", err)
	}
	execTrigger(t, db4, "CREATE TRIGGER blk_sat_upd BEFORE UPDATE ON customer_satisfactions BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc4.UpdateSatisfaction(ctx, row.ID, "new"); err == nil {
		t.Fatal("expected update save error")
	}
}

func TestShift_UpdateTriggerError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.ShiftSchedule{})
	svc := NewShiftService(db, logrus.New())
	ctx := context.Background()

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	start := time.Now().Add(time.Hour)
	shift, err := svc.CreateShift(ctx, &ShiftCreateRequest{
		AgentID: user.ID, ShiftType: "morning", StartTime: start, EndTime: start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateShift: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_shift_upd BEFORE UPDATE ON shift_schedules BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc.UpdateShift(ctx, shift.ID, &ShiftUpdateRequest{Status: stringPtr("active")}); err == nil {
		t.Fatal("expected shift update error")
	}
}

func TestAuthService_RotateTriggerError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	cfg := testAuthConfig()
	svc := NewAuthService(db, cfg)

	hash, err := bcryptHash("pw123456")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.Create(&models.User{
		ID: 71, Username: "u71", Email: "u71@x.com", Password: hash, Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{ID: "sess-71", UserID: 71, Status: "active", TokenVersion: 0}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_sess BEFORE UPDATE ON user_auth_sessions BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")

	tok, err := createHS256JWT(map[string]interface{}{
		"token_use": "refresh", "user_id": float64(71), "session_id": "sess-71",
		"session_token_version": float64(0), "token_version": float64(0),
		"iat": float64(time.Now().Unix()),
	}, cfg.JWT.Secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if _, err := svc.RefreshToken(context.Background(), tok, AuthSessionMetadata{}); err != ErrAuthInvalidRefreshToken {
		t.Fatalf("expected rotate update failure, got %v", err)
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
