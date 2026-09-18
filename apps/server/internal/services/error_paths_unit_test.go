package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"github.com/sirupsen/logrus"
)

// ---- constructor nil-logger branches ----

func TestConstructors_NilLogger(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Customer{})
	if NewSatisfactionService(db, nil) == nil {
		t.Fatal("expected satisfaction service")
	}
	if NewShiftService(db, nil) == nil {
		t.Fatal("expected shift service")
	}
	if NewSLAService(db, nil) == nil {
		t.Fatal("expected SLA service")
	}
	if NewStatisticsService(db, nil) == nil {
		t.Fatal("expected statistics service")
	}
	if NewAutomationService(db, nil) == nil {
		t.Fatal("expected automation service")
	}
	if NewAppIntegrationService(db, nil) == nil {
		t.Fatal("expected app integration service")
	}
}

// ---- agent service error branches ----

func TestAgentService_DroppedTableErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{})
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewAgentService(db, logger)
	ctx := context.Background()

	if err := db.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := svc.GetAgentStats(ctx, nil); err == nil {
		t.Fatal("expected stats error with agents table missing")
	}
	from := uint(1)
	svc.ApplySessionTransfer(ctx, "sess", &from, 2) // UpdateChatLoad error path logs warning
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

func TestEnhancedAIService_RetrieveErrorContinues(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	enh := NewEnhancedAIService(base, nil, "kb", logrus.New())
	enh.SetWeKnoraEnabled(false)
	enh.SetFallbackEnabled(false)

	resp, err := enh.ProcessQueryEnhanced(context.Background(), "普通问题", "s")
	if err != nil {
		t.Fatalf("expected graceful fallback, got %v", err)
	}
	if resp.Strategy != "fallback" {
		t.Fatalf("expected fallback strategy, got %q", resp.Strategy)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty fallback content")
	}
}

func TestEnhancedAIService_SyncKnowledgeBase_UploadError(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	client := &MockWeKnoraClient{uploadError: errors.New("upload failed")}
	enh := NewEnhancedAIService(base, client, "kb", logrus.New())

	if err := enh.SyncKnowledgeBase(context.Background()); err == nil {
		t.Fatal("expected sync error when uploads fail")
	}
}

// ---- app integration error branches ----

func TestAppIntegrationService_DroppedTableErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.AppIntegration{})
	svc := NewAppIntegrationService(db, nil)
	ctx := context.Background()

	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "X", Slug: "x", IFrameURL: "u"}); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	// duplicate name violates the unique index during insert
	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "X", Slug: "other", IFrameURL: "u"}); err == nil {
		t.Fatal("expected create error from duplicate name")
	}
	// update violating unique name
	if _, err := svc.Update(ctx, 1, &AppIntegrationUpdateRequest{Name: stringPtr("X")}); err != nil {
		t.Fatalf("self rename: %v", err)
	}
	if err := db.Migrator().DropTable("app_integrations"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "Y", Slug: "y", IFrameURL: "u"}); err == nil {
		t.Fatal("expected slug check error with missing table")
	}
	if err := svc.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error with missing table")
	}
	if encodeJSON(make(chan int)) != "" {
		t.Fatal("expected empty encoding for unserializable value")
	}
}

// ---- custom field error branches ----

