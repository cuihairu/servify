package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ticket/domain"
)

// closeErrCommandRepo 让 CloseTicket 单独失败（GetTicket 不受影响），
// 用于覆盖 CloseTicket 中 repo 层错误分支。
type closeErrCommandRepo struct {
	stubCommandRepo
	closeErr error
}

func (s *closeErrCommandRepo) CloseTicket(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error {
	if s.closeErr != nil {
		return s.closeErr
	}
	return s.stubCommandRepo.CloseTicket(ctx, ticket, fromStatus, userID, reason)
}

func TestCoverageTailUpdateTicketScalarFields(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	repo := &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {ID: 1, Title: "T", Description: "old", Category: "old", Priority: "low", CustomerID: 2, Status: "open", CreatedAt: now, UpdatedAt: now},
		},
	}}

	desc := "  new desc  "
	category := " hardware "
	priority := " HIGH "
	dto, err := NewCommandService(repo).UpdateTicket(ctx, 1, UpdateTicketCommand{
		Description: &desc,
		Category:    &category,
		Priority:    &priority,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Category != "hardware" || dto.Priority != "HIGH" {
		t.Fatalf("expected trimmed scalar fields, got %+v", dto)
	}
	stored := repo.tickets[1]
	if stored.Description != "new desc" || stored.Category != "hardware" || stored.Priority != "HIGH" {
		t.Fatalf("expected stored ticket updated, got %+v", stored)
	}
}

func TestCoverageTailUnassignTicketInvalidTransition(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	// 空状态 + 已指派：unassign 回退目标为 open，而空状态 -> open 不是合法迁移。
	agent := uint(4)
	repo := &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {ID: 1, Title: "T", CustomerID: 2, Status: "", AgentID: &agent, CreatedAt: now, UpdatedAt: now},
		},
	}}
	_, err := NewCommandService(repo).UnassignTicket(ctx, 1, UnassignTicketCommand{})
	if err == nil || err.Error() != "invalid status transition:  -> open" {
		t.Fatalf("expected invalid status transition error, got %v", err)
	}
}

func TestCoverageTailCloseTicketBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	newRepo := func(status string) *flakyCommandRepo {
		return &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T", CustomerID: 2, Status: status, CreatedAt: now, UpdatedAt: now},
			},
		}}
	}

	// 非法当前状态 -> statusPolicy 拒绝关闭
	repo := newRepo("pending")
	if _, err := NewCommandService(repo).CloseTicket(ctx, 1, CloseTicketCommand{}); err == nil || err.Error() != "invalid status transition: pending -> closed" {
		t.Fatalf("expected invalid transition error, got %v", err)
	}

	// GetTicket 成功但 CloseTicket 落库失败
	closeRepo := &closeErrCommandRepo{
		stubCommandRepo: stubCommandRepo{
			tickets: map[uint]*domain.Ticket{
				1: {ID: 1, Title: "T", CustomerID: 2, Status: "open", CreatedAt: now, UpdatedAt: now},
			},
		},
		closeErr: errors.New("close write failed"),
	}
	if _, err := NewCommandService(closeRepo).CloseTicket(ctx, 1, CloseTicketCommand{}); err == nil || err.Error() != "close write failed" {
		t.Fatalf("expected close write error, got %v", err)
	}
}

func TestCoverageTailBulkUpdateErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	agent := uint(3)

	// 指派工单上批量取消指派失败
	repo := &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			2: {ID: 2, Title: "T2", CustomerID: 2, Status: "assigned", AgentID: &agent, CreatedAt: now, UpdatedAt: now},
		},
	}}
	repo.unassignErr = errors.New("unassign blew up")
	result, err := NewCommandService(repo).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{2}, UnassignAgent: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].TicketID != 2 || result.Failed[0].Error != "unassign blew up" {
		t.Fatalf("expected unassign failure recorded, got %+v", result.Failed)
	}

	// 指派成功后重新拉取工单失败（第 3 次 GetTicket）
	repo = &flakyCommandRepo{stubCommandRepo: stubCommandRepo{
		tickets: map[uint]*domain.Ticket{
			1: {ID: 1, Title: "T1", CustomerID: 2, Status: "open", CreatedAt: now, UpdatedAt: now},
		},
		customerExists: true,
		agentAvailable: true,
	}}
	repo.getErrAt = map[int]error{3: errors.New("refetch blew up")}
	result, err = NewCommandService(repo).BulkUpdateTickets(ctx, BulkUpdateTicketsCommand{TicketIDs: []uint{1}, AgentID: &agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].TicketID != 1 || result.Failed[0].Error != "refetch blew up" {
		t.Fatalf("expected refetch failure recorded, got %+v", result.Failed)
	}
}

func TestCoverageTailValidatorSkipsInactiveOrOptionalRequired(t *testing.T) {
	validator := NewCustomFieldValidator()
	ctx := map[string]interface{}{"ticket.priority": "high"}

	// enforceRequired 开启时，非必填与未启用定义都应被跳过而不报错
	values, err := validator.Validate([]CustomFieldDefinition{
		{ID: 1, Key: "opt", Type: "string", Active: true, Required: false},
		{ID: 2, Key: "off", Type: "string", Active: false, Required: true},
	}, nil, ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values != nil {
		t.Fatalf("expected nil values, got %+v", values)
	}
}
