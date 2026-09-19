package services

// satisfaction / AI 编排的落库错误分支（自原 auth_error_paths_unit_test.go 拆出：
// auth 段已随 auth 模块迁移至 internal/modules/auth/application）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	mockllm "servify/apps/server/internal/platform/llm/mock"
)

func timeNow() time.Time { return time.Now() }

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
