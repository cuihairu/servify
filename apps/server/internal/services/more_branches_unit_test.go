package services

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	mockllm "servify/apps/server/internal/platform/llm/mock"

	"errors"
	"github.com/pion/webrtc/v4"
	"github.com/sirupsen/logrus"
)

func TestAIService_NewRequestError(t *testing.T) {
	svc := NewAIService("key", "http://127.0.0.1:1\x7f")
	if _, err := svc.callOpenAI(context.Background(), "p"); err == nil {
		t.Fatal("expected request creation error")
	}
}

func TestSatisfactionService_MoreErrorBranches(t *testing.T) {
	ctx := context.Background()

	// ScheduleSurvey: ticket validation query fails (non-notfound)
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, nil)
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.ScheduleSurvey(ctx, &models.Ticket{ID: 1}); err == nil || err.Error() == "ticket not found" {
		t.Fatalf("expected ticket query error, got %v", err)
	}

	// RespondSurvey: create fails without "already exists" marker
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
	ticket := &models.Ticket{Title: "T", CustomerID: customer.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	missingAgent := uint(999)
	survey := &models.SatisfactionSurvey{
		TicketID: ticket.ID, CustomerID: customer.ID, AgentID: &missingAgent,
		Status: "sent", SurveyToken: "tok-agent", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db2.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	if _, err := svc2.RespondSurvey(ctx, "tok-agent", 5, ""); err == nil {
		t.Fatal("expected agent-not-found error propagation")
	}

	// CreateSatisfaction: owner mismatch + agent query error
	db3 := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc3 := NewSatisfactionService(db3, nil)
	owner := &models.User{Username: "owner", Email: "owner@x.com", Role: "customer"}
	if err := db3.Create(owner).Error; err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := db3.Create(&models.Customer{UserID: owner.ID}).Error; err != nil {
		t.Fatalf("seed owner profile: %v", err)
	}
	other := &models.User{Username: "other", Email: "other@x.com", Role: "customer"}
	if err := db3.Create(other).Error; err != nil {
		t.Fatalf("seed other: %v", err)
	}
	if err := db3.Create(&models.Customer{UserID: other.ID}).Error; err != nil {
		t.Fatalf("seed other profile: %v", err)
	}
	ticket3 := &models.Ticket{Title: "T3", CustomerID: owner.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db3.Create(ticket3).Error; err != nil {
		t.Fatalf("seed ticket3: %v", err)
	}
	if _, err := svc3.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticket3.ID, CustomerID: other.ID, Rating: 5,
	}); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("expected owner mismatch error, got %v", err)
	}
	if err := db3.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := svc3.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticket3.ID, CustomerID: owner.ID, AgentID: &missingAgent, Rating: 5,
	}); err == nil {
		t.Fatal("expected agent query error")
	}
}

func TestSLAService_MoreBranches(t *testing.T) {
	ctx := context.Background()

	// config insert error
	db := newSLAErrorDB(t)
	svc := NewSLAService(db, logrus.New())
	execTrigger(t, db, "CREATE TRIGGER blk_cfg BEFORE INSERT ON sla_configs BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if _, err := svc.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	}); err == nil {
		t.Fatal("expected config insert error")
	}

	// priority-change conflict and combined priority+tier conflict
	db2 := newSLAErrorDB(t)
	svc2 := NewSLAService(db2, logrus.New())
	if _, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", CustomerTier: "vip",
		FirstResponseTime: 1, ResolutionTime: 60, EscalationTime: 30,
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "B", Priority: "high", FirstResponseTime: 2, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	// B (tier '') -> tier vip: combined conflict on priority+tier
	if _, err := svc2.UpdateSLAConfig(ctx, b.ID, &SLAConfigUpdateRequest{CustomerTier: stringPtr("vip")}); err == nil {
		t.Fatal("expected combined priority+tier conflict")
	}
	// low -> high conflicts with existing high config
	c, err := svc2.CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "C", Priority: "low", FirstResponseTime: 2, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil {
		t.Fatalf("create C: %v", err)
	}
	if _, err := svc2.UpdateSLAConfig(ctx, c.ID, &SLAConfigUpdateRequest{Priority: stringPtr("high")}); err == nil {
		t.Fatal("expected priority conflict")
	}
	// resolution + valid escalation assignments
	updated, err := svc2.UpdateSLAConfig(ctx, c.ID, &SLAConfigUpdateRequest{
		ResolutionTime: intPtr(90),
		EscalationTime: intPtr(45),
	})
	if err != nil {
		t.Fatalf("update C times: %v", err)
	}
	if updated.ResolutionTime != 90 || updated.EscalationTime != 45 {
		t.Fatalf("unexpected times: %+v", updated)
	}

	// monitor loop with per-ticket error (violations table dropped mid-flow)
	db3 := newSLAErrorDB(t)
	svc3 := NewSLAService(db3, logrus.New())
	now := time.Now()
	cfg := &models.SLAConfig{
		Name: "C", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db3.Create(cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	late := &models.Ticket{Title: "Late", Priority: "high", Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	if err := db3.Create(late).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db3.Migrator().DropTable("sla_violations"); err != nil {
		t.Fatalf("drop violations: %v", err)
	}
	if err := svc3.monitorSLAViolations(ctx); err != nil {
		t.Fatalf("monitor with ticket errors: %v", err)
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
