package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
)

type stubCustomerLookup struct {
	exists bool
	err    error
	calls  []uint
}

func (s *stubCustomerLookup) Exists(ctx context.Context, customerID uint) (bool, error) {
	s.calls = append(s.calls, customerID)
	if s.err != nil {
		return false, s.err
	}
	return s.exists, nil
}

type stubSLAService struct {
	checkCalls   []*models.Ticket
	resolveCalls []struct {
		ticketID uint
		types    []string
	}
	checkErr   error
	resolveErr error
}

func (s *stubSLAService) CheckSLAViolation(ctx context.Context, ticket *models.Ticket) (*models.SLAViolation, error) {
	if ticket != nil {
		cp := *ticket
		s.checkCalls = append(s.checkCalls, &cp)
	}
	if s.checkErr != nil {
		return nil, s.checkErr
	}
	return nil, nil
}

func (s *stubSLAService) ResolveViolationsByTicket(ctx context.Context, ticketID uint, types []string) error {
	cp := append([]string(nil), types...)
	s.resolveCalls = append(s.resolveCalls, struct {
		ticketID uint
		types    []string
	}{ticketID: ticketID, types: cp})
	return s.resolveErr
}

type stubSatisfactionService struct {
	scheduled []*models.Ticket
	err       error
}

func (s *stubSatisfactionService) ScheduleSurvey(ctx context.Context, ticket *models.Ticket) (*models.SatisfactionSurvey, error) {
	if ticket != nil {
		cp := *ticket
		s.scheduled = append(s.scheduled, &cp)
	}
	if s.err != nil {
		return nil, s.err
	}
	return &models.SatisfactionSurvey{}, nil
}

type stubBus struct {
	events []eventbus.Event
	err    error
}

func (s *stubBus) Subscribe(eventName string, handler eventbus.Handler) {}

