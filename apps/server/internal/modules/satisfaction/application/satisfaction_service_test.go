//go:build integration
// +build integration

package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// scopedContext 自 services 侧 integration helper 复刻
// （与无标签 helper unitScopedContext 刻意区分名字）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func newSatisfactionIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Customer{}, &models.Agent{}, &models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestSatisfactionService_ScheduleAndRespondSurvey(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	logger := logrus.New()
	svc := NewService(db, logger)

	now := time.Now()
	user := &models.User{ID: 1, Username: "customer1", Email: "c1@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: user.ID}).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}

	ticket := &models.Ticket{
		ID:         1,
		Title:      "测试工单",
		CustomerID: user.ID,
		Status:     "closed",
		Source:     "web",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	survey, err := svc.ScheduleSurvey(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ScheduleSurvey failed: %v", err)
	}
	if survey == nil || survey.SurveyToken == "" {
		t.Fatalf("expected survey with token, got %+v", survey)
	}
	if survey.Status != "sent" {
		t.Fatalf("expected status sent, got %s", survey.Status)
	}

	satisfaction, err := svc.RespondSurvey(context.Background(), survey.SurveyToken, 5, "great job")
	if err != nil {
		t.Fatalf("RespondSurvey failed: %v", err)
	}
	if satisfaction == nil || satisfaction.Rating != 5 {
		t.Fatalf("unexpected satisfaction result: %+v", satisfaction)
	}

	var savedSurvey models.SatisfactionSurvey
	if err := db.First(&savedSurvey, survey.ID).Error; err != nil {
		t.Fatalf("load saved survey: %v", err)
	}
	if savedSurvey.Status != "completed" {
		t.Fatalf("expected completed status, got %s", savedSurvey.Status)
	}
	if savedSurvey.SatisfactionID == nil {
		t.Fatalf("expected satisfaction_id recorded")
	}
}

func TestSatisfactionService_GetSurveyPreview(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	logger := logrus.New()
	svc := NewService(db, logger)

	now := time.Now()
	user := &models.User{ID: 2, Username: "customer2", Email: "c2@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: user.ID}).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}

	ticket := &models.Ticket{
		ID:         2,
		Title:      "预览工单",
		CustomerID: user.ID,
		Status:     "closed",
		Source:     "email",
		ResolvedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	survey, err := svc.ScheduleSurvey(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ScheduleSurvey failed: %v", err)
	}

	preview, err := svc.GetSurveyPreviewByToken(context.Background(), survey.SurveyToken)
	if err != nil {
		t.Fatalf("GetSurveyPreviewByToken failed: %v", err)
	}
	if preview == nil || preview.TicketTitle != ticket.Title {
		t.Fatalf("unexpected preview result: %+v", preview)
	}
	if preview.Status != "sent" {
		t.Fatalf("expected sent status, got %s", preview.Status)
	}
	if preview.ResolvedAt == nil {
		t.Fatalf("expected resolved time in preview")
	}
}

func TestSatisfactionService_ScopedByWorkspace(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	logger := logrus.New()
	svc := NewService(db, logger)

	now := time.Now()
	customerA := &models.User{ID: 10, Username: "customer-a", Email: "customer-a@example.com"}
	customerB := &models.User{ID: 11, Username: "customer-b", Email: "customer-b@example.com"}
	if err := db.Create(customerA).Error; err != nil {
		t.Fatalf("create customer A: %v", err)
	}
	if err := db.Create(customerB).Error; err != nil {
		t.Fatalf("create customer B: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customerA.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("create customer profile A: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customerB.ID, TenantID: "tenant-a", WorkspaceID: "workspace-b"}).Error; err != nil {
		t.Fatalf("create customer profile B: %v", err)
	}

	ticketA := &models.Ticket{
		ID:          10,
		Title:       "工单 A",
		CustomerID:  customerA.ID,
		Status:      "closed",
		Source:      "web",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ticketB := &models.Ticket{
		ID:          11,
		Title:       "工单 B",
		CustomerID:  customerB.ID,
		Status:      "closed",
		Source:      "web",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticketA).Error; err != nil {
		t.Fatalf("create ticket A: %v", err)
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")

	surveyA, err := svc.ScheduleSurvey(ctxA, ticketA)
	if err != nil {
		t.Fatalf("schedule survey A: %v", err)
	}
	if surveyA.TenantID != "tenant-a" || surveyA.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scope on survey A: %+v", surveyA)
	}
	if _, err := svc.ScheduleSurvey(ctxB, ticketB); err != nil {
		t.Fatalf("schedule survey B: %v", err)
	}

	satisfactionA, err := svc.RespondSurvey(context.Background(), surveyA.SurveyToken, 5, "great")
	if err != nil {
		t.Fatalf("respond survey A: %v", err)
	}
	if satisfactionA.TenantID != "tenant-a" || satisfactionA.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scope on satisfaction A: %+v", satisfactionA)
	}

	surveysA, totalSurveysA, err := svc.ListSurveys(ctxA, &SatisfactionSurveyListRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list surveys A: %v", err)
	}
	if totalSurveysA != 1 || len(surveysA) != 1 || surveysA[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scoped surveys: total=%d surveys=%+v", totalSurveysA, surveysA)
	}

	satisfactionsA, totalSatisfactionsA, err := svc.ListSatisfactions(ctxA, &SatisfactionListRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list satisfactions A: %v", err)
	}
	if totalSatisfactionsA != 1 || len(satisfactionsA) != 1 || satisfactionsA[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scoped satisfactions: total=%d items=%+v", totalSatisfactionsA, satisfactionsA)
	}

	statsA, err := svc.GetSatisfactionStats(ctxA, nil, nil)
	if err != nil {
		t.Fatalf("stats A: %v", err)
	}
	if statsA.TotalRatings != 1 || statsA.RatingDistribution[5] != 1 {
		t.Fatalf("unexpected scoped stats: %+v", statsA)
	}

	if _, err := svc.ResendSurvey(ctxB, surveyA.ID); err == nil {
		t.Fatal("expected cross-workspace resend to fail")
	}
	if _, err := svc.UpdateSatisfaction(ctxB, satisfactionA.ID, "cross-workspace"); err == nil {
		t.Fatal("expected cross-workspace update to fail")
	}
	if err := svc.DeleteSatisfaction(ctxB, satisfactionA.ID); err == nil {
		t.Fatal("expected cross-workspace delete to fail")
	}
}

func TestSatisfactionService_GetSatisfactionStats_DoesNotLeakCrossWorkspaceCategoryOrTrend(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	svc := NewService(db, logrus.New())
	day := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	if err := db.Create(&[]models.CustomerSatisfaction{
		{
			ID:          501,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			TicketID:    10,
			CustomerID:  11,
			AgentID:     uintPtr(21),
			Rating:      5,
			Category:    "support",
			CreatedAt:   day,
		},
		{
			ID:          502,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-b",
			TicketID:    12,
			CustomerID:  13,
			AgentID:     uintPtr(22),
			Rating:      1,
			Category:    "billing",
			CreatedAt:   day.Add(2 * time.Hour),
		},
	}).Error; err != nil {
		t.Fatalf("create satisfactions: %v", err)
	}

	start := day.Add(-time.Hour)
	end := day.Add(6 * time.Hour)
	stats, err := svc.GetSatisfactionStats(scopedContext("tenant-a", "workspace-a"), &start, &end)
	if err != nil {
		t.Fatalf("GetSatisfactionStats failed: %v", err)
	}
	if stats.TotalRatings != 1 || stats.AverageRating != 5 {
		t.Fatalf("unexpected scoped stats summary: %+v", stats)
	}
	if len(stats.CategoryStats) != 1 {
		t.Fatalf("expected 1 scoped category stat, got %+v", stats.CategoryStats)
	}
	support, ok := stats.CategoryStats["support"]
	if !ok || support.Count != 1 || support.AverageRating != 5 {
		t.Fatalf("unexpected scoped category stats: %+v", stats.CategoryStats)
	}
	if _, leaked := stats.CategoryStats["billing"]; leaked {
		t.Fatalf("unexpected cross-workspace category leak: %+v", stats.CategoryStats)
	}
	if len(stats.TrendData) != 1 || stats.TrendData[0].Count != 1 || stats.TrendData[0].AverageRating != 5 {
		t.Fatalf("unexpected scoped trend data: %+v", stats.TrendData)
	}
}

func TestSatisfactionService_ListSatisfactions_DoesNotLeakCrossScopePreloads(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	svc := NewService(db, logrus.New())
	now := time.Now()

	customerUserB := &models.User{ID: 301, Username: "customer-b", Email: "customer-b@example.com", Name: "Customer B"}
	agentUserB := &models.User{ID: 302, Username: "agent-b", Email: "agent-b@example.com", Name: "Agent B"}
	if err := db.Create(customerUserB).Error; err != nil {
		t.Fatalf("create customer user B: %v", err)
	}
	if err := db.Create(agentUserB).Error; err != nil {
		t.Fatalf("create agent user B: %v", err)
	}

	customerB := &models.Customer{ID: 201, TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: customerUserB.ID}
	if err := db.Create(customerB).Error; err != nil {
		t.Fatalf("create customer B: %v", err)
	}
	agentB := &models.Agent{TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: agentUserB.ID, Status: "online"}
	if err := db.Create(agentB).Error; err != nil {
		t.Fatalf("create agent B: %v", err)
	}

	ticketB := &models.Ticket{
		ID:          101,
		Title:       "Ticket B",
		CustomerID:  customerUserB.ID,
		Status:      "closed",
		Source:      "web",
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	satisfactionA := &models.CustomerSatisfaction{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		TicketID:    ticketB.ID,
		CustomerID:  customerB.ID,
		AgentID:     &agentUserB.ID,
		Rating:      5,
		Category:    "overall",
		CreatedAt:   now,
	}
	if err := db.Create(satisfactionA).Error; err != nil {
		t.Fatalf("create satisfaction A: %v", err)
	}

	items, total, err := svc.ListSatisfactions(scopedContext("tenant-a", "workspace-a"), &SatisfactionListRequest{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListSatisfactions failed: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected scoped satisfactions: total=%d items=%+v", total, items)
	}
	if items[0].Ticket.ID != 0 {
		t.Fatalf("expected ticket preload to stay scoped, got %+v", items[0].Ticket)
	}
	if items[0].Customer.ID != 0 {
		t.Fatalf("expected customer preload to stay scoped, got %+v", items[0].Customer)
	}
	if items[0].Agent != nil {
		t.Fatalf("expected agent preload to stay scoped, got %+v", items[0].Agent)
	}
}

func TestSatisfactionService_GetSurveyPreview_DoesNotLeakCrossScopeAgent(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	logger := logrus.New()
	svc := NewService(db, logger)
	now := time.Now()

	customerUser := &models.User{ID: 401, Username: "customer-prev", Email: "customer-prev@example.com"}
	agentUserB := &models.User{ID: 402, Username: "agent-prev-b", Email: "agent-prev-b@example.com", Name: "Agent Preview B", Role: "agent"}
	if err := db.Create(customerUser).Error; err != nil {
		t.Fatalf("create customer user: %v", err)
	}
	if err := db.Create(agentUserB).Error; err != nil {
		t.Fatalf("create agent user B: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customerUser.ID, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: agentUserB.ID, TenantID: "tenant-a", WorkspaceID: "workspace-b", Status: "online"}).Error; err != nil {
		t.Fatalf("create agent B: %v", err)
	}

	ticketA := &models.Ticket{
		ID:          401,
		Title:       "工单预览 A",
		CustomerID:  customerUser.ID,
		AgentID:     &agentUserB.ID,
		Status:      "closed",
		Source:      "web",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		ResolvedAt:  &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticketA).Error; err != nil {
		t.Fatalf("create ticket A: %v", err)
	}

	surveyA, err := svc.ScheduleSurvey(scopedContext("tenant-a", "workspace-a"), ticketA)
	if err != nil {
		t.Fatalf("schedule survey A: %v", err)
	}

	preview, err := svc.GetSurveyPreviewByToken(context.Background(), surveyA.SurveyToken)
	if err != nil {
		t.Fatalf("GetSurveyPreviewByToken failed: %v", err)
	}
	if preview.AgentName != "" {
		t.Fatalf("expected preview agent name to stay scoped, got %+v", preview)
	}
}

func TestSatisfactionService_ScheduleSurvey_RejectsCrossScopeTicket(t *testing.T) {
	db := newSatisfactionIntegrationDB(t)
	logger := logrus.New()
	svc := NewService(db, logger)
	now := time.Now()

	customerB := &models.User{ID: 451, Username: "customer-bx", Email: "customer-bx@example.com"}
	if err := db.Create(customerB).Error; err != nil {
		t.Fatalf("create customer B: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customerB.ID, TenantID: "tenant-a", WorkspaceID: "workspace-b"}).Error; err != nil {
		t.Fatalf("create customer profile B: %v", err)
	}

	ticketB := &models.Ticket{
		ID:          451,
		Title:       "工单 Bx",
		CustomerID:  customerB.ID,
		Status:      "closed",
		Source:      "web",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	if _, err := svc.ScheduleSurvey(scopedContext("tenant-a", "workspace-a"), ticketB); err == nil {
		t.Fatal("expected cross-scope ticket scheduling to fail")
	}
}
