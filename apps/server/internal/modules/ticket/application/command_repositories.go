package application

import (
	"context"

	"servify/apps/server/internal/modules/ticket/domain"
)

type CommandRepository interface {
	CreateTicket(ctx context.Context, ticket *domain.Ticket) error
	// CreateTicketAtomically writes ticket + initial status record + outbox
	// event in one DB transaction. It is idempotent on ticket.IdempotencyKey:
	// a retry with the same key returns the already-created ticket without
	// writing a duplicate row or duplicate outbox event. buildOutbox is called
	// inside the transaction after the ticket row has its ID, so the outbox
	// payload can embed the assigned ID; it may return nil to skip the outbox.
	CreateTicketAtomically(ctx context.Context, ticket *domain.Ticket, initialStatus *domain.StatusChange, buildOutbox func(*domain.Ticket) *domain.OutboxEvent) (created bool, err error)
	UpdateTicket(ctx context.Context, ticket *domain.Ticket) error
	UpdateTicketWithStatus(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error
	AssignTicket(ctx context.Context, ticket *domain.Ticket, previousAgentID *uint, fromStatus string, userID uint, reason string) error
	UnassignTicket(ctx context.Context, ticket *domain.Ticket, previousAgentID uint, fromStatus string, userID uint, reason string) error
	CloseTicket(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error
	AddComment(ctx context.Context, ticketID uint, comment *domain.Comment) error
	RecordStatusChange(ctx context.Context, ticketID uint, change *domain.StatusChange) error
	GetTicket(ctx context.Context, ticketID uint) (*domain.Ticket, error)
	CustomerExists(ctx context.Context, customerID uint) (bool, error)
	AgentAssignable(ctx context.Context, agentID uint) (bool, error)
}
