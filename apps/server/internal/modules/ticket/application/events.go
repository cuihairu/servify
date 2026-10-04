package application

import (
	"fmt"
	"time"

	"servify/apps/server/internal/platform/eventbus"
)

const (
	TicketCreatedEventName  = "ticket.created"
	TicketAssignedEventName = "ticket.assigned"
	TicketClosedEventName   = "ticket.closed"
)

type TicketEvent struct {
	eventbus.BaseEvent
	TicketID   uint
	CustomerID uint
	AgentID    *uint
	// SessionID 是工单来源会话 ID；timeline 投影（V1.0 B1-2）借它把
	// ticket.* 事件归入 conversation_events，无来源会话（如邮件建单）
	// 时为 nil 且不投影。
	SessionID *string
	Status    string
	Priority  string
	Category  string
	Source    string
}

func NewTicketEvent(name string, ticket TicketDTO) TicketEvent {
	return TicketEvent{
		BaseEvent: eventbus.BaseEvent{
			EventID:          fmt.Sprintf("%s-%d-%d", name, ticket.ID, time.Now().UnixNano()),
			EventName:        name,
			EventOccurredAt:  time.Now(),
			EventAggregateID: fmt.Sprintf("ticket:%d", ticket.ID),
		},
		TicketID:   ticket.ID,
		CustomerID: ticket.CustomerID,
		AgentID:    ticket.AgentID,
		SessionID:  ticket.SessionID,
		Status:     ticket.Status,
		Priority:   ticket.Priority,
		Category:   ticket.Category,
		Source:     ticket.Source,
	}
}
