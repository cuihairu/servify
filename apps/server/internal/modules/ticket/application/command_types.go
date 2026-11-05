package application

import "time"

type CreateTicketCommand struct {
	Title          string
	Description    string
	CustomerID     uint
	IdempotencyKey string
	Category       string
	Priority       string
	Source         string
	Tags           string
	SessionID      *string
}

type UpdateTicketCommand struct {
	Title       *string
	Description *string
	AgentID     *uint
	Category    *string
	Priority    *string
	Status      *string
	Tags        *string
	DueDate     *time.Time
	ActorID     uint
}

type AssignTicketCommand struct {
	AgentID uint
	UserID  uint
}

type UnassignTicketCommand struct {
	UserID uint
	Reason string
}

type AddCommentCommand struct {
	UserID      uint
	Content     string
	CommentType string
}

type BulkUpdateTicketsCommand struct {
	TicketIDs     []uint
	Status        *string
	SetTags       *string
	AddTags       []string
	RemoveTags    []string
	AgentID       *uint
	UnassignAgent bool
	UserID        uint
}

type BulkUpdateFailure struct {
	TicketID uint   `json:"ticket_id"`
	Error    string `json:"error"`
}

type BulkUpdateResult struct {
	Updated []uint              `json:"updated"`
	Failed  []BulkUpdateFailure `json:"failed"`
}

type CloseTicketCommand struct {
	UserID uint
	Reason string
}
