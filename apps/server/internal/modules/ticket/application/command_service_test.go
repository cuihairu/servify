package application

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ticket/domain"
	"servify/apps/server/internal/platform/eventbus"
)

type stubCommandRepo struct {
	tickets        map[uint]*domain.Ticket
	comments       map[uint][]domain.Comment
	statusChanges  map[uint][]domain.StatusChange
	nextID         uint
	nextCommentID  uint
	nextChangeID   uint
	customerExists bool
	agentAvailable bool
	closeCalls     int
	err            error
}

type stubEventBus struct {
	events []eventbus.Event
	err    error
}

func (s *stubEventBus) Subscribe(eventName string, handler eventbus.Handler) {}

func (s *stubEventBus) Publish(ctx context.Context, event eventbus.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func (s *stubCommandRepo) CreateTicket(ctx context.Context, ticket *domain.Ticket) error {
	if s.err != nil {
		return s.err
	}
	if s.tickets == nil {
		s.tickets = map[uint]*domain.Ticket{}
	}
	if s.nextID == 0 {
		s.nextID = 1
	}
	ticket.ID = s.nextID
	s.nextID++
	cp := *ticket
	s.tickets[ticket.ID] = &cp
	return nil
}

func (s *stubCommandRepo) CreateTicketAtomically(ctx context.Context, ticket *domain.Ticket, initialStatus *domain.StatusChange, buildOutbox func(*domain.Ticket) *domain.OutboxEvent) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if ticket.IdempotencyKey != nil && *ticket.IdempotencyKey != "" {
		for _, existing := range s.tickets {
			if existing.IdempotencyKey != nil && *existing.IdempotencyKey == *ticket.IdempotencyKey {
				*ticket = *existing
				return false, nil
			}
		}
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		return false, err
	}
	if initialStatus != nil {
		change := *initialStatus
		if err := s.RecordStatusChange(ctx, ticket.ID, &change); err != nil {
			return false, err
		}
	}
	_ = buildOutbox(ticket)
	return true, nil
}

func (s *stubCommandRepo) UpdateTicket(ctx context.Context, ticket *domain.Ticket) error {
	if s.err != nil {
		return s.err
	}
	cp := *ticket
	s.tickets[ticket.ID] = &cp
	return nil
}