func TestCustomFieldService_DroppedTableErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.CustomField{})
	svc := NewCustomFieldService(db)
	ctx := context.Background()

	if _, err := svc.Create(ctx, &CustomFieldCreateRequest{Key: "k1", Name: "n", Type: "string"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Create(ctx, &CustomFieldCreateRequest{Key: "k1", Name: "n2", Type: "string"}); err == nil {
		t.Fatal("expected duplicate key error")
	}
	if err := db.Migrator().DropTable("custom_fields"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := svc.List(ctx, "", false); err == nil {
		t.Fatal("expected list error with missing table")
	}
	if err := svc.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error with missing table")
	}
}

// ---- macro error branches ----

func TestMacroService_DroppedTableErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.Macro{}, &models.Ticket{}, &models.TicketComment{})
	svc := NewMacroService(db)
	ctx := context.Background()

	if _, err := svc.Create(ctx, &MacroCreateRequest{Name: "m", Content: "c"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Create(ctx, &MacroCreateRequest{Name: "m", Content: "dup"}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	if err := db.Migrator().DropTable("macros"); err != nil {
		t.Fatalf("drop macros: %v", err)
	}
	if _, err := svc.List(ctx); err == nil {
		t.Fatal("expected list error with missing table")
	}
	if err := svc.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error with missing table")
	}
}

func TestMacroService_ApplyToTicket_DroppedComments(t *testing.T) {
	db := newServicesTestDB(t, &models.Macro{}, &models.Ticket{}, &models.TicketComment{})
	svc := NewMacroService(db)
	ctx := context.Background()

	macro, err := svc.Create(ctx, &MacroCreateRequest{Name: "m", Content: "c"})
	if err != nil {
		t.Fatalf("seed macro: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Migrator().DropTable("ticket_comments"); err != nil {
		t.Fatalf("drop comments: %v", err)
	}
	if _, err := svc.ApplyToTicket(ctx, macro.ID, ticket.ID, 1); err == nil {
		t.Fatal("expected comment create error")
	}
}

// ---- shift error branches ----

func TestShiftService_DroppedTableErrors(t *testing.T) {
	db := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.ShiftSchedule{})
	svc := NewShiftService(db, logrus.New())
	ctx := unitScopedContext("t1", "w1")

	user := &models.User{Username: "agent", Email: "agent@x.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	start := time.Now().Add(time.Hour)
	end := start.Add(time.Hour)

	if err := db.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := svc.CreateShift(ctx, &ShiftCreateRequest{AgentID: user.ID, ShiftType: "morning", StartTime: start, EndTime: end}); err == nil {
		t.Fatal("expected agent lookup error")
	}

	// shift table missing scenarios
	db2 := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.ShiftSchedule{})
	svc2 := NewShiftService(db2, logrus.New())
	if err := db2.Migrator().DropTable("shift_schedules"); err != nil {
		t.Fatalf("drop shifts: %v", err)
	}
	if _, _, err := svc2.ListShifts(ctx, &ShiftListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list error")
	}
	if _, err := svc2.UpdateShift(ctx, 1, &ShiftUpdateRequest{}); err == nil {
		t.Fatal("expected update error")
	}
	if err := svc2.DeleteShift(ctx, 1); err == nil {
		t.Fatal("expected delete error")
	}
	if _, err := svc2.GetShiftStats(ctx); err == nil {
		t.Fatal("expected stats error")
	}
	// unscoped preload shortcut branch
	db3 := newServicesTestDB(t, &models.User{}, &models.Agent{}, &models.ShiftSchedule{})
	svc3 := NewShiftService(db3, logrus.New())
	if _, _, err := svc3.ListShifts(context.Background(), &ShiftListRequest{Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("unscoped list: %v", err)
	}
}

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

// ---- workspace error branches ----

func TestWorkspaceService_DroppedTableErrors(t *testing.T) {
	ctx := context.Background()

	dbAgents := newServicesTestDB(t,
		&models.User{}, &models.Agent{}, &models.Customer{},
		&models.Session{}, &models.Ticket{}, &models.Message{},
	)
	if err := dbAgents.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := NewWorkspaceService(dbAgents, nil).GetOverview(ctx, 5); err == nil {
		t.Fatal("expected agents count error")
	}

	dbTickets := newServicesTestDB(t,
		&models.User{}, &models.Agent{}, &models.Customer{},
		&models.Session{}, &models.Ticket{}, &models.Message{},
	)
	if err := dbTickets.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := NewWorkspaceService(dbTickets, nil).GetOverview(ctx, 5); err == nil {
		t.Fatal("expected recent sessions load error")
	}
}

// ---- statistics error branches ----

func TestStatisticsService_DroppedTableErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	db := newServicesTestDB(t,
		&models.User{}, &models.Agent{}, &models.Ticket{}, &models.Session{},
		&models.Message{}, &models.CustomerSatisfaction{}, &models.Customer{}, &models.DailyStats{},
	)
	svc := NewStatisticsService(db, logrus.New())
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.GetAgentPerformanceStats(ctx, now.Add(-time.Hour), now, 5); err == nil {
		t.Fatal("expected agent perf error")
	}
	if _, err := svc.GetTicketCategoryStats(ctx, now.Add(-time.Hour), now); err == nil {
		t.Fatal("expected category stats error")
	}
	if _, err := svc.GetTicketPriorityStats(ctx, now.Add(-time.Hour), now); err == nil {
		t.Fatal("expected priority stats error")
	}
	if _, err := svc.GetRemoteAssistTicketStats(ctx); err == nil {
		t.Fatal("expected remote assist error")
	}

	db2 := newServicesTestDB(t, &models.Customer{})
	if err := db2.Migrator().DropTable("customers"); err != nil {
		t.Fatalf("drop customers: %v", err)
	}
	svc2 := NewStatisticsService(db2, logrus.New())
	if _, err := svc2.GetCustomerSourceStats(ctx); err == nil {
		t.Fatal("expected customer source stats error")
	}
}

func TestStatisticsService_WorkerTickerLoop(t *testing.T) {
	svc := newStatisticsTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.StartDailyStatsWorkerContext(ctx, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
}

// ---- automation extras ----

func TestAutomationHandlerAdapter_DeleteTrigger(t *testing.T) {
	svc, _ := newAutomationTestService(t)
	adapter := NewAutomationHandlerAdapter(svc)
	_ = adapter.DeleteTrigger(context.Background(), 999) // error propagated from module
}

func TestAutomationService_MatchTriggerNilReceiver(t *testing.T) {
	var svc *AutomationService
	if svc.matchTrigger(context.Background(), models.AutomationTrigger{}, AutomationEvent{}, nil, false) {
		t.Fatal("nil receiver should not match")
	}
}

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

// ---- agent runtime maintenance extras ----

func TestAgentRuntimeMaintenance_CleanupSkipsZeroActivity(t *testing.T) {
	repo := &maintenanceRepo{
		profile: &agentdomain.AgentProfile{UserID: 9, MaxChatConcurrency: 2},
		model:   &models.Agent{UserID: 9},
	}
	registry := &maintenanceRegistry{items: map[uint]agentapp.AgentRuntimeDTO{
		9: {UserID: 9, Status: "online"}, // zero LastActivity
	}}
	module := agentapp.NewService(repo, registry)
	m := newAgentRuntimeMaintenance(logrus.New(), module)
	m.cleanupInactiveAgents(context.Background(), time.Minute)
	if len(repo.statusUpdates) != 0 {
		t.Fatalf("expected no updates for zero activity, got %v", repo.statusUpdates)
	}
}
