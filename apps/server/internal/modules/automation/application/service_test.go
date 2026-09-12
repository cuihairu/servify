package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
)

type stubRepo struct {
	triggers []models.AutomationTrigger
	runs     []string
	priority string

	listErr           error
	activeTriggersErr error
	createErr         error
	deleteErr         error
	listRunsErr       error
	listRunsRuns      []models.AutomationRun
	listRunsTotal     int64
	getTicketErr      error
	ticket            *models.Ticket
	updatePriorityErr error
	updateTagsErr     error
	createCommentErr  error

	listedEvents []string
	createdReq   *TriggerRequest
	deletedIDs   []uint
	queries      []RunListQuery
	fetched      []uint
	tags         []string
	comments     []string
}

func (s *stubRepo) ListTriggers(ctx context.Context) ([]models.AutomationTrigger, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.triggers, nil
}
func (s *stubRepo) ListActiveTriggersByEvent(ctx context.Context, event string) ([]models.AutomationTrigger, error) {
	s.listedEvents = append(s.listedEvents, event)
	if s.activeTriggersErr != nil {
		return nil, s.activeTriggersErr
	}
	return s.triggers, nil
}
func (s *stubRepo) CreateTrigger(ctx context.Context, req TriggerRequest) (*models.AutomationTrigger, error) {
	s.createdReq = &req
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &models.AutomationTrigger{ID: 1, Name: req.Name, Event: req.Event}, nil
}
func (s *stubRepo) DeleteTrigger(ctx context.Context, id uint) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}
func (s *stubRepo) ListRuns(ctx context.Context, query RunListQuery) ([]models.AutomationRun, int64, error) {
	s.queries = append(s.queries, query)
	if s.listRunsErr != nil {
		return nil, 0, s.listRunsErr
	}
	return s.listRunsRuns, s.listRunsTotal, nil
}
func (s *stubRepo) RecordRun(ctx context.Context, triggerID uint, ticketID uint, status, message string) error {
	s.runs = append(s.runs, status)
	return nil
}
func (s *stubRepo) GetTicket(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	s.fetched = append(s.fetched, ticketID)
	if s.getTicketErr != nil {
		return nil, s.getTicketErr
	}
	if s.ticket != nil {
		t := *s.ticket
		t.ID = ticketID
		return &t, nil
	}
	return &models.Ticket{ID: ticketID, Priority: "normal", Status: "open", Tags: "base"}, nil
}
func (s *stubRepo) UpdateTicketPriority(ctx context.Context, ticketID uint, priority string) error {
	if s.updatePriorityErr != nil {
		return s.updatePriorityErr
	}
	s.priority = priority
	return nil
}
func (s *stubRepo) UpdateTicketTags(ctx context.Context, ticketID uint, tags string) error {
	if s.updateTagsErr != nil {
		return s.updateTagsErr
	}
	s.tags = append(s.tags, tags)
	return nil
}
func (s *stubRepo) CreateTicketComment(ctx context.Context, ticketID uint, content string) error {
	if s.createCommentErr != nil {
		return s.createCommentErr
	}
	s.comments = append(s.comments, content)
	return nil
}

