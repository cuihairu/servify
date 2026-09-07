package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	mockllm "servify/apps/server/internal/platform/llm/mock"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return logger
}

func timeNow() time.Time { return time.Now() }

func TestAuthService_RegisterInternalErrors(t *testing.T) {
	// bcrypt rejects passwords longer than 72 bytes
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewAuthService(db, testAuthConfig())
	if _, err := svc.Register(context.Background(), RegisterInput{
		Username: "long-pw", Email: "long@x.com", Password: strings.Repeat("x", 100),
	}, AuthSessionMetadata{}); err == nil {
		t.Fatal("expected bcrypt error for long password")
	}

	// session creation failure surfaces
	db2 := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc2 := NewAuthService(db2, testAuthConfig())
	if err := db2.Create(&models.User{
		ID: 61, Username: "u61", Email: "u61@x.com", Password: "x", Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db2.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	// register a different user: user row inserts, session insert fails
	if _, err := svc2.Register(context.Background(), RegisterInput{
		Username: "u62", Email: "u62@x.com", Password: "pw123456",
	}, AuthSessionMetadata{}); err == nil {
		t.Fatal("expected session create error")
	}

	// nil-db guard inside createAuthSession
	nilSvc := &AuthService{}
	if _, err := nilSvc.createAuthSession(context.Background(), 1, AuthSessionMetadata{}); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
}

func TestAuthService_LoginInternalErrors(t *testing.T) {
	// user lookup error (not found variant)
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewAuthService(db, testAuthConfig())
	if err := db.Migrator().DropTable("users"); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Username: "u", Password: "p"}, AuthSessionMetadata{}); err == nil ||
		err == ErrAuthInvalidCredentials {
		t.Fatalf("expected query error, got %v", err)
	}

	// session creation failure during login
	db2 := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc2 := NewAuthService(db2, testAuthConfig())
	if err := db2.Create(&models.User{
		ID: 62, Username: "u62", Email: "u62@x.com", Password: "pw123456", Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db2.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc2.Login(context.Background(), LoginInput{Username: "u62", Password: "pw123456"}, AuthSessionMetadata{}); err == nil {
		t.Fatal("expected session create error during login")
	}
}

func TestAuthService_SessionQueryErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.UserAuthSession{})
	svc := NewAuthService(db, testAuthConfig())
	ctx := context.Background()

	if err := db.Migrator().DropTable("user_auth_sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc.ListAuthSessions(ctx, 1); err == nil {
		t.Fatal("expected list sessions error")
	}
	if _, err := svc.RevokeCurrentSession(ctx, 1, "s"); err == nil {
		t.Fatal("expected revoke current error")
	}
	if _, err := svc.RevokeOtherSessions(ctx, 1, "s"); err == nil {
		t.Fatal("expected revoke others error")
	}
	if _, err := svc.RefreshToken(ctx, "x", AuthSessionMetadata{}); err == nil {
		t.Fatal("expected refresh error")
	}
}

func TestAppIntegrationService_UpdateUniqueViolation(t *testing.T) {
	db := newServicesTestDB(t, &models.AppIntegration{})
	svc := NewAppIntegrationService(db, nil)
	ctx := context.Background()

	first, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "One", Slug: "one", IFrameURL: "u"})
	if err != nil {
		t.Fatalf("create one: %v", err)
	}
	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "Two", Slug: "two", IFrameURL: "u"}); err != nil {
		t.Fatalf("create two: %v", err)
	}
	if _, err := svc.Update(ctx, first.ID, &AppIntegrationUpdateRequest{Name: stringPtr("Two")}); err == nil {
		t.Fatal("expected unique name violation on update")
	}
}

func TestCustomerService_ModuleQueryError(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Session{}, &models.Ticket{}, &models.Message{},
	)
	svc := NewCustomerService(db, nil)
	if err := db.Migrator().DropTable("customers"); err != nil {
		t.Fatalf("drop customers: %v", err)
	}
	if _, _, err := svc.ListCustomers(context.Background(), &CustomerListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list customers error")
	}
}

func TestSatisfactionService_CreateInsertError(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, nil)
	ctx := context.Background()

	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer"}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customer.ID}).Error; err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CustomerID: customer.ID, CreatedAt: timeNow(), UpdatedAt: timeNow()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Migrator().DropTable("customer_satisfactions"); err != nil {
		t.Fatalf("drop satisfactions: %v", err)
	}
	// duplicate-check query errors, insert then also fails
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticket.ID, CustomerID: customer.ID, Rating: 5,
	}); err == nil {
		t.Fatal("expected insert error")
	}
}

func TestSatisfactionService_ScheduleSurveyExistingLoadError(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewSatisfactionService(db, nil)
	ticket := &models.Ticket{Title: "T", CreatedAt: timeNow(), UpdatedAt: timeNow()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Migrator().DropTable("satisfaction_surveys"); err != nil {
		t.Fatalf("drop surveys: %v", err)
	}
	// existing-survey lookup errors (non-notfound)
	if _, err := svc.ScheduleSurvey(context.Background(), ticket); err == nil {
		t.Fatal("expected existing survey load error")
	}
}

func TestOrchestratedAI_ProcessQueryError(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	svc := NewOrchestratedEnhancedAIService(
		base,
		&mockllm.Provider{ChatError: errors.New("llm unavailable")},
		nil,
		"",
		nil,
		"",
		nil,
	)
	// no provider + no fallback configured -> orchestrator error surfaces when LLM fails
	svc.SetFallbackEnabled(false)
	if _, err := svc.ProcessQuery(context.Background(), "普通问题", "sess"); err == nil {
		t.Fatal("expected ProcessQuery error")
	}
}

func TestAgentService_ApplySessionTransfer_ModuleError(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	logger := newTestLogger()
	svc := NewAgentService(db, logger)
	ctx := context.Background()

	user := &models.User{Username: "agent9", Email: "agent9@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := svc.CreateAgent(ctx, &AgentCreateRequest{UserID: user.ID}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := svc.AgentGoOnline(ctx, user.ID); err != nil {
		t.Fatalf("AgentGoOnline: %v", err)
	}
	if err := db.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	// registry hit + UpdateChatLoad failure triggers the warn branch
	svc.ApplySessionTransfer(ctx, "sess-x", nil, user.ID)
}
