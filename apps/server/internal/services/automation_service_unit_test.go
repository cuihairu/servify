package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newAutomationTestService(t *testing.T) (*AutomationService, *gorm.DB) {
	t.Helper()
	db := newServicesTestDB(t,
		&models.AutomationTrigger{}, &models.AutomationRun{},
		&models.Ticket{}, &models.TicketComment{},
	)
	return NewAutomationService(db, logrus.New()), db
}

func TestAutomationService_Triggers(t *testing.T) {
	svc, _ := newAutomationTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateTrigger(ctx, nil); err == nil {
		t.Fatal("expected error for nil trigger request")
	}
	if _, err := svc.CreateTrigger(ctx, &AutomationTriggerRequest{Name: "t", Event: "bad.event"}); err == nil {
		t.Fatal("expected unsupported event error")
	}

	trig, err := svc.CreateTrigger(ctx, &AutomationTriggerRequest{
		Name:       "escalate",
		Event:      "ticket_created",
		Conditions: []TriggerCondition{{Field: "ticket.priority", Op: "eq", Value: "urgent"}},
		Actions:    []TriggerAction{{Type: "set_priority", Params: map[string]interface{}{"priority": "urgent"}}},
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	triggers, err := svc.ListTriggers(ctx)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}

	if err := svc.DeleteTrigger(ctx, trig.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
}

func TestAutomationService_EventsAndRuns(t *testing.T) {
	svc, db := newAutomationTestService(t)
	ctx := context.Background()

	ticket := &models.Ticket{Title: "T", Priority: "urgent", Status: "open", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	if _, err := svc.CreateTrigger(ctx, &AutomationTriggerRequest{
		Name:       "escalate",
		Event:      "ticket.created",
		Conditions: []TriggerCondition{{Field: "ticket.priority", Op: "eq", Value: "urgent"}},
		Actions:    []TriggerAction{{Type: "notify_log"}},
	}); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	svc.HandleEvent(ctx, AutomationEvent{Type: "ticket_created", TicketID: ticket.ID})
	svc.HandleEvent(ctx, AutomationEvent{Type: "unknown_event"})

	runs, total, err := svc.ListRuns(ctx, &AutomationRunListRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("expected one run, got %d/%d", total, len(runs))
	}
	if _, _, err := svc.ListRuns(ctx, nil); err != nil {
		t.Fatalf("ListRuns nil: %v", err)
	}

	resp, err := svc.BatchRun(ctx, &AutomationBatchRunRequest{Event: "ticket_created", TicketIDs: []uint{ticket.ID}})
	if err != nil {
		t.Fatalf("BatchRun: %v", err)
	}
	if resp.TicketsProcessed != 1 || resp.Matches != 1 {
		t.Fatalf("unexpected batch result: %+v", resp)
	}

	if _, err := svc.BatchRun(ctx, nil); err == nil {
		t.Fatal("expected nil batch request error")
	}
}

func TestAutomationService_MatchTriggerHelpers(t *testing.T) {
	svc, db := newAutomationTestService(t)
	ctx := context.Background()

	ticket := &models.Ticket{Title: "T", Priority: "high", Status: "open", Tags: "vip"}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	attrs := map[string]interface{}{"ticket.priority": "high"}
	if !evaluateCondition(TriggerCondition{Field: "ticket.priority", Op: "eq", Value: "high"}, attrs) {
		t.Fatal("eq should match")
	}
	if evaluateCondition(TriggerCondition{Field: "ticket.priority", Op: "neq", Value: "high"}, attrs) {
		t.Fatal("neq should not match")
	}
	if !evaluateCondition(TriggerCondition{Field: "ticket.priority", Op: "contains", Value: "ig"}, attrs) {
		t.Fatal("contains should match")
	}
	if evaluateCondition(TriggerCondition{Field: "missing", Op: "eq", Value: "x"}, attrs) {
		t.Fatal("missing field should not match")
	}
	if evaluateCondition(TriggerCondition{Field: "ticket.priority", Op: "bogus", Value: "high"}, attrs) {
		t.Fatal("unknown op should not match")
	}

	if !isSupportedEvent("ticket_created") || isSupportedEvent("nope") {
		t.Fatal("unexpected supported event result")
	}

	trig := &models.AutomationTrigger{
		Name:       "m",
		Event:      "ticket.created",
		Conditions: `[{"field":"ticket.priority","op":"eq","value":"high"}]`,
		Actions:    `[{"type":"notify_log"}]`,
	}
	if !svc.matchTrigger(ctx, *trig, AutomationEvent{Type: "ticket_created", TicketID: ticket.ID}, ticket, true) {
		t.Fatal("expected trigger match with ticket")
	}
	if svc.matchTrigger(ctx, *trig, AutomationEvent{Type: "ticket_created", TicketID: ticket.ID}, nil, true) {
		t.Fatal("expected no match without ticket payload")
	}
	if !svc.matchTrigger(ctx, *trig, AutomationEvent{Type: "ticket_created", TicketID: ticket.ID}, ticket, false) {
		t.Fatal("non-dry-run match should execute notify_log and succeed")
	}
}

func TestAutomationService_SetEventBus(t *testing.T) {
	svc, _ := newAutomationTestService(t)
	// nil bus：subscriber.Register(nil) 为幂等空操作，不应 panic
	svc.SetEventBus(nil)
}
