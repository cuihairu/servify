package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/modules/routing/domain"
	"servify/apps/server/internal/platform/eventbus"
)

func uintPtr(v uint) *uint {
	return &v
}

type stubRoutingRepo struct {
	assignments map[string]*domain.Assignment
	transferLog []domain.TransferRecord
	queue       map[string]*domain.QueueEntry
	err         error
}

func (s *stubRoutingRepo) CreateAssignment(ctx context.Context, assignment *domain.Assignment) error {
	if s.err != nil {
		return s.err
	}
	if s.assignments == nil {
		s.assignments = map[string]*domain.Assignment{}
	}
	cp := *assignment
	s.assignments[assignment.SessionID] = &cp
	s.transferLog = append(s.transferLog, domain.TransferRecord{
		SessionID:      assignment.SessionID,
		FromAgentID:    assignment.FromAgentID,
		ToAgentID:      uintPtr(assignment.ToAgentID),
		Reason:         assignment.Reason,
		Notes:          assignment.Notes,
		SessionSummary: assignment.SessionSummary,
		TransferredAt:  assignment.AssignedAt,
	})
	return nil
}

func (s *stubRoutingRepo) ListAssignments(ctx context.Context, sessionID string) ([]domain.TransferRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]domain.TransferRecord, 0, len(s.transferLog))
	for _, item := range s.transferLog {
		if item.SessionID == sessionID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *stubRoutingRepo) ListRecentAssignments(ctx context.Context, limit int) ([]domain.TransferRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]domain.TransferRecord, 0, len(s.transferLog))
	for _, item := range s.transferLog {
		out = append(out, item)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *stubRoutingRepo) CreateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error {
	if s.err != nil {
		return s.err
	}
	if s.queue == nil {
		s.queue = map[string]*domain.QueueEntry{}
	}
	cp := *entry
	s.queue[entry.SessionID] = &cp
	return nil
}

func (s *stubRoutingRepo) GetQueueEntry(ctx context.Context, sessionID string) (*domain.QueueEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	item, ok := s.queue[sessionID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *item
	return &cp, nil
}

func (s *stubRoutingRepo) ListQueueEntries(ctx context.Context, status string, limit int) ([]domain.QueueEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]domain.QueueEntry, 0, len(s.queue))
	for _, item := range s.queue {
		if item.Status != domain.QueueStatus(status) {
			continue
		}
		cp := *item
		out = append(out, cp)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *stubRoutingRepo) UpdateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error {
	if s.err != nil {
		return s.err
	}
	cp := *entry
	s.queue[entry.SessionID] = &cp
	return nil
}