func TestBatchRunDryRunMatches(t *testing.T) {
	repo := &stubRepo{
		triggers: []models.AutomationTrigger{{
			ID:         7,
			Name:       "raise",
			Event:      "ticket.updated",
			Conditions: `[{"field":"ticket.status","op":"eq","value":"open"}]`,
			Actions:    `[{"type":"set_priority","params":{"priority":"high"}}]`,
			Active:     true,
		}},
	}
	svc := NewService(repo)
	resp, err := svc.BatchRun(context.Background(), BatchRunRequest{
		Event:     "ticket.updated",
		TicketIDs: []uint{1},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("BatchRun() error = %v", err)
	}
	if resp.Matches != 1 {
		t.Fatalf("expected 1 match, got %d", resp.Matches)
	}
	if repo.priority != "" {
		t.Fatalf("dry run should not update priority")
	}
}

func TestHandleEventRepoError(t *testing.T) {
	repo := &stubRepo{activeTriggersErr: errors.New("db down")}
	svc := NewService(repo)
	svc.HandleEvent(context.Background(), Event{Type: "ticket_updated", TicketID: 1})
	if len(repo.listedEvents) != 1 || repo.listedEvents[0] != "ticket.updated" {
		t.Fatalf("unexpected listed events: %v", repo.listedEvents)
	}
	if len(repo.fetched) != 0 || len(repo.runs) != 0 {
		t.Fatalf("expected no ticket fetch or runs, fetched=%v runs=%v", repo.fetched, repo.runs)
	}
}

func TestHandleEventNoTriggers(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	svc.HandleEvent(context.Background(), Event{Type: "ticket.created", TicketID: 3})
	if len(repo.listedEvents) != 1 {
		t.Fatalf("expected one list call, got %v", repo.listedEvents)
	}
	if len(repo.fetched) != 0 || len(repo.runs) != 0 {
		t.Fatalf("expected no ticket fetch or runs, fetched=%v runs=%v", repo.fetched, repo.runs)
	}
}

func TestHandleEventRunsMatchingTrigger(t *testing.T) {
	repo := &stubRepo{
		triggers: []models.AutomationTrigger{{
			ID:         4,
			Name:       "raise",
			Event:      "ticket.updated",
			Conditions: `[{"field":"ticket.status","op":"eq","value":"open"}]`,
			Actions:    `[{"type":"set_priority","params":{"priority":"high"}}]`,
			Active:     true,
		}},
	}
	svc := NewService(repo)
	svc.HandleEvent(context.Background(), Event{Type: "ticket.updated", TicketID: 9})
	if len(repo.fetched) != 1 || repo.fetched[0] != 9 {
		t.Fatalf("expected ticket 9 fetched, got %v", repo.fetched)
	}
	if repo.priority != "high" {
		t.Fatalf("expected priority update to high, got %q", repo.priority)
	}
	if len(repo.runs) != 1 || repo.runs[0] != "success" {
		t.Fatalf("expected one success run, got %v", repo.runs)
	}
}

func TestHandleEventContinuesAfterActionFailure(t *testing.T) {
	repo := &stubRepo{
		updatePriorityErr: errors.New("update failed"),
		triggers: []models.AutomationTrigger{
			{ID: 1, Event: "ticket.updated", Actions: `[{"type":"set_priority","params":{"priority":"high"}}]`},
			{ID: 2, Event: "ticket.updated", Actions: `[{"type":"notify_log"}]`},
		},
	}
	svc := NewService(repo)
	svc.HandleEvent(context.Background(), Event{Type: "ticket.updated", TicketID: 5})
	if len(repo.runs) != 2 || repo.runs[0] != "failed" || repo.runs[1] != "success" {
		t.Fatalf("expected failed then success runs, got %v", repo.runs)
	}
}

func TestHandleEventWithoutTicketID(t *testing.T) {
	repo := &stubRepo{
		triggers: []models.AutomationTrigger{{ID: 1, Event: "ticket.created", Actions: `[{"type":"notify_log"}]`}},
	}
	svc := NewService(repo)
	svc.HandleEvent(context.Background(), Event{Type: "ticket.created"})
	if len(repo.fetched) != 0 {
		t.Fatalf("expected no ticket fetch, got %v", repo.fetched)
	}
	if len(repo.runs) != 1 || repo.runs[0] != "success" {
		t.Fatalf("expected one success run, got %v", repo.runs)
	}
}

func TestHandleBusEventParsesAggregateID(t *testing.T) {
	repo := &stubRepo{
		triggers: []models.AutomationTrigger{{ID: 1, Event: "ticket.created", Active: true}},
	}
	svc := NewService(repo)
	svc.HandleBusEvent(context.Background(), "ticket_created", "42", nil)
	if len(repo.listedEvents) != 1 || repo.listedEvents[0] != "ticket.created" {
		t.Fatalf("unexpected listed events: %v", repo.listedEvents)
	}
	if len(repo.fetched) != 1 || repo.fetched[0] != 42 {
		t.Fatalf("expected ticket 42 fetched, got %v", repo.fetched)
	}
}

func TestHandleBusEventInvalidAggregateID(t *testing.T) {
	repo := &stubRepo{
		triggers: []models.AutomationTrigger{{ID: 1, Event: "ticket.updated", Active: true}},
	}
	svc := NewService(repo)
	svc.HandleBusEvent(context.Background(), "ticket.updated", "not-a-number", nil)
	if len(repo.listedEvents) != 1 || repo.listedEvents[0] != "ticket.updated" {
		t.Fatalf("unexpected listed events: %v", repo.listedEvents)
	}
	if len(repo.fetched) != 0 {
		t.Fatalf("expected no ticket fetch, got %v", repo.fetched)
	}
}

func TestListTriggersPassthrough(t *testing.T) {
	repo := &stubRepo{triggers: []models.AutomationTrigger{{ID: 3, Name: "t"}}}
	svc := NewService(repo)
	got, err := svc.ListTriggers(context.Background())
	if err != nil {
		t.Fatalf("ListTriggers() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("unexpected triggers: %+v", got)
	}

	repo.listErr = errors.New("list failed")
	if _, err := svc.ListTriggers(context.Background()); err == nil {
		t.Fatal("expected list error to propagate")
	}
}

func TestCreateTriggerValidation(t *testing.T) {
	svc := NewService(&stubRepo{})
	_, err := svc.CreateTrigger(context.Background(), TriggerRequest{Event: "ticket.created"})
	if err == nil || err.Error() != "name required" {
		t.Fatalf("expected name required error, got %v", err)
	}
	_, err = svc.CreateTrigger(context.Background(), TriggerRequest{Name: "n", Event: "bogus.event"})
	if err == nil || !strings.Contains(err.Error(), "unsupported event") {
		t.Fatalf("expected unsupported event error, got %v", err)
	}
}

func TestCreateTriggerNormalizesEvent(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig, err := svc.CreateTrigger(context.Background(), TriggerRequest{Name: "n", Event: "sla_violation"})
	if err != nil {
		t.Fatalf("CreateTrigger() error = %v", err)
	}
	if repo.createdReq == nil || repo.createdReq.Event != "sla.violation" {
		t.Fatalf("expected normalized event, got %+v", repo.createdReq)
	}
	if trig == nil || trig.Name != "n" || trig.Event != "sla.violation" {
		t.Fatalf("unexpected trigger: %+v", trig)
	}
}

func TestCreateTriggerRepoError(t *testing.T) {
	repo := &stubRepo{createErr: errors.New("insert failed")}
	svc := NewService(repo)
	if _, err := svc.CreateTrigger(context.Background(), TriggerRequest{Name: "n", Event: "ticket.created"}); err == nil {
		t.Fatal("expected create error to propagate")
	}
}

func TestDeleteTriggerPassthrough(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	if err := svc.DeleteTrigger(context.Background(), 11); err != nil {
		t.Fatalf("DeleteTrigger() error = %v", err)
	}
	if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != 11 {
		t.Fatalf("unexpected deleted ids: %v", repo.deletedIDs)
	}
	repo.deleteErr = errors.New("delete failed")
	if err := svc.DeleteTrigger(context.Background(), 11); err == nil {
		t.Fatal("expected delete error to propagate")
	}
}

func TestListRunsDefaultsAndClamps(t *testing.T) {
	repo := &stubRepo{listRunsRuns: []models.AutomationRun{{ID: 1}}, listRunsTotal: 1}
	svc := NewService(repo)
	runs, total, err := svc.ListRuns(context.Background(), RunListQuery{})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 1 || total != 1 {
		t.Fatalf("unexpected runs=%+v total=%d", runs, total)
	}
	if q := repo.queries[0]; q.Page != 1 || q.PageSize != 20 {
		t.Fatalf("expected defaults page=1 size=20, got %+v", q)
	}

	svc.ListRuns(context.Background(), RunListQuery{Page: 2, PageSize: 500})
	if q := repo.queries[1]; q.Page != 2 || q.PageSize != 100 {
		t.Fatalf("expected clamp page=2 size=100, got %+v", q)
	}

	svc.ListRuns(context.Background(), RunListQuery{Page: -1, PageSize: -5})
	if q := repo.queries[2]; q.Page != 1 || q.PageSize != 20 {
		t.Fatalf("expected fallback page=1 size=20, got %+v", q)
	}

	repo.listRunsErr = errors.New("query failed")
	if _, _, err := svc.ListRuns(context.Background(), RunListQuery{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list runs error to propagate")
	}
}

func TestBatchRunValidation(t *testing.T) {
	svc := NewService(&stubRepo{})
	if _, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "nope", TicketIDs: []uint{1}}); err == nil || !strings.Contains(err.Error(), "unsupported event") {
		t.Fatalf("expected unsupported event error, got %v", err)
	}
	if _, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "ticket.created"}); err == nil || err.Error() != "ticket_ids required" {
		t.Fatalf("expected ticket_ids required error, got %v", err)
	}
	ids := make([]uint, 501)
	if _, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "ticket.created", TicketIDs: ids}); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("expected too many ids error, got %v", err)
	}
}

