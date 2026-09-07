package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ticket/domain"
)

// flakyCommandRepo extends stubCommandRepo with per-method error injection.
type flakyCommandRepo struct {
	stubCommandRepo
	createErr     error
	updateErr     error
	updateStatErr error
	assignErr     error
	unassignErr   error
	addCommentErr error
	recordErr     error
	existsErr     error
	assignableErr error
	getErrAt      map[int]error // 1-based call index -> error for GetTicket
	getCalls      int
}

func (s *flakyCommandRepo) CreateTicket(ctx context.Context, ticket *domain.Ticket) error {
	if s.createErr != nil {
		return s.createErr
	}
	return s.stubCommandRepo.CreateTicket(ctx, ticket)
}

func (s *flakyCommandRepo) UpdateTicket(ctx context.Context, ticket *domain.Ticket) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.stubCommandRepo.UpdateTicket(ctx, ticket)
}

func (s *flakyCommandRepo) UpdateTicketWithStatus(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error {
	if s.updateStatErr != nil {
		return s.updateStatErr
	}
	return s.stubCommandRepo.UpdateTicketWithStatus(ctx, ticket, fromStatus, userID, reason)
}

func (s *flakyCommandRepo) AssignTicket(ctx context.Context, ticket *domain.Ticket, previousAgentID *uint, fromStatus string, userID uint, reason string) error {
	if s.assignErr != nil {
		return s.assignErr
	}
	return s.stubCommandRepo.AssignTicket(ctx, ticket, previousAgentID, fromStatus, userID, reason)
}

func (s *flakyCommandRepo) UnassignTicket(ctx context.Context, ticket *domain.Ticket, previousAgentID uint, fromStatus string, userID uint, reason string) error {
	if s.unassignErr != nil {
		return s.unassignErr
	}
	return s.stubCommandRepo.UnassignTicket(ctx, ticket, previousAgentID, fromStatus, userID, reason)
}

func (s *flakyCommandRepo) AddComment(ctx context.Context, ticketID uint, comment *domain.Comment) error {
	if s.addCommentErr != nil {
		return s.addCommentErr
	}
	return s.stubCommandRepo.AddComment(ctx, ticketID, comment)
}

func (s *flakyCommandRepo) RecordStatusChange(ctx context.Context, ticketID uint, change *domain.StatusChange) error {
	if s.recordErr != nil {
		return s.recordErr
	}
	return s.stubCommandRepo.RecordStatusChange(ctx, ticketID, change)
}

func (s *flakyCommandRepo) CustomerExists(ctx context.Context, customerID uint) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.stubCommandRepo.CustomerExists(ctx, customerID)
}

func (s *flakyCommandRepo) AgentAssignable(ctx context.Context, agentID uint) (bool, error) {
	if s.assignableErr != nil {
		return false, s.assignableErr
	}
	return s.stubCommandRepo.AgentAssignable(ctx, agentID)
}

func (s *flakyCommandRepo) GetTicket(ctx context.Context, ticketID uint) (*domain.Ticket, error) {
	s.getCalls++
	if err, ok := s.getErrAt[s.getCalls]; ok {
		return nil, err
	}
	return s.stubCommandRepo.GetTicket(ctx, ticketID)
}

func strPtr(v string) *string { return &v }
func uintPtrCmd(v uint) *uint { return &v }

