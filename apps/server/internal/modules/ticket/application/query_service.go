package application

import (
	"context"
	"fmt"
	"strings"
)

type QueryService struct {
	repo QueryRepository
}

func NewQueryService(repo QueryRepository) *QueryService {
	return &QueryService{repo: repo}
}

func (s *QueryService) GetTicketByID(ctx context.Context, ticketID uint) (*TicketDetailsDTO, error) {
	if ticketID == 0 {
		return nil, fmt.Errorf("ticket id required")
	}
	details, err := s.repo.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	return MapTicketDetails(details), nil
}

func (s *QueryService) ListTickets(ctx context.Context, query ListTicketsQuery) (*ListTicketsResultDTO, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	items, total, err := s.repo.ListTickets(ctx, query)
	if err != nil {
		return nil, err
	}
	return &ListTicketsResultDTO{
		Items: MapTickets(items),
		Total: total,
	}, nil
}

func (s *QueryService) GetTicketStats(ctx context.Context, agentID *uint) (*TicketStatsDTO, error) {
	return s.repo.GetTicketStats(ctx, agentID)
}

// openTicketScanLimit 是单次拦截查询的取样上限：拦截语义只需要存在性与提示，
// 单会话未结单超过该数时以总数呈现（不逐页取尽）。
const openTicketScanLimit = 100

// openTicketStatuses 是「未完结」的口径：尚未走到 resolved/closed 终态。
var openTicketStatuses = []string{"open", "assigned", "in_progress"}

// CountOpenBySession 返回某会话下未完结工单的数量与 ID 列表——
// Conversation 关闭前拦截（v1-convergence-plan §7.1）的数据面：
// 会话关闭时若仍有未完结工单，须先建单收尾或明确降级。
// ID 列表用于拦截提示（上限 openTicketScanLimit 张，超出以截断计数呈现）。
func (s *QueryService) CountOpenBySession(ctx context.Context, sessionID string) (int64, []uint, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, nil, fmt.Errorf("session id required")
	}
	items, total, err := s.repo.ListTickets(ctx, ListTicketsQuery{
		Page:     1,
		PageSize: openTicketScanLimit,
		Status:   openTicketStatuses,
		SessionID: &sessionID,
	})
	if err != nil {
		return 0, nil, err
	}
	ids := make([]uint, 0, len(items))
	for _, ticket := range items {
		ids = append(ids, ticket.ID)
	}
	return total, ids, nil
}