func (s *stubRoutingRepo) MarkQueueEntryTransferred(ctx context.Context, sessionID string, agentID uint, assignedAt time.Time) (*domain.QueueEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	item, ok := s.queue[sessionID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *item
	cp.Status = domain.QueueStatusTransferred
	cp.AssignedAt = &assignedAt
	cp.AssignedTo = &agentID
	s.queue[sessionID] = &cp
	return &cp, nil
}

type stubRoutingPublisher struct {
	events []eventbus.Event
}

func (s *stubRoutingRepo) ClaimQueueEntries(ctx context.Context, now time.Time, leaseBefore time.Time, limit int) ([]domain.QueueEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s *stubRoutingRepo) ReleaseQueueClaim(ctx context.Context, sessionID string) error {
	return s.err
}

func (s *stubRoutingPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestServiceAssignAgentPublishesEvents(t *testing.T) {
	repo := &stubRoutingRepo{}
	pub := &stubRoutingPublisher{}
	svc := NewService(repo, pub)
	now := time.Now()
	svc.now = func() time.Time { return now }

	fromAgentID := uint(1)
	got, err := svc.AssignAgent(context.Background(), AssignAgentCommand{
		SessionID:      "sess-1",
		AgentID:        9,
		FromAgentID:    &fromAgentID,
		Reason:         "handoff",
		SessionSummary: "summary",
		AssignedAt:     now,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ToAgentID != 9 || got.FromAgentID == nil || *got.FromAgentID != 1 || got.SessionSummary != "summary" || !got.AssignedAt.Equal(now) {
		t.Fatalf("unexpected assignment dto: %+v", got)
	}
	if len(pub.events) != 2 || pub.events[0].Name() != RoutingAgentAssignedEventName || pub.events[1].Name() != RoutingTransferCompletedEventName {
		t.Fatalf("unexpected published events: %+v", pub.events)
	}
}

func TestServiceAddToWaitingQueueAndCancel(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)
	now := time.Now()
	svc.now = func() time.Time { return now }

	entry, err := svc.AddToWaitingQueue(context.Background(), AddToWaitingQueueCommand{
		SessionID:    "sess-1",
		Reason:       "no_agent",
		TargetSkills: []string{"billing"},
		Priority:     "high",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if entry.Status != string(domain.QueueStatusWaiting) || len(entry.TargetSkills) != 1 {
		t.Fatalf("unexpected queue entry: %+v", entry)
	}

	cancelled, err := svc.CancelWaiting(context.Background(), CancelWaitingCommand{
		SessionID: "sess-1",
		Reason:    "user_left",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cancelled.Status != string(domain.QueueStatusCancelled) || cancelled.Notes != "user_left" {
		t.Fatalf("unexpected cancelled entry: %+v", cancelled)
	}
}

func TestServiceRequestHumanHandoffDelegatesToWaitingQueue(t *testing.T) {
	repo := &stubRoutingRepo{}
	svc := NewService(repo, nil)

	got, err := svc.RequestHumanHandoff(context.Background(), RequestHumanHandoffCommand{
		SessionID: "sess-2",
		Reason:    "manual",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.SessionID != "sess-2" || got.Status != string(domain.QueueStatusWaiting) {
		t.Fatalf("unexpected handoff result: %+v", got)
	}
}

func TestServiceListWaitingEntries(t *testing.T) {
	repo := &stubRoutingRepo{
		queue: map[string]*domain.QueueEntry{
			"sess-1": {SessionID: "sess-1", Status: domain.QueueStatusWaiting},
			"sess-2": {SessionID: "sess-2", Status: domain.QueueStatusCancelled},
		},
	}
	svc := NewService(repo, nil)

	got, err := svc.ListWaitingEntries(context.Background(), "waiting", 10)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "sess-1" {
		t.Fatalf("unexpected waiting entries: %+v", got)
	}
}

func TestServiceMarkWaitingTransferred(t *testing.T) {
	repo := &stubRoutingRepo{
		queue: map[string]*domain.QueueEntry{
			"sess-1": {SessionID: "sess-1", Status: domain.QueueStatusWaiting},
		},
	}
	svc := NewService(repo, nil)
	now := time.Now()
	svc.now = func() time.Time { return now }

	got, err := svc.MarkWaitingTransferred(context.Background(), MarkWaitingTransferredCommand{
		SessionID:  "sess-1",
		AssignedTo: 9,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != string(domain.QueueStatusTransferred) || got.AssignedTo == nil || *got.AssignedTo != 9 {
		t.Fatalf("unexpected transferred entry: %+v", got)
	}
}

func TestServiceGetTransferHistory(t *testing.T) {
	repo := &stubRoutingRepo{
		transferLog: []domain.TransferRecord{
			{SessionID: "sess-1", Reason: "handoff"},
			{SessionID: "sess-2", Reason: "other"},
		},
	}
	svc := NewService(repo, nil)

	got, err := svc.GetTransferHistory(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "sess-1" || got[0].Reason != "handoff" {
		t.Fatalf("unexpected transfer history: %+v", got)
	}
}

func TestServiceListRecentTransferHistory(t *testing.T) {
	repo := &stubRoutingRepo{
		transferLog: []domain.TransferRecord{
			{SessionID: "sess-1", Reason: "handoff"},
			{SessionID: "sess-2", Reason: "other"},
		},
	}
	svc := NewService(repo, nil)

	got, err := svc.ListRecentTransferHistory(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "sess-1" {
		t.Fatalf("unexpected recent transfer history: %+v", got)
	}
}