func (s *stubBus) Publish(ctx context.Context, event eventbus.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func TestApplyUpdateTicketSideEffectsPublishesAssignedEventAndEvaluatesSLA(t *testing.T) {
	now := time.Now()
	sla := &stubSLAService{}
	bus := &stubBus{}
	ticket := &models.Ticket{
		ID:         7,
		Title:      "Billing",
		CustomerID: 11,
		AgentID:    uintPtr(9),
		Status:     "resolved",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		sla,
		nil,
		bus,
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint) (*models.Ticket, error) { return ticket, nil },
		nil,
		nil,
	)

	got, err := o.ApplyUpdateTicketSideEffects(context.Background(), &TicketUpdatePreparation{
		StatusChanged: true,
		AgentChanged:  true,
	}, ticket.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != ticket {
		t.Fatalf("expected loaded ticket to be returned")
	}
	if len(bus.events) != 1 || bus.events[0].Name() != ticketapp.TicketAssignedEventName {
		t.Fatalf("expected assigned event, got %+v", bus.events)
	}
	if len(sla.resolveCalls) != 2 {
		t.Fatalf("expected two SLA resolve calls, got %+v", sla.resolveCalls)
	}
	if sla.resolveCalls[0].ticketID != ticket.ID || len(sla.resolveCalls[0].types) != 1 || sla.resolveCalls[0].types[0] != "resolution" {
		t.Fatalf("unexpected resolution resolve call: %+v", sla.resolveCalls[0])
	}
	if sla.resolveCalls[1].ticketID != ticket.ID || len(sla.resolveCalls[1].types) != 1 || sla.resolveCalls[1].types[0] != "first_response" {
		t.Fatalf("unexpected first-response resolve call: %+v", sla.resolveCalls[1])
	}
	if len(sla.checkCalls) != 1 || sla.checkCalls[0].ID != ticket.ID {
		t.Fatalf("expected one SLA check call, got %+v", sla.checkCalls)
	}
}

func TestApplyAssignTicketSideEffectsForFirstAssignment(t *testing.T) {
	sla := &stubSLAService{}
	o := NewTicketOrchestrator(nil, logrus.New(), sla, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	original := &models.Ticket{ID: 5, Status: "open"}
	updated := &models.Ticket{ID: 5, Status: "assigned", AgentID: uintPtr(12)}
	o.ApplyAssignTicketSideEffects(context.Background(), original, updated)

	if len(sla.resolveCalls) != 2 {
		t.Fatalf("expected two SLA resolve calls, got %+v", sla.resolveCalls)
	}
	if sla.resolveCalls[0].types[0] != "first_response" || sla.resolveCalls[1].types[0] != "first_response" {
		t.Fatalf("expected first-response resolve calls, got %+v", sla.resolveCalls)
	}
	if len(sla.checkCalls) != 1 || sla.checkCalls[0].ID != updated.ID {
		t.Fatalf("expected SLA check for updated ticket, got %+v", sla.checkCalls)
	}
}

func TestApplyCloseTicketSideEffectsAddsCommentResolvesSLAAndSchedulesSurvey(t *testing.T) {
	sla := &stubSLAService{}
	satisfaction := &stubSatisfactionService{}
	now := time.Now()
	closedTicket := &models.Ticket{
		ID:         8,
		Title:      "Closed",
		CustomerID: 3,
		Status:     "closed",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	var commentCalls []struct {
		ticketID    uint
		userID      uint
		content     string
		commentType string
	}
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		sla,
		satisfaction,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint) (*models.Ticket, error) { return closedTicket, nil },
		nil,
		func(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error) {
			commentCalls = append(commentCalls, struct {
				ticketID    uint
				userID      uint
				content     string
				commentType string
			}{ticketID: ticketID, userID: userID, content: content, commentType: commentType})
			return &models.TicketComment{ID: 1}, nil
		},
	)

	o.ApplyCloseTicketSideEffects(context.Background(), closedTicket.ID, 99, "done")

	if len(commentCalls) != 1 {
		t.Fatalf("expected one comment call, got %+v", commentCalls)
	}
	if commentCalls[0].ticketID != closedTicket.ID || commentCalls[0].userID != 99 || commentCalls[0].commentType != "system" {
		t.Fatalf("unexpected comment call: %+v", commentCalls[0])
	}
	if len(sla.resolveCalls) != 1 || sla.resolveCalls[0].types[0] != "resolution" {
		t.Fatalf("expected resolution SLA resolve, got %+v", sla.resolveCalls)
	}
	if len(satisfaction.scheduled) != 1 || satisfaction.scheduled[0].ID != closedTicket.ID {
		t.Fatalf("expected one survey scheduling call, got %+v", satisfaction.scheduled)
	}
}

func TestApplyCloseTicketSideEffectsSkipsSurveyWhenTicketLoadFails(t *testing.T) {
	satisfaction := &stubSatisfactionService{}
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		satisfaction,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint) (*models.Ticket, error) {
			return nil, errors.New("load failed")
		},
		nil,
		func(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error) {
			return &models.TicketComment{ID: 1}, nil
		},
	)

	o.ApplyCloseTicketSideEffects(context.Background(), 10, 7, "done")

	if len(satisfaction.scheduled) != 0 {
		t.Fatalf("expected no survey scheduling when load fails, got %+v", satisfaction.scheduled)
	}
}

