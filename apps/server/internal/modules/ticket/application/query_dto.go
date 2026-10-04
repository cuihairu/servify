package application

import (
	"time"

	"servify/apps/server/internal/modules/ticket/domain"
)

type TicketDTO struct {
	ID         uint   `json:"id"`
	Title      string `json:"title"`
	CustomerID uint   `json:"customer_id"`
	AgentID    *uint  `json:"agent_id,omitempty"`
	// SessionID 是工单来源会话（conversation）的 ID；V1.0 收敛 B1-2：
	// ticket.* 事件借它投影进 conversation_events（Service Timeline）。
	SessionID  *string    `json:"session_id,omitempty"`
	Category   string     `json:"category"`
	Priority   string     `json:"priority"`
	Status     string     `json:"status"`
	Source     string     `json:"source"`
	Tags       string     `json:"tags"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
}

type CommentDTO struct {
	ID        uint      `json:"id"`
	UserID    uint      `json:"user_id"`
	Content   string    `json:"content"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type TicketDetailsDTO struct {
	TicketDTO
	Description       string                    `json:"description"`
	SessionID         *string                   `json:"session_id,omitempty"`
	DueDate           *time.Time                `json:"due_date,omitempty"`
	CustomFieldValues []domain.CustomFieldValue `json:"custom_field_values,omitempty"`
	Comments          []domain.Comment          `json:"comments,omitempty"`
	StatusHistory     []domain.StatusChange     `json:"status_history,omitempty"`
}

type ListTicketsResultDTO struct {
	Items []TicketDTO `json:"items"`
	Total int64       `json:"total"`
}

type TicketStatsDTO struct {
	Total        int64              `json:"total"`
	TodayCreated int64              `json:"today_created"`
	Pending      int64              `json:"pending"`
	Resolved     int64              `json:"resolved"`
	ByStatus     []StatusCountDTO   `json:"by_status"`
	ByPriority   []PriorityCountDTO `json:"by_priority"`
}

type StatusCountDTO struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type PriorityCountDTO struct {
	Priority string `json:"priority"`
	Count    int64  `json:"count"`
}

func MapTicket(ticket domain.Ticket) TicketDTO {
	return TicketDTO{
		ID:         ticket.ID,
		Title:      ticket.Title,
		CustomerID: ticket.CustomerID,
		AgentID:    ticket.AgentID,
		SessionID:  ticket.SessionID,
		Category:   ticket.Category,
		Priority:   ticket.Priority,
		Status:     ticket.Status,
		Source:     ticket.Source,
		Tags:       ticket.Tags,
		CreatedAt:  ticket.CreatedAt,
		UpdatedAt:  ticket.UpdatedAt,
		ResolvedAt: ticket.ResolvedAt,
		ClosedAt:   ticket.ClosedAt,
	}
}

func MapTickets(tickets []domain.Ticket) []TicketDTO {
	out := make([]TicketDTO, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, MapTicket(ticket))
	}
	return out
}

func MapTicketDetails(details *domain.TicketDetails) *TicketDetailsDTO {
	if details == nil {
		return nil
	}
	base := MapTicket(details.Ticket)
	return &TicketDetailsDTO{
		TicketDTO:         base,
		Description:       details.Description,
		SessionID:         details.SessionID,
		DueDate:           details.DueDate,
		CustomFieldValues: details.CustomFieldValues,
		Comments:          details.Comments,
		StatusHistory:     details.StatusHistory,
	}
}

func MapComment(comment domain.Comment) CommentDTO {
	return CommentDTO{
		ID:        comment.ID,
		UserID:    comment.UserID,
		Content:   comment.Content,
		Type:      comment.Type,
		CreatedAt: comment.CreatedAt,
	}
}