func TestBatchRunListError(t *testing.T) {
	repo := &stubRepo{activeTriggersErr: errors.New("db down")}
	svc := NewService(repo)
	if _, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "ticket.created", TicketIDs: []uint{1}}); err == nil {
		t.Fatal("expected list error to propagate")
	}
}

func TestBatchRunSkipsUnloadedTickets(t *testing.T) {
	repo := &stubRepo{
		getTicketErr: errors.New("ticket missing"),
		triggers: []models.AutomationTrigger{{
			ID:    1,
			Event: "ticket.created",
		}},
	}
	svc := NewService(repo)
	resp, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "ticket.created", TicketIDs: []uint{1, 2}})
	if err != nil {
		t.Fatalf("BatchRun() error = %v", err)
	}
	if resp.TicketsProcessed != 0 || resp.Matches != 0 || len(resp.Results) != 0 {
		t.Fatalf("expected empty response, got %+v", resp)
	}
}

func TestBatchRunCollectsMatches(t *testing.T) {
	repo := &stubRepo{
		triggers: []models.AutomationTrigger{
			{
				ID:         1,
				Event:      "ticket.updated",
				Conditions: `[{"field":"ticket.priority","op":"eq","value":"normal"}]`,
				Actions:    `[{"type":"set_priority","params":{"priority":"high"}}]`,
			},
			{
				ID:         2,
				Event:      "ticket.updated",
				Conditions: `[{"field":"ticket.priority","op":"eq","value":"urgent"}]`,
			},
		},
	}
	svc := NewService(repo)
	resp, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "ticket_updated", TicketIDs: []uint{1, 2}})
	if err != nil {
		t.Fatalf("BatchRun() error = %v", err)
	}
	if resp.Event != "ticket.updated" {
		t.Fatalf("expected normalized event, got %q", resp.Event)
	}
	if resp.TicketsProcessed != 2 || resp.Matches != 2 {
		t.Fatalf("unexpected processed=%d matches=%d", resp.TicketsProcessed, resp.Matches)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %+v", resp.Results)
	}
	for _, r := range resp.Results {
		if len(r.MatchedTriggerIDs) != 1 || r.MatchedTriggerIDs[0] != 1 {
			t.Fatalf("unexpected matched triggers: %+v", r)
		}
	}
	if repo.priority != "high" {
		t.Fatalf("expected priority update, got %q", repo.priority)
	}
}

