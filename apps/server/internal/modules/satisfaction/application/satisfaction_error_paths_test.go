package application

// satisfaction 落库错误分支（自 services/satisfaction_ai_error_paths_unit_test.go
// 的 satisfaction 段下沉；同文件的 AI 编排测试留在 services 包）。

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
)

func TestSatisfactionService_CreateInsertError(t *testing.T) {
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, nil)
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
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, nil)
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

func TestService_NilLogger(t *testing.T) {
	db := newSatisfactionTestDB(t, &models.CustomerSatisfaction{})
	if NewService(db, nil) == nil {
		t.Fatal("expected satisfaction service")
	}
}