func (s *stubCommandRepo) UpdateTicketWithStatus(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error {
	if s.err != nil {
		return s.err
	}
	if err := s.RecordStatusChange(ctx, ticket.ID, &domain.StatusChange{
		UserID:     userID,
		FromStatus: fromStatus,
		ToStatus:   ticket.Status,
		Reason:     reason,
		CreatedAt:  ticket.UpdatedAt,
	}); err != nil {
		return err
	}
	cp := *ticket
	s.tickets[ticket.ID] = &cp
	return nil
}

func (s *stubCommandRepo) AssignTicket(ctx context.Context, ticket *domain.Ticket, previousAgentID *uint, fromStatus string, userID uint, reason string) error {
	if s.err != nil {
		return s.err
	}
	if err := s.RecordStatusChange(ctx, ticket.ID, &domain.StatusChange{
		UserID:     userID,
		FromStatus: fromStatus,
		ToStatus:   ticket.Status,
		Reason:     reason,
		CreatedAt:  ticket.UpdatedAt,
	}); err != nil {
		return err
	}
	cp := *ticket
	s.tickets[ticket.ID] = &cp
	return nil
}

func (s *stubCommandRepo) UnassignTicket(ctx context.Context, ticket *domain.Ticket, previousAgentID uint, fromStatus string, userID uint, reason string) error {
	if s.err != nil {
		return s.err
	}
	if err := s.RecordStatusChange(ctx, ticket.ID, &domain.StatusChange{
		UserID:     userID,
		FromStatus: fromStatus,
		ToStatus:   ticket.Status,
		Reason:     reason,
		CreatedAt:  ticket.UpdatedAt,
	}); err != nil {
		return err
	}
	cp := *ticket
	s.tickets[ticket.ID] = &cp
	return nil
}

func (s *stubCommandRepo) CloseTicket(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error {
	if s.err != nil {
		return s.err
	}
	s.closeCalls++
	if err := s.RecordStatusChange(ctx, ticket.ID, &domain.StatusChange{
		UserID:     userID,
		FromStatus: fromStatus,
		ToStatus:   "closed",
		Reason:     reason,
		CreatedAt:  time.Now(),
	}); err != nil {
		return err
	}
	cp := *ticket
	s.tickets[ticket.ID] = &cp
	return nil
}

func (s *stubCommandRepo) AddComment(ctx context.Context, ticketID uint, comment *domain.Comment) error {
	if s.err != nil {
		return s.err
	}
	if s.comments == nil {
		s.comments = map[uint][]domain.Comment{}
	}
	if s.nextCommentID == 0 {
		s.nextCommentID = 1
	}
	comment.ID = s.nextCommentID
	s.nextCommentID++
	s.comments[ticketID] = append(s.comments[ticketID], *comment)
	return nil
}

func (s *stubCommandRepo) RecordStatusChange(ctx context.Context, ticketID uint, change *domain.StatusChange) error {
	if s.err != nil {
		return s.err
	}
	if s.statusChanges == nil {
		s.statusChanges = map[uint][]domain.StatusChange{}
	}
	if s.nextChangeID == 0 {
		s.nextChangeID = 1
	}
	change.ID = s.nextChangeID
	s.nextChangeID++
	s.statusChanges[ticketID] = append(s.statusChanges[ticketID], *change)
	return nil
}

func (s *stubCommandRepo) GetTicket(ctx context.Context, ticketID uint) (*domain.Ticket, error) {
	if s.err != nil {
		return nil, s.err
	}
	ticket, ok := s.tickets[ticketID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *ticket
	return &cp, nil
}

func (s *stubCommandRepo) CustomerExists(ctx context.Context, customerID uint) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.customerExists, nil
}

func (s *stubCommandRepo) AgentAssignable(ctx context.Context, agentID uint) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.agentAvailable, nil
}

func TestCommandServiceCreateTicket(t *testing.T) {
	bus := &stubEventBus{}
	repo := &stubCommandRepo{customerExists: true}
	svc := NewCommandServiceWithBus(repo, bus)
	got, err := svc.CreateTicket(context.Background(), CreateTicketCommand{
		Title:      "Need Help",
		CustomerID: 7,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID == 0 || got.Status != "open" || got.Source != "web" {
		t.Fatalf("unexpected created ticket: %+v", got)
	}
	if len(repo.statusChanges[got.ID]) != 1 || repo.statusChanges[got.ID][0].ToStatus != "open" {
		t.Fatalf("expected initial open status history, got %+v", repo.statusChanges[got.ID])
	}
	// ticket.created 可靠事件由 outbox 统一投递，CreateTicket 不再直接发布。
	if len(bus.events) != 0 {
		t.Fatalf("expected no direct bus event (outbox owns delivery), got %+v", bus.events)
	}
}

func TestCommandServiceUpdateTicket(t *testing.T) {
	repo := &stubCommandRepo{
		customerExists: true,
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)
	newStatus := "resolved"
	newTitle := "Billing Updated"

	got, err := svc.UpdateTicket(context.Background(), 1, UpdateTicketCommand{
		Title:   &newTitle,
		Status:  &newStatus,
		ActorID: 99,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Title != "Billing Updated" || got.Status != "resolved" || got.ResolvedAt == nil {
		t.Fatalf("unexpected updated ticket: %+v", got)
	}
	if len(repo.statusChanges[1]) != 1 || repo.statusChanges[1][0].ToStatus != "resolved" {
		t.Fatalf("expected status history to be recorded, got %+v", repo.statusChanges[1])
	}
}

func TestCommandServiceUpdateTicketPublishesAssignedEventOnAgentChange(t *testing.T) {
	bus := &stubEventBus{}
	agentID := uint(8)
	repo := &stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandServiceWithBus(repo, bus)
	if _, err := svc.UpdateTicket(context.Background(), 1, UpdateTicketCommand{AgentID: &agentID, ActorID: 1}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(bus.events) != 1 || bus.events[0].Name() != TicketAssignedEventName {
		t.Fatalf("expected assigned event, got %+v", bus.events)
	}
}

func TestCommandServiceAssignTicket(t *testing.T) {
	repo := &stubCommandRepo{
		agentAvailable: true,
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)
	got, err := svc.AssignTicket(context.Background(), 1, AssignTicketCommand{AgentID: 88})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.AgentID == nil || *got.AgentID != 88 || got.Status != "assigned" {
		t.Fatalf("unexpected assigned ticket: %+v", got)
	}
	if len(repo.statusChanges[1]) != 1 || repo.statusChanges[1][0].ToStatus != "assigned" {
		t.Fatalf("expected assignment status history, got %+v", repo.statusChanges[1])
	}
}

func TestCommandServiceUnassignTicket(t *testing.T) {
	repo := &stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "assigned",
				AgentID:    uintPtr(88),
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)
	got, err := svc.UnassignTicket(context.Background(), 1, UnassignTicketCommand{UserID: 5})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.AgentID != nil || got.Status != "open" {
		t.Fatalf("unexpected unassigned ticket: %+v", got)
	}
	if len(repo.statusChanges[1]) != 1 || repo.statusChanges[1][0].ToStatus != "open" {
		t.Fatalf("expected unassign status history, got %+v", repo.statusChanges[1])
	}
}

func TestCommandServiceAddComment(t *testing.T) {
	repo := &stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)
	got, err := svc.AddComment(context.Background(), 1, AddCommentCommand{
		UserID:  7,
		Content: " Need more logs ",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID == 0 || got.Content != "Need more logs" || got.Type != "comment" {
		t.Fatalf("unexpected comment: %+v", got)
	}
	if len(repo.comments[1]) != 1 {
		t.Fatalf("expected comment to be persisted, got %+v", repo.comments[1])
	}
}

func TestCommandServiceCloseTicket(t *testing.T) {
	bus := &stubEventBus{}
	repo := &stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandServiceWithBus(repo, bus)
	got, err := svc.CloseTicket(context.Background(), 1, CloseTicketCommand{UserID: 8, Reason: "done"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != "closed" || got.ClosedAt == nil {
		t.Fatalf("unexpected closed ticket: %+v", got)
	}
	if repo.closeCalls != 1 {
		t.Fatalf("expected close repository path to be used, got %d", repo.closeCalls)
	}
	if len(repo.statusChanges[1]) != 1 || repo.statusChanges[1][0].ToStatus != "closed" {
		t.Fatalf("expected close status history, got %+v", repo.statusChanges[1])
	}
	if len(bus.events) != 1 || bus.events[0].Name() != TicketClosedEventName {
		t.Fatalf("expected closed event, got %+v", bus.events)
	}
}

func TestStatusTransitionPolicyRejectsInvalidTransition(t *testing.T) {
	repo := &stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "resolved",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)
	nextStatus := "assigned"

	_, err := svc.UpdateTicket(context.Background(), 1, UpdateTicketCommand{
		Status:  &nextStatus,
		ActorID: 42,
	})
	if err == nil {
		t.Fatal("expected invalid transition error")
	}
}

func TestCommandServiceBulkUpdateTickets(t *testing.T) {
	repo := &stubCommandRepo{
		agentAvailable: true,
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				Tags:       "vip",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
			2: {
				ID:         2,
				Title:      "Support",
				CustomerID: 10,
				Status:     "assigned",
				Tags:       "old",
				AgentID:    uintPtr(88),
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)
	resolved := "resolved"
	agentID := uint(7)

	got, err := svc.BulkUpdateTickets(context.Background(), BulkUpdateTicketsCommand{
		TicketIDs:  []uint{2, 1, 2, 0},
		Status:     &resolved,
		AddTags:    []string{"urgent"},
		RemoveTags: []string{"old"},
		AgentID:    &agentID,
		UserID:     99,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got.Failed) != 0 || len(got.Updated) != 2 {
		t.Fatalf("unexpected bulk result: %+v", got)
	}
	if repo.tickets[1].Status != "resolved" || repo.tickets[2].Status != "resolved" {
		t.Fatalf("expected tickets to be resolved: %+v %+v", repo.tickets[1], repo.tickets[2])
	}
	if repo.tickets[1].AgentID == nil || *repo.tickets[1].AgentID != agentID {
		t.Fatalf("expected ticket 1 to be assigned: %+v", repo.tickets[1])
	}
	if repo.tickets[2].Tags != "urgent" || repo.tickets[1].Tags != "urgent,vip" {
		t.Fatalf("unexpected tag merge: ticket1=%q ticket2=%q", repo.tickets[1].Tags, repo.tickets[2].Tags)
	}
}

func TestCommandServiceBulkUpdateTicketsWithUnassign(t *testing.T) {
	repo := &stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "assigned",
				AgentID:    uintPtr(88),
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandService(repo)

	got, err := svc.BulkUpdateTickets(context.Background(), BulkUpdateTicketsCommand{
		TicketIDs:     []uint{1},
		UnassignAgent: true,
		UserID:        5,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got.Failed) != 0 || len(got.Updated) != 1 {
		t.Fatalf("unexpected bulk result: %+v", got)
	}
	if repo.tickets[1].AgentID != nil || repo.tickets[1].Status != "open" {
		t.Fatalf("expected ticket to be unassigned and reopened: %+v", repo.tickets[1])
	}
}

func uintPtr(v uint) *uint {
	return &v
}

func TestCommandServiceAssignTicketPublishesEvent(t *testing.T) {
	bus := &stubEventBus{}
	repo := &stubCommandRepo{
		agentAvailable: true,
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "open",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandServiceWithBus(repo, bus)
	if _, err := svc.AssignTicket(context.Background(), 1, AssignTicketCommand{AgentID: 88, UserID: 2}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(bus.events) != 1 || bus.events[0].Name() != TicketAssignedEventName {
		t.Fatalf("expected assigned event, got %+v", bus.events)
	}
}

// TestCommandServiceReopenResolvedTicket 重开政策（todo 195）：客户追问已
// 解决工单时坐席重开同一工单——resolved → open 清空解决时间戳、状态流水
// 记 reopen_ticket（区别于普通 update_ticket）、发布 ticket.reopened 供
// timeline 投影「重开工单」归入来源会话。
func TestCommandServiceReopenResolvedTicket(t *testing.T) {
	bus := &stubEventBus{}
	resolvedAt := time.Now().Add(-time.Hour)
	repo := &stubCommandRepo{
		customerExists: true,
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "resolved",
				ResolvedAt: &resolvedAt,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandServiceWithBus(repo, bus)
	status := "open"
	got, err := svc.UpdateTicket(context.Background(), 1, UpdateTicketCommand{Status: &status, ActorID: 42})
	if err != nil {
		t.Fatalf("expected reopen to succeed, got %v", err)
	}
	if got.Status != "open" {
		t.Fatalf("expected reopened status open, got %+v", got)
	}
	if got.ResolvedAt != nil {
		t.Fatalf("expected resolved_at cleared on reopen, got %+v", got.ResolvedAt)
	}
	changes := repo.statusChanges[1]
	if len(changes) == 0 {
		t.Fatalf("expected reopen status history, got none")
	}
	last := changes[len(changes)-1]
	if last.Reason != "reopen_ticket" || last.FromStatus != "resolved" || last.ToStatus != "open" || last.UserID != 42 {
		t.Fatalf("expected reopen_ticket history entry, got %+v", last)
	}
	reopened := false
	for _, ev := range bus.events {
		if ev.Name() == TicketReopenedEventName {
			reopened = true
		}
	}
	if !reopened {
		t.Fatalf("expected ticket.reopened published, got %+v", bus.events)
	}
}

// TestCommandServiceReopenClosedRejected 重开政策的终态半边：closed → open
// 被状态机拒绝且错误信息指引新建工单，不产生任何写入或事件。
func TestCommandServiceReopenClosedRejected(t *testing.T) {
	bus := &stubEventBus{}
	closedAt := time.Now().Add(-2 * time.Hour)
	repo := &stubCommandRepo{
		customerExists: true,
		tickets: map[uint]*domain.Ticket{
			1: {
				ID:         1,
				Title:      "Billing",
				CustomerID: 9,
				Status:     "closed",
				ClosedAt:   &closedAt,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}
	svc := NewCommandServiceWithBus(repo, bus)
	status := "open"
	_, err := svc.UpdateTicket(context.Background(), 1, UpdateTicketCommand{Status: &status, ActorID: 42})
	if err == nil || !strings.Contains(err.Error(), "closed is terminal") {
		t.Fatalf("expected closed-terminal rejection, got %v", err)
	}
	if len(bus.events) != 0 || len(repo.statusChanges[1]) != 0 {
		t.Fatalf("expected no writes or events on rejected reopen, events=%+v changes=%+v", bus.events, repo.statusChanges[1])
	}
}