func TestBatchRunActionFailureNotMatched(t *testing.T) {
	repo := &stubRepo{
		updatePriorityErr: errors.New("update failed"),
		triggers: []models.AutomationTrigger{{
			ID:      1,
			Event:   "ticket.updated",
			Actions: `[{"type":"set_priority","params":{"priority":"high"}}]`,
		}},
	}
	svc := NewService(repo)
	resp, err := svc.BatchRun(context.Background(), BatchRunRequest{Event: "ticket.updated", TicketIDs: []uint{1}})
	if err != nil {
		t.Fatalf("BatchRun() error = %v", err)
	}
	if resp.Matches != 0 {
		t.Fatalf("expected no matches, got %d", resp.Matches)
	}
	if resp.TicketsProcessed != 1 || len(resp.Results) != 1 || len(resp.Results[0].MatchedTriggerIDs) != 0 {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
	if len(repo.runs) != 1 || repo.runs[0] != "failed" {
		t.Fatalf("expected failed run, got %v", repo.runs)
	}
}

func TestMatchTriggerInvalidConditionsJSON(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Conditions: "{invalid"}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected no match on invalid conditions json")
	}
	if len(repo.runs) != 0 {
		t.Fatalf("expected no runs recorded, got %v", repo.runs)
	}
}

