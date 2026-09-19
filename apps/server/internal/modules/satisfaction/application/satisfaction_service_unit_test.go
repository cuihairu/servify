package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newSatisfactionTestService(t *testing.T) (*SatisfactionService, *gorm.DB) {
	t.Helper()
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	return NewService(db, logrus.New()), db
}

func seedSatisfactionData(t *testing.T, db *gorm.DB) (customerID, agentID, ticketID uint) {
	t.Helper()
	customer := &models.User{ID: 101, Username: "cust", Email: "cust@x.com", Role: "customer"}
	agent := &models.User{ID: 102, Username: "agent", Email: "agent@x.com", Role: "agent", Name: "Agent Name"}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customer.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed customer profile: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: agent.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed agent profile: %v", err)
	}
	resolved := time.Now().Add(-time.Hour)
	ticket := &models.Ticket{
		ID: 201, Title: "Ticket", CustomerID: customer.ID, AgentID: &agent.ID,
		Status: "resolved", ResolvedAt: &resolved, Source: "CHAT",
		TenantID: "t1", WorkspaceID: "w1", CreatedAt: time.Now().Add(-2 * time.Hour), UpdatedAt: time.Now(),
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	return customer.ID, agent.ID, ticket.ID
}

func TestSatisfactionService_ScheduleSurvey(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	customerID, agentID, ticketID := seedSatisfactionData(t, db)

	if _, err := svc.ScheduleSurvey(context.Background(), nil); err == nil {
		t.Fatal("expected nil ticket error")
	}
	if _, err := svc.ScheduleSurvey(context.Background(), &models.Ticket{ID: 999}); err == nil {
		t.Fatal("expected ticket not found error")
	}

	survey, err := svc.ScheduleSurvey(context.Background(), &models.Ticket{ID: ticketID, TenantID: "t1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("ScheduleSurvey: %v", err)
	}
	if survey == nil || survey.Status != "sent" || survey.Channel != "chat" || survey.SurveyToken == "" {
		t.Fatalf("unexpected survey: %+v", survey)
	}

	// reusing existing queued/sent survey
	again, err := svc.ScheduleSurvey(context.Background(), &models.Ticket{ID: ticketID})
	if err != nil {
		t.Fatalf("ScheduleSurvey second: %v", err)
	}
	if again == nil || again.ID != survey.ID {
		t.Fatalf("expected reuse, got %+v", again)
	}

	// existing satisfaction suppresses new surveys
	if err := db.Create(&models.Ticket{
		ID: 777, Title: "Rated", CustomerID: customerID,
		TenantID: "t1", WorkspaceID: "w1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed rated ticket: %v", err)
	}
	if err := db.Create(&models.CustomerSatisfaction{
		TicketID: 777, CustomerID: customerID, TenantID: "t1", WorkspaceID: "w1",
		Rating: 5, CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed satisfaction: %v", err)
	}
	suppressed, err := svc.ScheduleSurvey(context.Background(), &models.Ticket{ID: 777, TenantID: "t1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("ScheduleSurvey with satisfaction: %v", err)
	}
	if suppressed != nil {
		t.Fatalf("expected nil survey, got %+v", suppressed)
	}
	_ = agentID
}

func TestSatisfactionService_ListSurveys(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	customerID, _, ticketID := seedSatisfactionData(t, db)
	ctx := unitScopedContext("t1", "w1")

	if _, err := svc.ScheduleSurvey(ctx, &models.Ticket{ID: ticketID}); err != nil {
		t.Fatalf("ScheduleSurvey: %v", err)
	}

	items, total, err := svc.ListSurveys(ctx, &SatisfactionSurveyListRequest{
		Page: 0, PageSize: 0,
		TicketID:   uintPtr(ticketID),
		CustomerID: uintPtr(customerID),
		Status:     []string{"sent"},
		Channel:    []string{"chat"},
	})
	if err != nil {
		t.Fatalf("ListSurveys: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected surveys: %d %+v", total, items)
	}
}

func TestSatisfactionService_PreviewAndRespond(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	_, _, ticketID := seedSatisfactionData(t, db)
	ctx := context.Background()

	if _, err := svc.GetSurveyPreviewByToken(ctx, ""); err != ErrSurveyNotFound {
		t.Fatalf("expected ErrSurveyNotFound, got %v", err)
	}
	if _, err := svc.GetSurveyPreviewByToken(ctx, "missing-token"); err != ErrSurveyNotFound {
		t.Fatalf("expected ErrSurveyNotFound, got %v", err)
	}
	if _, err := svc.RespondSurvey(ctx, "", 5, ""); err != ErrSurveyNotFound {
		t.Fatalf("expected ErrSurveyNotFound, got %v", err)
	}
	if _, err := svc.RespondSurvey(ctx, "missing-token", 5, ""); err != ErrSurveyNotFound {
		t.Fatalf("expected ErrSurveyNotFound, got %v", err)
	}

	survey, err := svc.ScheduleSurvey(ctx, &models.Ticket{ID: ticketID, TenantID: "t1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("ScheduleSurvey: %v", err)
	}

	preview, err := svc.GetSurveyPreviewByToken(ctx, survey.SurveyToken)
	if err != nil {
		t.Fatalf("GetSurveyPreviewByToken: %v", err)
	}
	if preview.TicketTitle != "Ticket" || preview.AgentName != "Agent Name" {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	sat, err := svc.RespondSurvey(ctx, survey.SurveyToken, 4, "nice")
	if err != nil {
		t.Fatalf("RespondSurvey: %v", err)
	}
	if sat.Rating != 4 {
		t.Fatalf("unexpected satisfaction: %+v", sat)
	}

	// completed survey cannot be responded to again
	if _, err := svc.RespondSurvey(ctx, survey.SurveyToken, 5, ""); err != ErrSurveyCompleted {
		t.Fatalf("expected ErrSurveyCompleted, got %v", err)
	}
	if _, err := svc.ResendSurvey(ctx, survey.ID); err != ErrSurveyCompleted {
		t.Fatalf("expected ErrSurveyCompleted, got %v", err)
	}

	// duplicate rating path returns existing record; a new ScheduleSurvey would
	// be suppressed by the existing rating, so seed a queued survey directly.
	queuedAt := time.Now()
	survey2 := &models.SatisfactionSurvey{
		TicketID: ticketID, CustomerID: 101, Channel: "email", Status: "queued",
		SurveyToken: "dup-token", CreatedAt: queuedAt, UpdatedAt: queuedAt,
	}
	if err := db.Create(survey2).Error; err != nil {
		t.Fatalf("seed queued survey: %v", err)
	}
	existing, err := svc.RespondSurvey(ctx, survey2.SurveyToken, 3, "")
	if err != nil {
		t.Fatalf("RespondSurvey duplicate: %v", err)
	}
	if existing == nil || existing.ID != sat.ID {
		t.Fatalf("expected existing satisfaction, got %+v", existing)
	}
}

func TestSatisfactionService_ExpiredSurvey(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	_, _, ticketID := seedSatisfactionData(t, db)

	past := time.Now().Add(-time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: ticketID, CustomerID: 101, Channel: "email", Status: "sent",
		SurveyToken: "expired-token", SentAt: &past, ExpiresAt: &past,
		CreatedAt: past, UpdatedAt: past,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("seed expired survey: %v", err)
	}

	if _, err := svc.RespondSurvey(context.Background(), "expired-token", 5, ""); err != ErrSurveyExpired {
		t.Fatalf("expected ErrSurveyExpired, got %v", err)
	}
}

func TestSatisfactionService_ResendSurvey(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	_, _, ticketID := seedSatisfactionData(t, db)

	if _, err := svc.ResendSurvey(context.Background(), 999); err != ErrSurveyNotFound {
		t.Fatalf("expected ErrSurveyNotFound, got %v", err)
	}

	past := time.Now().Add(-2 * time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: ticketID, CustomerID: 101, Channel: "email", Status: "queued",
		SentAt: &past, ExpiresAt: &past, CreatedAt: past, UpdatedAt: past,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}

	resent, err := svc.ResendSurvey(context.Background(), survey.ID)
	if err != nil {
		t.Fatalf("ResendSurvey: %v", err)
	}
	if resent.Status != "sent" || resent.ExpiresAt.Before(time.Now()) {
		t.Fatalf("unexpected resent survey: %+v", resent)
	}

	// empty token gets regenerated
	if err := db.Model(&models.SatisfactionSurvey{}).Where("id = ?", survey.ID).
		Update("survey_token", "").Error; err != nil {
		t.Fatalf("clear token: %v", err)
	}
	regen, err := svc.ResendSurvey(context.Background(), survey.ID)
	if err != nil {
		t.Fatalf("ResendSurvey regen: %v", err)
	}
	if regen.SurveyToken == "" {
		t.Fatal("expected regenerated token")
	}

	// completed without satisfaction id can be resent
	if err := db.Model(&models.SatisfactionSurvey{}).Where("id = ?", survey.ID).
		Updates(map[string]interface{}{"status": "completed", "satisfaction_id": nil, "completed_at": time.Now()}).Error; err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	completedResent, err := svc.ResendSurvey(context.Background(), survey.ID)
	if err != nil {
		t.Fatalf("ResendSurvey completed: %v", err)
	}
	if completedResent.Status != "sent" {
		t.Fatalf("unexpected completed resend status: %+v", completedResent)
	}
	// originalCompleted=true keeps CompletedAt/SatisfactionID untouched
	if completedResent.CompletedAt == nil {
		t.Fatal("expected CompletedAt preserved for originally completed survey")
	}
}

func TestSatisfactionService_CreateSatisfactionValidation(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	customerID, agentID, ticketID := seedSatisfactionData(t, db)
	ctx := unitScopedContext("t1", "w1")

	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: 999, CustomerID: customerID, Rating: 5}); err == nil {
		t.Fatal("expected ticket not found error")
	}
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: ticketID, CustomerID: 999, Rating: 5}); err == nil {
		t.Fatal("expected customer not found error")
	}
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: ticketID, CustomerID: customerID + 1, Rating: 5}); err == nil {
		t.Fatal("expected owner mismatch error")
	}

	// create without agent
	first, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: ticketID, CustomerID: customerID, Rating: 5})
	if err != nil {
		t.Fatalf("CreateSatisfaction: %v", err)
	}
	if first.Category != "overall" {
		t.Fatalf("expected default category, got %q", first.Category)
	}

	// duplicate rating rejected
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: ticketID, CustomerID: customerID, Rating: 4}); err == nil {
		t.Fatal("expected duplicate rating error")
	}

	// with invalid agent
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: 202, CustomerID: customerID, AgentID: uintPtr(999), Rating: 5,
	}); err == nil {
		t.Fatal("expected agent not found error")
	}

	// valid second ticket with agent
	ticket2 := &models.Ticket{
		ID: 202, Title: "T2", CustomerID: customerID, AgentID: &agentID,
		TenantID: "t1", WorkspaceID: "w1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(ticket2).Error; err != nil {
		t.Fatalf("seed ticket2: %v", err)
	}
	second, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: 202, CustomerID: customerID, AgentID: uintPtr(agentID), Rating: 3, Category: "service_quality",
	})
	if err != nil {
		t.Fatalf("CreateSatisfaction with agent: %v", err)
	}
	if second.Category != "service_quality" {
		t.Fatalf("unexpected category: %+v", second)
	}
}