func TestPrepareCreateTicketAppliesDefaultsAndBuildsCustomFieldContext(t *testing.T) {
	lookup := &stubCustomerLookup{exists: true}
	var gotProvided map[string]interface{}
	var gotContext map[string]interface{}
	var gotRequired bool
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		nil,
		nil,
		lookup.Exists,
		nil,
		func(ctx context.Context, provided map[string]interface{}, ticketCtx map[string]interface{}, enforceRequired bool) ([]models.TicketCustomFieldValue, error) {
			gotProvided = provided
			gotContext = ticketCtx
			gotRequired = enforceRequired
			return []models.TicketCustomFieldValue{{CustomFieldID: 9, Value: "vip"}}, nil
		},
		nil,
		nil,
		nil,
		nil,
	)

	prepared, err := o.PrepareCreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
		Title:        "Need help",
		Description:  "customer issue",
		CustomerID:   7,
		SessionID:    "sess-1",
		Tags:         "vip",
		CustomFields: map[string]interface{}{"tier": "gold"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(lookup.calls) != 1 || lookup.calls[0] != 7 {
		t.Fatalf("expected customer lookup call, got %+v", lookup.calls)
	}
	if prepared.Ticket.Category != "general" || prepared.Ticket.Priority != "normal" || prepared.Ticket.Source != "web" || prepared.Ticket.Status != "open" {
		t.Fatalf("expected defaults on ticket, got %+v", prepared.Ticket)
	}
	if prepared.Ticket.SessionID == nil || *prepared.Ticket.SessionID != "sess-1" {
		t.Fatalf("expected session id to be carried, got %+v", prepared.Ticket.SessionID)
	}
	if len(prepared.CustomFieldValues) != 1 || prepared.CustomFieldValues[0].CustomFieldID != 9 {
		t.Fatalf("expected prepared custom field values, got %+v", prepared.CustomFieldValues)
	}
	if gotProvided["tier"] != "gold" || !gotRequired {
		t.Fatalf("expected custom field builder to receive payload and required flag, provided=%+v required=%v", gotProvided, gotRequired)
	}
	if gotContext["ticket.category"] != "general" || gotContext["ticket.priority"] != "normal" || gotContext["ticket.source"] != "web" || gotContext["ticket.status"] != "open" {
		t.Fatalf("unexpected custom field context: %+v", gotContext)
	}
}

func TestPrepareUpdateTicketBuildsMutationContextAndStatusChange(t *testing.T) {
	oldTicket := &models.Ticket{
		ID:         3,
		Title:      "Old",
		CustomerID: 7,
		Category:   "billing",
		Priority:   "normal",
		Source:     "web",
		Status:     "open",
	}
	newTitle := "New"
	newCategory := "technical"
	newPriority := "high"
	newStatus := "resolved"
	var gotTicketID uint
	var gotProvided map[string]interface{}
	var gotContext map[string]interface{}
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint) (*models.Ticket, error) { return oldTicket, nil },
		nil,
		nil,
	)
	o.prepareCustomFieldUpdate = func(ctx context.Context, ticketID uint, provided map[string]interface{}, ticketCtx map[string]interface{}) (*ticketapp.CustomFieldMutation, error) {
		gotTicketID = ticketID
		gotProvided = provided
		gotContext = ticketCtx
		return &ticketapp.CustomFieldMutation{ClearAll: true}, nil
	}

	prepared, err := o.PrepareUpdateTicket(context.Background(), oldTicket.ID, &ticketcontract.UpdateTicketRequest{
		Title:        &newTitle,
		Category:     &newCategory,
		Priority:     &newPriority,
		Status:       &newStatus,
		CustomFields: map[string]interface{}{"env": "prod"},
	}, 42)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if prepared == nil || !prepared.StatusChanged {
		t.Fatalf("expected status change to be detected, got %+v", prepared)
	}
	if prepared.StatusChange == nil || prepared.StatusChange.FromStatus != "open" || prepared.StatusChange.ToStatus != "resolved" || prepared.StatusChange.UserID != 42 {
		t.Fatalf("unexpected status change: %+v", prepared.StatusChange)
	}
	if prepared.Updates["title"] != "New" || prepared.Updates["category"] != "technical" || prepared.Updates["priority"] != "high" || prepared.Updates["status"] != "resolved" {
		t.Fatalf("unexpected updates map: %+v", prepared.Updates)
	}
	if _, ok := prepared.Updates["resolved_at"]; !ok {
		t.Fatalf("expected resolved_at update, got %+v", prepared.Updates)
	}
	if gotTicketID != oldTicket.ID || gotProvided["env"] != "prod" {
		t.Fatalf("unexpected custom field mutation input: ticketID=%d provided=%+v", gotTicketID, gotProvided)
	}
	if gotContext["ticket.category"] != "technical" || gotContext["ticket.priority"] != "high" || gotContext["ticket.source"] != "web" || gotContext["ticket.status"] != "resolved" {
		t.Fatalf("unexpected mutation context: %+v", gotContext)
	}
	if prepared.Mutation == nil || !prepared.Mutation.ClearAll {
		t.Fatalf("expected mutation result to be carried, got %+v", prepared.Mutation)
	}
}

