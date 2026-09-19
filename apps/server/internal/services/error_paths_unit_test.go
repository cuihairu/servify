package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
)

// ---- constructor nil-logger branches ----

func TestConstructors_NilLogger(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Customer{})
	if NewSatisfactionService(db, nil) == nil {
		t.Fatal("expected satisfaction service")
	}
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

func TestSatisfactionService_DroppedTableErrors(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, logrus.New())
	ctx := context.Background()

	if err := db.Migrator().DropTable("satisfaction_surveys"); err != nil {
		t.Fatalf("drop surveys: %v", err)
	}
	if _, err := svc.GetSurveyPreviewByToken(ctx, "tok"); err == nil || err == ErrSurveyNotFound {
		t.Fatalf("expected load error, got %v", err)
	}
	if _, err := svc.RespondSurvey(ctx, "tok", 5, ""); err == nil || err == ErrSurveyNotFound {
		t.Fatalf("expected load error, got %v", err)
	}
	if _, err := svc.ResendSurvey(ctx, 1); err == nil || err == ErrSurveyNotFound {
		t.Fatalf("expected load error, got %v", err)
	}
	if _, _, err := svc.ListSurveys(ctx, &SatisfactionSurveyListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list surveys error")
	}

	if err := db.Migrator().DropTable("customer_satisfactions"); err != nil {
		t.Fatalf("drop satisfactions: %v", err)
	}
	if _, err := svc.GetSatisfaction(ctx, 1); err == nil {
		t.Fatal("expected get satisfaction error")
	}
	if _, _, err := svc.ListSatisfactions(ctx, &SatisfactionListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list satisfactions error")
	}
	if _, err := svc.GetSatisfactionByTicket(ctx, 1); err == nil {
		t.Fatal("expected get by ticket error")
	}
	if _, err := svc.GetSatisfactionStats(ctx, nil, nil); err == nil {
		t.Fatal("expected stats error")
	}
	if err := svc.DeleteSatisfaction(ctx, 1); err == nil {
		t.Fatal("expected delete error")
	}
	if _, err := svc.UpdateSatisfaction(ctx, 1, "c"); err == nil {
		t.Fatal("expected update error")
	}
}

func TestSatisfactionService_ScheduleSurveyCountError(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, logrus.New())
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Migrator().DropTable("customer_satisfactions"); err != nil {
		t.Fatalf("drop satisfactions: %v", err)
	}
	if _, err := svc.ScheduleSurvey(context.Background(), ticket); err == nil {
		t.Fatal("expected satisfaction count error")
	}
}

func TestSatisfactionService_TicketLoadErrors(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, logrus.New())
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: 1, CustomerID: 1, Status: "sent", SurveyToken: "tok-err", SentAt: &past, ExpiresAt: &past,
		CreatedAt: past, UpdatedAt: past,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.GetSurveyPreviewByToken(ctx, "tok-err"); err == nil {
		t.Fatal("expected ticket preload error")
	}
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: 1, CustomerID: 1, Rating: 5}); err == nil {
		t.Fatal("expected ticket lookup error")
	}
}

// ---- statistics error branches ----

// ---- router extras ----

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