func TestCoverageCreateTicketErrorBranches(t *testing.T) {
	ctx := context.Background()

	if _, err := NewCommandService(&flakyCommandRepo{}).CreateTicket(ctx, CreateTicketCommand{Title: "  "}); err == nil || err.Error() != "title required" {
		t.Fatalf("expected title required error, got %v", err)
	}
	if _, err := NewCommandService(&flakyCommandRepo{}).CreateTicket(ctx, CreateTicketCommand{Title: "t", CustomerID: 0}); err == nil || err.Error() != "customer id required" {
		t.Fatalf("expected customer id required error, got %v", err)
	}

	repo := &flakyCommandRepo{existsErr: errors.New("lookup failed")}
	if _, err := NewCommandService(repo).CreateTicket(ctx, CreateTicketCommand{Title: "t", CustomerID: 1}); err == nil || err.Error() != "lookup failed" {
		t.Fatalf("expected lookup error, got %v", err)
	}

	repo = &flakyCommandRepo{stubCommandRepo: stubCommandRepo{customerExists: false}}
	if _, err := NewCommandService(repo).CreateTicket(ctx, CreateTicketCommand{Title: "t", CustomerID: 1}); err == nil || err.Error() != "customer not found" {
		t.Fatalf("expected customer not found error, got %v", err)
	}

	repo = &flakyCommandRepo{stubCommandRepo: stubCommandRepo{customerExists: true}, createErr: errors.New("insert failed")}
	if _, err := NewCommandService(repo).CreateTicket(ctx, CreateTicketCommand{Title: "t", CustomerID: 1}); err == nil {
		t.Fatal("expected create error")
	}

	repo = &flakyCommandRepo{stubCommandRepo: stubCommandRepo{customerExists: true}, recordErr: errors.New("record failed")}
	if _, err := NewCommandService(repo).CreateTicket(ctx, CreateTicketCommand{Title: "t", CustomerID: 1}); err == nil {
		t.Fatal("expected record status change error")
	}

	repo = &flakyCommandRepo{stubCommandRepo: stubCommandRepo{customerExists: true}}
	svc := NewCommandServiceWithBus(repo, &stubEventBus{err: errors.New("bus down")})
	if _, err := svc.CreateTicket(ctx, CreateTicketCommand{Title: "t", CustomerID: 1}); err == nil {
		t.Fatal("expected publish error")
	}
}

func TestCoverageUpdateTicketBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	seed := func(status string, agentID *uint) *flakyCommandRepo {
		return &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T", CustomerID: 2, Status: status, AgentID: agentID, CreatedAt: now, UpdatedAt: now},
			},
			customerExists: true,
		}}
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).UpdateTicket(ctx, 0, UpdateTicketCommand{}); err == nil || err.Error() != "ticket id required" {
		t.Fatalf("expected ticket id required, got %v", err)
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).UpdateTicket(ctx, 9, UpdateTicketCommand{}); err == nil {
		t.Fatal("expected get ticket error")
	}

	// blank title after trim
	repo := seed("open", nil)
	if _, err := NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Title: strPtr("   ")}); err == nil || err.Error() != "title required" {
		t.Fatalf("expected title required, got %v", err)
	}

	// invalid transition
	repo = seed("open", nil)
	bad := "bogus"
	if _, err := NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Status: &bad}); err == nil {
		t.Fatal("expected invalid status transition error")
	}

	// same status -> regular update path, no status change record
	repo = seed("open", nil)
	same := "open"
	dto, err := NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Status: &same, DueDate: &now})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.tickets[1].Description != "" || repo.tickets[1].DueDate == nil {
		t.Fatalf("expected due date applied on stored ticket: %+v", repo.tickets[1])
	}

	// resolved transition -> UpdateTicketWithStatus + resolved_at
	repo = seed("open", nil)
	resolved := "resolved"
	dto, err = NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Status: &resolved})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.ResolvedAt == nil {
		t.Fatal("expected resolved_at to be set")
	}

	// closed transition
	repo = seed("open", nil)
	closed := "closed"
	dto, err = NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Status: &closed})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.ClosedAt == nil {
		t.Fatal("expected closed_at to be set")
	}

	// update failures
	repo = seed("open", nil)
	repo.updateErr = errors.New("update failed")
	if _, err := NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Title: strPtr("x")}); err == nil {
		t.Fatal("expected update error")
	}

	repo = seed("open", nil)
	repo.updateStatErr = errors.New("update status failed")
	resolved2 := "resolved"
	if _, err := NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{Status: &resolved2}); err == nil {
		t.Fatal("expected update-with-status error")
	}

	// agent changed but publish fails
	repo = seed("open", nil)
	svc := NewCommandServiceWithBus(repo, &stubEventBus{err: errors.New("bus down")})
	agent := uint(5)
	if _, err := svc.UpdateTicket(ctx, 1, UpdateTicketCommand{AgentID: &agent}); err == nil {
		t.Fatal("expected publish error")
	}
}

func TestCoverageAssignTicketBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	seed := func(status string, agentID *uint, available bool) *flakyCommandRepo {
		return &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T", CustomerID: 2, Status: status, AgentID: agentID, CreatedAt: now, UpdatedAt: now},
			},
			customerExists: true,
			agentAvailable: available,
		}}
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).AssignTicket(ctx, 0, AssignTicketCommand{AgentID: 1}); err == nil || err.Error() != "ticket id required" {
		t.Fatalf("expected ticket id required, got %v", err)
	}
	if _, err := NewCommandService(&flakyCommandRepo{}).AssignTicket(ctx, 1, AssignTicketCommand{}); err == nil || err.Error() != "agent id required" {
		t.Fatalf("expected agent id required, got %v", err)
	}
	if _, err := NewCommandService(&flakyCommandRepo{}).AssignTicket(ctx, 9, AssignTicketCommand{AgentID: 1}); err == nil {
		t.Fatal("expected get ticket error")
	}

	repo := seed("open", nil, true)
	repo.assignableErr = errors.New("assignable check failed")
	if _, err := NewCommandService(repo).AssignTicket(ctx, 1, AssignTicketCommand{AgentID: 1}); err == nil {
		t.Fatal("expected assignable error")
	}

	repo = seed("open", nil, false)
	if _, err := NewCommandService(repo).AssignTicket(ctx, 1, AssignTicketCommand{AgentID: 1}); err == nil || err.Error() != "agent not available" {
		t.Fatalf("expected agent not available, got %v", err)
	}

	repo = seed("open", nil, true)
	repo.assignErr = errors.New("assign failed")
	if _, err := NewCommandService(repo).AssignTicket(ctx, 1, AssignTicketCommand{AgentID: 1}); err == nil {
		t.Fatal("expected assign error")
	}

	repo = seed("open", nil, true)
	svc := NewCommandServiceWithBus(repo, &stubEventBus{err: errors.New("bus down")})
	if _, err := svc.AssignTicket(ctx, 1, AssignTicketCommand{AgentID: 1}); err == nil {
		t.Fatal("expected publish error")
	}

	// transfer (previous agent set) and blank-status ticket
	prev := uint(3)
	repo = seed("assigned", &prev, true)
	dto, err := NewCommandService(repo).AssignTicket(ctx, 1, AssignTicketCommand{AgentID: 7, UserID: 9})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *dto.AgentID != 7 || dto.Status != "assigned" {
		t.Fatalf("unexpected dto: %+v", dto)
	}
	if changes := repo.statusChanges[1]; len(changes) != 1 || changes[0].Reason != "transfer_ticket" {
		t.Fatalf("expected transfer reason, got %+v", changes)
	}

	repo = seed("", nil, true)
	dto, err = NewCommandService(repo).AssignTicket(ctx, 1, AssignTicketCommand{AgentID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Status != "assigned" {
		t.Fatalf("expected assigned status, got %+v", dto)
	}
}

func TestCoverageUnassignTicketBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	agent := uint(4)
	seed := func(status string, agentID *uint) *flakyCommandRepo {
		return &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T", CustomerID: 2, Status: status, AgentID: agentID, CreatedAt: now, UpdatedAt: now},
			},
		}}
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).UnassignTicket(ctx, 0, UnassignTicketCommand{}); err == nil || err.Error() != "ticket id required" {
		t.Fatalf("expected ticket id required, got %v", err)
	}
	if _, err := NewCommandService(&flakyCommandRepo{}).UnassignTicket(ctx, 9, UnassignTicketCommand{}); err == nil {
		t.Fatal("expected get ticket error")
	}

	// no agent assigned: returns dto untouched
	repo := seed("open", nil)
	dto, err := NewCommandService(repo).UnassignTicket(ctx, 1, UnassignTicketCommand{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.AgentID != nil {
		t.Fatalf("expected nil agent, got %+v", dto)
	}

	// default reason and open fallback
	repo = seed("assigned", &agent)
	dto, err = NewCommandService(repo).UnassignTicket(ctx, 1, UnassignTicketCommand{UserID: 8})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.AgentID != nil || dto.Status != "open" {
		t.Fatalf("unexpected dto: %+v", dto)
	}
	if changes := repo.statusChanges[1]; len(changes) != 1 || changes[0].Reason != "unassign_ticket" {
		t.Fatalf("expected default reason, got %+v", changes)
	}

	// custom reason
	repo = seed("in_progress", &agent)
	if _, err := NewCommandService(repo).UnassignTicket(ctx, 1, UnassignTicketCommand{UserID: 8, Reason: "  offboarding "}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changes := repo.statusChanges[1]; len(changes) != 1 || changes[0].Reason != "offboarding" {
		t.Fatalf("expected custom reason, got %+v", changes)
	}

	// failure
	repo = seed("assigned", &agent)
	repo.unassignErr = errors.New("unassign failed")
	if _, err := NewCommandService(repo).UnassignTicket(ctx, 1, UnassignTicketCommand{}); err == nil {
		t.Fatal("expected unassign error")
	}
}

func TestCoverageAddCommentBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	repo := &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {ID: 1, Title: "T", CustomerID: 2, Status: "open", CreatedAt: now, UpdatedAt: now},
		},
	}}
	svc := NewCommandService(repo)

	if _, err := svc.AddComment(ctx, 0, AddCommentCommand{UserID: 1, Content: "x"}); err == nil || err.Error() != "ticket id required" {
		t.Fatalf("expected ticket id required, got %v", err)
	}
	if _, err := svc.AddComment(ctx, 1, AddCommentCommand{Content: "x"}); err == nil || err.Error() != "user id required" {
		t.Fatalf("expected user id required, got %v", err)
	}
	if _, err := svc.AddComment(ctx, 1, AddCommentCommand{UserID: 1, Content: "   "}); err == nil || err.Error() != "content required" {
		t.Fatalf("expected content required, got %v", err)
	}
	if _, err := svc.AddComment(ctx, 9, AddCommentCommand{UserID: 1, Content: "x"}); err == nil {
		t.Fatal("expected get ticket error")
	}

	repo.addCommentErr = errors.New("insert failed")
	if _, err := svc.AddComment(ctx, 1, AddCommentCommand{UserID: 1, Content: "x"}); err == nil {
		t.Fatal("expected add comment error")
	}
	repo.addCommentErr = nil

	dto, err := svc.AddComment(ctx, 1, AddCommentCommand{UserID: 1, Content: " hi ", CommentType: "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Content != "hi" || dto.Type != "comment" {
		t.Fatalf("expected trimmed content and default type, got %+v", dto)
	}
}

func TestCoverageCloseTicketBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	seed := func() *flakyCommandRepo {
		return &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T", CustomerID: 2, Status: "open", CreatedAt: now, UpdatedAt: now},
			},
		}}
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).CloseTicket(ctx, 0, CloseTicketCommand{}); err == nil || err.Error() != "ticket id required" {
		t.Fatalf("expected ticket id required, got %v", err)
	}
	if _, err := NewCommandService(&flakyCommandRepo{}).CloseTicket(ctx, 9, CloseTicketCommand{}); err == nil {
		t.Fatal("expected get ticket error")
	}

	repo := seed()
	repo.closeCalls = 0
	repo.stubCommandRepo.err = errors.New("close failed")
	if _, err := NewCommandService(repo).CloseTicket(ctx, 1, CloseTicketCommand{}); err == nil {
		t.Fatal("expected close error")
	}
	repo.stubCommandRepo.err = nil

	repo = seed()
	svc := NewCommandServiceWithBus(repo, &stubEventBus{err: errors.New("bus down")})
	if _, err := svc.CloseTicket(ctx, 1, CloseTicketCommand{}); err == nil {
		t.Fatal("expected publish error")
	}
}

func TestCoverageBulkUpdateBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	seed := func() *flakyCommandRepo {
		return &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T1", CustomerID: 2, Status: "open", Tags: "a,b", CreatedAt: now, UpdatedAt: now},
				2: {ID: 2, Title: "T2", CustomerID: 2, Status: "open", CreatedAt: now, UpdatedAt: now},
			},
			customerExists: true,
			agentAvailable: true,
		}}
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{}); err == nil || err.Error() != "ticket ids required" {
		t.Fatalf("expected ticket ids required, got %v", err)
	}

	agent := uint(3)
	if _, err := NewCommandService(&flakyCommandRepo{}).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{1}, AgentID: &agent, UnassignAgent: true}); err == nil || err.Error() != "cannot set both unassign_agent and agent_id" {
		t.Fatalf("expected conflict error, got %v", err)
	}

	if _, err := NewCommandService(&flakyCommandRepo{}).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{0, 0}}); err == nil || err.Error() != "no valid ticket ids" {
		t.Fatalf("expected no valid ids error, got %v", err)
	}

	// mixed success/failure with duplicate ids, status + tag deltas
	repo := seed()
	svc := NewCommandService(repo)
	closed := "closed"
	result, err := svc.BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{
		TicketIDs:  []uint{1, 1, 2, 99, 0},
		Status:     &closed,
		AddTags:    []string{" C ", "c"},
		RemoveTags: []string{"a"},
		UserID:     7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Updated) != 2 || result.Updated[0] != 1 || result.Updated[1] != 2 {
		t.Fatalf("unexpected updated ids: %+v", result.Updated)
	}
	if len(result.Failed) != 1 || result.Failed[0].TicketID != 99 || result.Failed[0].Error == "" {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}
	if got := repo.tickets[1].Tags; got != "C,b" {
		t.Fatalf("expected tag delta applied, got %q", got)
	}
	if repo.tickets[1].Status != "closed" {
		t.Fatalf("expected closed status, got %q", repo.tickets[1].Status)
	}

	// set tags overrides deltas
	repo = seed()
	svc = NewCommandService(repo)
	setTags := " x , y "
	result, err = svc.BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{1}, SetTags: &setTags})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Updated) != 1 || repo.tickets[1].Tags != "x,y" {
		t.Fatalf("expected set tags, got %+v %q", result.Updated, repo.tickets[1].Tags)
	}

	// unassign ticket without agent -> no-op, no update needed
	repo = seed()
	svc = NewCommandService(repo)
	result, err = svc.BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{2}, UnassignAgent: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Updated) != 1 {
		t.Fatalf("expected ticket 2 updated, got %+v", result.Updated)
	}

	// unassign ticket with agent -> UnassignTicket path
	repo = seed()
	ag := uint(8)
	repo.tickets[2].AgentID = &ag
	repo.tickets[2].Status = "assigned"
	result, err = NewCommandService(repo).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{2}, UnassignAgent: true, Status: strPtr("resolved")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Updated) != 1 || repo.tickets[2].Status != "resolved" {
		t.Fatalf("unexpected state: %+v status=%s", result.Updated, repo.tickets[2].Status)
	}

	// agent assignment path succeeds then re-fetch fails
	repo = seed()
	repo.getErrAt = map[int]error{2: errors.New("refetch failed")}
	result, err = NewCommandService(repo).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{1}, AgentID: &agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].TicketID != 1 {
		t.Fatalf("expected refetch failure recorded, got %+v", result.Failed)
	}

	// agent assignment failure path
	repo = seed()
	repo.agentAvailable = false
	result, err = NewCommandService(repo).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{1}, AgentID: &agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("expected failure recorded, got %+v", result.Failed)
	}

	// update failure path
	repo = seed()
	repo.updateErr = errors.New("update failed")
	result, err = NewCommandService(repo).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{1}, SetTags: strPtr("z")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("expected failure recorded, got %+v", result.Failed)
	}
}