func TestMatchTriggerInvalidActionsJSON(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: "{invalid"}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected no match on invalid actions json")
	}
	if len(repo.runs) != 0 {
		t.Fatalf("expected no runs recorded, got %v", repo.runs)
	}
}

func TestMatchTriggerDryRunSkipsActions(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: "{invalid"}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, true) {
		t.Fatal("expected dry run to match without parsing actions")
	}
	if len(repo.runs) != 0 || repo.priority != "" {
		t.Fatalf("expected no side effects, runs=%v priority=%q", repo.runs, repo.priority)
	}
}

func TestMatchTriggerConditionOnMissingField(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Conditions: `[{"field":"ticket.status","op":"eq","value":"open"}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, nil, false) {
		t.Fatal("expected no match when ticket attributes missing")
	}
	if len(repo.runs) != 0 {
		t.Fatalf("expected no runs recorded, got %v", repo.runs)
	}
}

func TestMatchTriggerSLAViolationPayload(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{
		ID:         1,
		Conditions: `[{"field":"violation.type","op":"eq","value":"resolution"}]`,
		Actions:    `[{"type":"notify_log"}]`,
	}
	evt := Event{Type: "sla.violation", Payload: &models.SLAViolation{ViolationType: "resolution"}}
	if !svc.MatchTrigger(context.Background(), trig, evt, nil, false) {
		t.Fatal("expected sla violation payload to match")
	}

	evt.Payload = &models.SLAViolation{ViolationType: "first_response"}
	if svc.MatchTrigger(context.Background(), trig, evt, nil, false) {
		t.Fatal("expected mismatched violation type to fail")
	}
}

func TestMatchTriggerNonPointerPayloadIgnored(t *testing.T) {
	svc := NewService(&stubRepo{})
	trig := models.AutomationTrigger{
		ID:         1,
		Conditions: `[{"field":"violation.type","op":"eq","value":"resolution"}]`,
	}
	evt := Event{Type: "sla.violation", Payload: models.SLAViolation{ViolationType: "resolution"}}
	if svc.MatchTrigger(context.Background(), trig, evt, nil, false) {
		t.Fatal("expected non-pointer payload to expose no violation attrs")
	}
}

func TestMatchTriggerSuccessRecordsRun(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 8, Actions: `[{"type":"notify_log"}]`}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.created", TicketID: 3}, nil, false) {
		t.Fatal("expected match")
	}
	if len(repo.runs) != 1 || repo.runs[0] != "success" {
		t.Fatalf("expected success run, got %v", repo.runs)
	}
}

func TestExecuteActionSetPriorityRequiresTicket(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"set_priority","params":{"priority":"high"}}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, nil, false) {
		t.Fatal("expected failure without ticket")
	}
	if len(repo.runs) != 1 || repo.runs[0] != "failed" {
		t.Fatalf("expected failed run, got %v", repo.runs)
	}
}

func TestExecuteActionSetPriorityRequiresParam(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"set_priority"}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected failure without priority param")
	}
	if repo.priority != "" {
		t.Fatalf("expected no priority update, got %q", repo.priority)
	}
}

func TestExecuteActionSetPriorityUpdatesTicket(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"set_priority","params":{"priority":"urgent"}}]`}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated", TicketID: 6}, &TicketView{ID: 6}, false) {
		t.Fatal("expected success")
	}
	if repo.priority != "urgent" {
		t.Fatalf("expected urgent priority, got %q", repo.priority)
	}
}

func TestExecuteActionAddTagRequiresTicket(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"add_tag","params":{"tag":"vip"}}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, nil, false) {
		t.Fatal("expected failure without ticket")
	}
	if len(repo.tags) != 0 {
		t.Fatalf("expected no tag update, got %v", repo.tags)
	}
}

func TestExecuteActionAddTagRequiresParam(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"add_tag"}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected failure without tag param")
	}
	if len(repo.tags) != 0 {
		t.Fatalf("expected no tag update, got %v", repo.tags)
	}
}