// TestPrepareUpdateTicketReopenClearsResolvedAt 重开政策（todo 195）的编排
// 面：resolved → open 时 updates 携带 resolved_at=nil（GORM 置 NULL 清空
// 解决时间戳），状态流水 from=resolved to=open。
func TestPrepareUpdateTicketReopenClearsResolvedAt(t *testing.T) {
	oldTicket := &models.Ticket{
		ID:         4,
		Title:      "Old",
		CustomerID: 7,
		Status:     "resolved",
	}
	newStatus := "open"
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint) (*models.Ticket, error) { return oldTicket, nil },
		nil,
		nil,
	)

	prepared, err := o.PrepareUpdateTicket(context.Background(), oldTicket.ID, &ticketcontract.UpdateTicketRequest{
		Status: &newStatus,
	}, 42)
	if err != nil {
		t.Fatalf("expected reopen to be allowed, got %v", err)
	}
	if prepared == nil || !prepared.StatusChanged {
		t.Fatalf("expected status change to be detected, got %+v", prepared)
	}
	if prepared.Updates["status"] != "open" {
		t.Fatalf("unexpected updates map: %+v", prepared.Updates)
	}
	if v, ok := prepared.Updates["resolved_at"]; !ok || v != nil {
		t.Fatalf("expected resolved_at cleared (nil) on reopen, got ok=%v v=%v", ok, v)
	}
	if prepared.StatusChange == nil || prepared.StatusChange.FromStatus != "resolved" || prepared.StatusChange.ToStatus != "open" {
		t.Fatalf("unexpected status change: %+v", prepared.StatusChange)
	}
}

func TestPrepareUpdateTicketRejectsInvalidStatusTransition(t *testing.T) {
	oldTicket := &models.Ticket{
		ID:         4,
		CustomerID: 7,
		Status:     "resolved",
		Category:   "billing",
		Priority:   "normal",
		Source:     "web",
	}
	nextStatus := "assigned"
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint) (*models.Ticket, error) { return oldTicket, nil },
		nil,
		nil,
	)

	_, err := o.PrepareUpdateTicket(context.Background(), oldTicket.ID, &ticketcontract.UpdateTicketRequest{
		Status: &nextStatus,
	}, 1)
	if err == nil {
		t.Fatal("expected invalid status transition error")
	}
}

func TestAutoAssignAgentUsesInjectedSelector(t *testing.T) {
	var assignCalls []struct {
		ticketID   uint
		agentID    uint
		assignerID uint
	}
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context) (*models.Agent, error) {
			return &models.Agent{UserID: 15}, nil
		},
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error {
			assignCalls = append(assignCalls, struct {
				ticketID   uint
				agentID    uint
				assignerID uint
			}{ticketID: ticketID, agentID: agentID, assignerID: assignerID})
			return nil
		},
		nil,
	)

	o.autoAssignAgent(21)

	if len(assignCalls) != 1 {
		t.Fatalf("expected one assign call, got %+v", assignCalls)
	}
	if assignCalls[0].ticketID != 21 || assignCalls[0].agentID != 15 || assignCalls[0].assignerID != 0 {
		t.Fatalf("unexpected assign call: %+v", assignCalls[0])
	}
}

func TestAutoAssignAgentSkipsWhenNoCandidate(t *testing.T) {
	called := false
	o := NewTicketOrchestrator(
		nil,
		logrus.New(),
		nil,
		nil,
		nil,
		nil,
		func(ctx context.Context) (*models.Agent, error) {
			return nil, errors.New("not found")
		},
		nil,
		nil,
		nil,
		func(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error {
			called = true
			return nil
		},
		nil,
	)

	o.autoAssignAgent(22)

	if called {
		t.Fatal("expected no assign call when no candidate is available")
	}
}

func uintPtr(v uint) *uint {
	return &v
}
