package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ticket/domain"
)

type stubQueryRepo struct {
	details *domain.TicketDetails
	items   []domain.Ticket
	total   int64
	stats   *TicketStatsDTO
	err     error
}

func (s stubQueryRepo) GetTicketByID(ctx context.Context, ticketID uint) (*domain.TicketDetails, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.details == nil {
		return nil, fmt.Errorf("not found")
	}
	return s.details, nil
}

func (s stubQueryRepo) ListTickets(ctx context.Context, query ListTicketsQuery) ([]domain.Ticket, int64, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.items, s.total, nil
}

func (s stubQueryRepo) GetTicketStats(ctx context.Context, agentID *uint) (*TicketStatsDTO, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.stats == nil {
		return &TicketStatsDTO{}, nil
	}
	return s.stats, nil
}

func TestQueryServiceGetTicketByID(t *testing.T) {
	now := time.Now()
	svc := NewQueryService(stubQueryRepo{
		details: &domain.TicketDetails{
			Ticket: domain.Ticket{
				ID:          1,
				Title:       "Billing issue",
				Description: "Need help",
				CustomerID:  10,
				Status:      "open",
				Priority:    "high",
				Category:    "billing",
				Source:      "web",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	})

	got, err := svc.GetTicketByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != 1 || got.Title != "Billing issue" {
		t.Fatalf("unexpected ticket details: %+v", got)
	}
}

func TestQueryServiceListTickets(t *testing.T) {
	now := time.Now()
	svc := NewQueryService(stubQueryRepo{
		items: []domain.Ticket{
			{
				ID:         1,
				Title:      "Billing issue",
				CustomerID: 10,
				Status:     "open",
				Priority:   "high",
				Category:   "billing",
				Source:     "web",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		},
		total: 1,
	})

	got, err := svc.ListTickets(context.Background(), ListTicketsQuery{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("unexpected list result: %+v", got)
	}
}

func TestQueryServiceGetTicketStats(t *testing.T) {
	svc := NewQueryService(stubQueryRepo{
		stats: &TicketStatsDTO{
			Total:        3,
			TodayCreated: 1,
			Pending:      2,
			Resolved:     1,
			ByStatus:     []StatusCountDTO{{Status: "open", Count: 2}},
			ByPriority:   []PriorityCountDTO{{Priority: "high", Count: 1}},
		},
	})

	got, err := svc.GetTicketStats(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Total != 3 || got.Pending != 2 || len(got.ByStatus) != 1 || len(got.ByPriority) != 1 {
		t.Fatalf("unexpected stats result: %+v", got)
	}
}

// recordingQueryRepo 记录 ListTickets 收到的过滤条件，供拦截查询断言透传口径。
type recordingQueryRepo struct {
	stubQueryRepo
	gotQuery ListTicketsQuery
}

func (r *recordingQueryRepo) ListTickets(ctx context.Context, query ListTicketsQuery) ([]domain.Ticket, int64, error) {
	r.gotQuery = query
	return r.items, r.total, nil
}

func TestQueryServiceCountOpenBySession(t *testing.T) {
	repo := &recordingQueryRepo{
		stubQueryRepo: stubQueryRepo{
			items: []domain.Ticket{
				{ID: 7, Status: "open", SessionID: strPtr("conv-1")},
				{ID: 9, Status: "in_progress", SessionID: strPtr("conv-1")},
			},
			total: 2,
		},
	}
	svc := NewQueryService(repo)

	total, ids, err := svc.CountOpenBySession(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if total != 2 || len(ids) != 2 || ids[0] != 7 || ids[1] != 9 {
		t.Fatalf("unexpected open tickets: total=%d ids=%v", total, ids)
	}

	// 透传口径：未完结三态 + 会话过滤 + 取样上限。
	if repo.gotQuery.SessionID == nil || *repo.gotQuery.SessionID != "conv-1" {
		t.Fatalf("session filter not passed through: %+v", repo.gotQuery)
	}
	if len(repo.gotQuery.Status) != 3 {
		t.Fatalf("expected open/assigned/in_progress statuses, got %v", repo.gotQuery.Status)
	}
	if repo.gotQuery.PageSize != openTicketScanLimit {
		t.Fatalf("expected scan limit %d, got %d", openTicketScanLimit, repo.gotQuery.PageSize)
	}
}

func TestQueryServiceCountOpenBySessionRejectsBlankSession(t *testing.T) {
	svc := NewQueryService(stubQueryRepo{})

	for _, sessionID := range []string{"", "   "} {
		if _, _, err := svc.CountOpenBySession(context.Background(), sessionID); err == nil {
			t.Fatalf("expected error for blank session id %q", sessionID)
		}
	}
}