func TestExecuteActionAddTagVariants(t *testing.T) {
	cases := []struct {
		name    string
		tags    string
		tag     string
		want    string
		wantSet bool
	}{
		{"empty tags", "", "vip", "vip", true},
		{"append tag", "base", "vip", "base,vip", true},
		{"existing tag", "base,vip", "vip", "base,vip", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{}
			svc := NewService(repo)
			trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"add_tag","params":{"tag":"` + tc.tag + `"}}]`}
			if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1, Tags: tc.tags}, false) {
				t.Fatal("expected success")
			}
			if len(repo.tags) != 1 || repo.tags[0] != tc.want {
				t.Fatalf("expected tags %q, got %v", tc.want, repo.tags)
			}
		})
	}
}

func TestExecuteActionAddCommentRequiresTicket(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"add_comment","params":{"content":"hi"}}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, nil, false) {
		t.Fatal("expected failure without ticket")
	}
	if len(repo.comments) != 0 {
		t.Fatalf("expected no comment, got %v", repo.comments)
	}
}

func TestExecuteActionAddCommentRequiresContent(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"add_comment"}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected failure without content")
	}
	if len(repo.comments) != 0 {
		t.Fatalf("expected no comment, got %v", repo.comments)
	}
}

func TestExecuteActionAddCommentCreatesComment(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"add_comment","params":{"content":"auto note"}}]`}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated", TicketID: 2}, &TicketView{ID: 2}, false) {
		t.Fatal("expected success")
	}
	if len(repo.comments) != 1 || repo.comments[0] != "auto note" {
		t.Fatalf("unexpected comments: %v", repo.comments)
	}
}

func TestExecuteActionNotifyLogNoop(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"notify_log"}]`}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, nil, false) {
		t.Fatal("expected success")
	}
	if repo.priority != "" || len(repo.tags) != 0 || len(repo.comments) != 0 {
		t.Fatalf("expected no side effects: priority=%q tags=%v comments=%v", repo.priority, repo.tags, repo.comments)
	}
}

func TestExecuteActionUnsupportedType(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"explode"}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected failure on unsupported action")
	}
	if len(repo.runs) != 1 || repo.runs[0] != "failed" {
		t.Fatalf("expected failed run, got %v", repo.runs)
	}
}

type recordingDispatcher struct {
	url     string
	secret  string
	payload map[string]interface{}
	err     error
}

func (r *recordingDispatcher) Dispatch(ctx context.Context, url, secret string, payload map[string]interface{}) error {
	r.url, r.secret, r.payload = url, secret, payload
	return r.err
}

func TestExecuteActionCallWebhookDispatches(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	repo := &stubRepo{}
	svc := NewService(repo)
	svc.SetWebhookDispatcher(dispatcher)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"call_webhook","params":{"url":"https://ops.example.com/hook","secret":"s3cret"}}]`}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated", TicketID: 5}, &TicketView{ID: 5, Title: "printer", Priority: "high"}, false) {
		t.Fatal("expected success with dispatcher configured")
	}
	if dispatcher.url != "https://ops.example.com/hook" || dispatcher.secret != "s3cret" {
		t.Fatalf("unexpected dispatch target: %s %s", dispatcher.url, dispatcher.secret)
	}
	if dispatcher.payload["id"] != float64(5) || dispatcher.payload["title"] != "printer" {
		t.Fatalf("expected default ticket snapshot payload, got %+v", dispatcher.payload)
	}
}