func TestCoverageTagHelpers(t *testing.T) {
	if got := splitTags("   "); got != nil {
		t.Fatalf("expected nil for blank tags, got %+v", got)
	}
	got := splitTags(" a ,, b ,")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected split result: %+v", got)
	}

	if got := normalizeTags(nil); got != nil {
		t.Fatalf("expected nil for empty tags, got %+v", got)
	}
	got = normalizeTags([]string{" B ", "", "b", "a"})
	if len(got) != 2 || got[0] != "B" || got[1] != "a" {
		t.Fatalf("unexpected normalized tags: %+v", got)
	}

	delta := applyTagDelta("a, b", []string{" C ", "c"}, []string{"a"})
	if len(delta) != 2 || delta[0] != "C" || delta[1] != "b" {
		t.Fatalf("unexpected delta: %+v", delta)
	}
}

func TestCoverageRecordStatusChangeNoop(t *testing.T) {
	repo := &flakyCommandRepo{}
	svc := NewCommandService(repo)
	if err := svc.recordStatusChange(context.Background(), 1, 0, "open", "open", "noop"); err != nil {
		t.Fatalf("expected nil for identical statuses, got %v", err)
	}
	if err := svc.recordStatusChange(context.Background(), 1, 0, "open", "closed", "go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.statusChanges[1]) != 1 {
		t.Fatalf("expected one recorded change, got %+v", repo.statusChanges)
	}
}