func TestSatisfactionService_GetListStatsDeleteUpdate(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	customerID, agentID, ticketID := seedSatisfactionData(t, db)
	ctx := unitScopedContext("t1", "w1")

	created, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticketID, CustomerID: customerID, AgentID: uintPtr(agentID), Rating: 5, Comment: "c",
	})
	if err != nil {
		t.Fatalf("CreateSatisfaction: %v", err)
	}

	got, err := svc.GetSatisfaction(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetSatisfaction: %v %+v", err, got)
	}
	if _, err := svc.GetSatisfaction(ctx, 999); err == nil {
		t.Fatal("expected not found error")
	}

	byTicket, err := svc.GetSatisfactionByTicket(ctx, ticketID)
	if err != nil || byTicket == nil {
		t.Fatalf("GetSatisfactionByTicket: %v %+v", err, byTicket)
	}
	if none, err := svc.GetSatisfactionByTicket(ctx, 999); err != nil || none != nil {
		t.Fatalf("expected nil for missing ticket: %v %+v", err, none)
	}

	items, total, err := svc.ListSatisfactions(ctx, &SatisfactionListRequest{
		Page: 1, PageSize: 10,
		TicketID:   uintPtr(ticketID),
		CustomerID: uintPtr(customerID),
		AgentID:    uintPtr(agentID),
		Rating:     []int{5},
		Category:   []string{"overall"},
		DateFrom:   timePtr(time.Now().Add(-time.Hour)),
		DateTo:     timePtr(time.Now().Add(time.Hour)),
		SortBy:     "created_at",
		SortOrder:  "asc",
	})
	if err != nil {
		t.Fatalf("ListSatisfactions: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected list: %d %+v", total, items)
	}
	if _, _, err := svc.ListSatisfactions(ctx, &SatisfactionListRequest{SortBy: "", SortOrder: "bogus"}); err != nil {
		t.Fatalf("ListSatisfactions defaults: %v", err)
	}

	stats, err := svc.GetSatisfactionStats(ctx, timePtr(time.Now().Add(-time.Hour)), timePtr(time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("GetSatisfactionStats: %v", err)
	}
	if stats.TotalRatings != 1 || stats.AverageRating != 5 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.RatingDistribution[5] != 1 {
		t.Fatalf("unexpected distribution: %+v", stats.RatingDistribution)
	}
	if _, err := svc.GetSatisfactionStats(ctx, nil, nil); err != nil {
		t.Fatalf("GetSatisfactionStats no dates: %v", err)
	}

	updated, err := svc.UpdateSatisfaction(ctx, created.ID, "new comment")
	if err != nil {
		t.Fatalf("UpdateSatisfaction: %v", err)
	}
	if updated.Comment != "new comment" {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := svc.UpdateSatisfaction(ctx, 999, "x"); err == nil {
		t.Fatal("expected update not found error")
	}

	if err := svc.DeleteSatisfaction(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSatisfaction: %v", err)
	}
	if err := svc.DeleteSatisfaction(ctx, created.ID); err == nil {
		t.Fatal("expected delete not found error")
	}
}

func TestSatisfactionHelpers(t *testing.T) {
	cases := map[string]string{
		"chat":  "chat",
		" IM ":  "chat",
		"voice": "voice",
		"phone": "voice",
		"email": "email",
		"weird": "email",
		"":      "email",
	}
	for in, want := range cases {
		if got := detectSurveyChannel(in); got != want {
			t.Fatalf("detectSurveyChannel(%q) = %q, want %q", in, got, want)
		}
	}

	if tenant, ws := tenantAndWorkspace(contextWithRecordScope(context.Background(), "rt", "rw")); tenant != "rt" || ws != "rw" {
		t.Fatalf("record scope fallback: %q/%q", tenant, ws)
	}
	if tenant, ws := tenantAndWorkspace(contextWithRecordScope(unitScopedContext("ct", "cw"), "rt", "rw")); tenant != "ct" || ws != "cw" {
		t.Fatalf("record scope keeps context: %q/%q", tenant, ws)
	}

	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	if scopeAwareSatisfactionPreloads(db, context.Background()) == nil {
		t.Fatal("expected preloads chain")
	}
	if scopeAwareTicketAgentPreload(db, context.Background()) == nil {
		t.Fatal("expected agent preload chain (unscoped)")
	}
	if scopeAwareTicketAgentPreload(db, unitScopedContext("t", "")) == nil {
		t.Fatal("expected agent preload chain (tenant only)")
	}
}