func TestExecuteActionCallWebhookCustomPayload(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	svc := NewService(&stubRepo{})
	svc.SetWebhookDispatcher(dispatcher)
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"call_webhook","params":{"url":"https://x.example.com","payload":{"text":"escalated"}}}]`}
	if !svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated", TicketID: 5}, &TicketView{ID: 5}, false) {
		t.Fatal("expected success")
	}
	if len(dispatcher.payload) != 1 || dispatcher.payload["text"] != "escalated" {
		t.Fatalf("expected custom payload to override snapshot, got %+v", dispatcher.payload)
	}
}

func TestExecuteActionCallWebhookRequiresURL(t *testing.T) {
	svc := NewService(&stubRepo{})
	svc.SetWebhookDispatcher(&recordingDispatcher{})
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"call_webhook","params":{"secret":"s"}}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected failure without url param")
	}
}

func TestExecuteActionCallWebhookRequiresDispatcher(t *testing.T) {
	svc := NewService(&stubRepo{})
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"call_webhook","params":{"url":"https://x.example.com"}}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected failure without dispatcher")
	}
}

func TestExecuteActionCallWebhookDispatchErrorFailsRun(t *testing.T) {
	svc := NewService(&stubRepo{})
	svc.SetWebhookDispatcher(&recordingDispatcher{err: errors.New("boom")})
	trig := models.AutomationTrigger{ID: 1, Actions: `[{"type":"call_webhook","params":{"url":"https://x.example.com"}}]`}
	if svc.MatchTrigger(context.Background(), trig, Event{Type: "ticket.updated"}, &TicketView{ID: 1}, false) {
		t.Fatal("expected dispatch error to fail the trigger")
	}
}

func TestDefaultWebhookPayloadVariants(t *testing.T) {
	if got := defaultWebhookPayload(nil); len(got) != 0 {
		t.Fatalf("expected empty map for nil ticket, got %+v", got)
	}
	got := defaultWebhookPayload(&models.Ticket{ID: 7})
	if got["id"] != float64(7) {
		t.Fatalf("expected marshaled snapshot, got %+v", got)
	}
}

func TestEvaluateCondition(t *testing.T) {
	attrs := map[string]interface{}{
		"ticket.status": "open",
		"ticket.tags":   "a,b",
		"ticket.count":  3,
	}
	cases := []struct {
		name string
		cond TriggerCondition
		want bool
	}{
		{"eq match", TriggerCondition{Field: "ticket.status", Op: "eq", Value: "open"}, true},
		{"eq mismatch", TriggerCondition{Field: "ticket.status", Op: "eq", Value: "closed"}, false},
		{"eq numeric formatted", TriggerCondition{Field: "ticket.count", Op: "eq", Value: 3}, true},
		{"eq numeric mismatch", TriggerCondition{Field: "ticket.count", Op: "eq", Value: 4}, false},
		{"neq true", TriggerCondition{Field: "ticket.status", Op: "neq", Value: "closed"}, true},
		{"neq false", TriggerCondition{Field: "ticket.status", Op: "neq", Value: "open"}, false},
		{"contains match", TriggerCondition{Field: "ticket.tags", Op: "contains", Value: "a"}, true},
		{"contains mismatch", TriggerCondition{Field: "ticket.tags", Op: "contains", Value: "z"}, false},
		{"unknown op", TriggerCondition{Field: "ticket.status", Op: "gt", Value: "open"}, false},
		{"missing field", TriggerCondition{Field: "ticket.missing", Op: "eq", Value: "x"}, false},
		{"empty op", TriggerCondition{Field: "ticket.status", Op: "", Value: "open"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateCondition(tc.cond, attrs); got != tc.want {
				t.Fatalf("EvaluateCondition(%+v) = %v, want %v", tc.cond, got, tc.want)
			}
		})
	}
}

func TestNormalizeEvent(t *testing.T) {
	cases := map[string]string{
		"ticket_created": "ticket.created",
		"ticket_updated": "ticket.updated",
		"sla_violation":  "sla.violation",
		"ticket.created": "ticket.created",
		"other":          "other",
	}
	for in, want := range cases {
		if got := normalizeEvent(in); got != want {
			t.Fatalf("normalizeEvent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSupportedEvent(t *testing.T) {
	supported := []string{
		"ticket.created",
		"ticket.updated",
		"ticket.closed",
		"ticket.assigned",
		"conversation.created",
		"conversation.message_received",
		"routing.agent_assigned",
		"routing.transfer_completed",
		"sla.violation",
	}
	for _, e := range supported {
		if !IsSupportedEvent(e) {
			t.Fatalf("expected %q supported", e)
		}
	}
	aliases := []string{"ticket_created", "ticket_updated", "sla_violation"}
	for _, e := range aliases {
		if !IsSupportedEvent(e) {
			t.Fatalf("expected alias %q supported", e)
		}
	}
	if IsSupportedEvent("bogus") {
		t.Fatal("expected bogus event unsupported")
	}
	if isSupportedEvent("bogus") {
		t.Fatal("expected bogus event unsupported (internal)")
	}
}
