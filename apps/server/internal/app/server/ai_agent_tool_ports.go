package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

type scopedAgentToolPorts struct {
	db      *gorm.DB
	session string
	handoff routingdelivery.RuntimeService
}

func newScopedAgentToolPorts(db *gorm.DB, routing routingdelivery.RuntimeService) aidelivery.AgentToolPorts {
	if db == nil || routing == nil {
		return aidelivery.AgentToolPorts{}
	}
	ports := scopedAgentToolPorts{db: db, handoff: routing}
	return aidelivery.AgentToolPorts{
		CustomerLookup: ports,
		TicketLookup:   ports,
		Handoff:        ports,
	}
}

func (p scopedAgentToolPorts) withSessionScope(ctx context.Context, lookup func(context.Context) (map[string]interface{}, error)) (map[string]interface{}, error) {
	toolCtx, err := p.toolContext(ctx)
	if err != nil {
		return nil, err
	}
	return lookup(toolCtx)
}

func (p scopedAgentToolPorts) GetCustomerSummary(ctx context.Context, customerID uint) (map[string]interface{}, error) {
	return p.withSessionScope(ctx, func(toolCtx context.Context) (map[string]interface{}, error) {
		var customer models.Customer
		if err := p.db.WithContext(toolCtx).First(&customer, "id = ?", customerID).Error; err != nil {
			return nil, fmt.Errorf("load customer: %w", err)
		}
		return summarizeCustomer(customer), nil
	})
}

func (p scopedAgentToolPorts) toolContext(ctx context.Context) (context.Context, error) {
	tenantID := strings.TrimSpace(platformauth.TenantIDFromContext(ctx))
	workspaceID := strings.TrimSpace(platformauth.WorkspaceIDFromContext(ctx))
	var session models.Session
	if err := p.db.WithContext(ctx).Select("tenant_id", "workspace_id", "user_id", "ticket_id").
		First(&session, "id = ?", p.session).Error; err != nil {
		return nil, fmt.Errorf("load agent session scope: %w", err)
	}
	if session.TenantID == "" || session.WorkspaceID == "" {
		return nil, errors.New("agent session scope is incomplete")
	}
	if tenantID != "" && tenantID != session.TenantID {
		return nil, errors.New("agent session tenant scope mismatch")
	}
	if workspaceID != "" && workspaceID != session.WorkspaceID {
		return nil, errors.New("agent session workspace scope mismatch")
	}
	return platformauth.ContextWithScope(ctx, session.TenantID, session.WorkspaceID), nil
}

func summarizeCustomer(customer models.Customer) map[string]interface{} {
	return map[string]interface{}{
		"id":       customer.ID,
		"company":  customer.Company,
		"industry": customer.Industry,
		"source":   customer.Source,
		"tags":     customer.Tags,
		"priority": customer.Priority,
		"notes":    truncateAgentSummary(customer.Notes, 1000),
	}
}

func (p scopedAgentToolPorts) GetTicketSummary(ctx context.Context, ticketID uint) (map[string]interface{}, error) {
	return p.withSessionScope(ctx, func(toolCtx context.Context) (map[string]interface{}, error) {
		var ticket models.Ticket
		if err := p.db.WithContext(toolCtx).First(&ticket, "id = ?", ticketID).Error; err != nil {
			return nil, fmt.Errorf("load ticket: %w", err)
		}
		return summarizeTicket(ticket), nil
	})
}

func (p scopedAgentToolPorts) RequestHandoff(ctx context.Context, conversationID string, reason string) (map[string]interface{}, error) {
	if conversationID != p.session {
		return nil, errors.New("agent handoff conversation mismatch")
	}
	toolCtx, err := p.toolContext(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := p.handoff.GetWaitingRecord(toolCtx, conversationID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("check handoff queue: %w", err)
	}
	if existing != nil && existing.Status == "waiting" {
		return handoffQueuedResult(existing.SessionID, existing.Status, existing.QueuedAt), nil
	}
	entry, err := p.handoff.AddToWaitingQueue(toolCtx, nil, conversationID, strings.TrimSpace(reason), nil, 0, "", "")
	if err != nil {
		return nil, fmt.Errorf("request handoff: %w", err)
	}
	return handoffQueuedResult(entry.SessionID, entry.Status, entry.QueuedAt), nil
}

func handoffQueuedResult(sessionID string, status string, queuedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"status":     status,
		"queued_at":  queuedAt.UTC().Format(time.RFC3339),
		"queued":     true,
	}
}

func summarizeTicket(ticket models.Ticket) map[string]interface{} {
	summary := map[string]interface{}{
		"id":          ticket.ID,
		"title":       ticket.Title,
		"description": truncateAgentSummary(ticket.Description, 1000),
		"ai_summary":  truncateAgentSummary(ticket.AISummary, 1000),
		"customer_id": ticket.CustomerID,
		"agent_id":    ticket.AgentID,
		"session_id":  ticket.SessionID,
		"category":    ticket.Category,
		"priority":    ticket.Priority,
		"status":      ticket.Status,
		"source":      ticket.Source,
		"tags":        ticket.Tags,
		"due_date":    ticket.DueDate,
		"resolved_at": ticket.ResolvedAt,
		"closed_at":   ticket.ClosedAt,
	}
	if !ticket.CreatedAt.IsZero() {
		summary["created_at"] = ticket.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !ticket.UpdatedAt.IsZero() {
		summary["updated_at"] = ticket.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return summary
}

func truncateAgentSummary(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
