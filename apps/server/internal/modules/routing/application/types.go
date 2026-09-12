package application

import (
	"time"

	"servify/apps/server/internal/modules/routing/domain"
)

type RequestHumanHandoffCommand struct {
	SessionID    string
	Reason       string
	TargetSkills []string
	Priority     string
	Notes        string
}

type AssignAgentCommand struct {
	SessionID      string
	AgentID        uint
	FromAgentID    *uint
	Reason         string
	Notes          string
	SessionSummary string
	AssignedAt     time.Time
}

type AddToWaitingQueueCommand struct {
	SessionID     string
	Reason        string
	TargetSkills  []string
	TargetGroupID uint // 指定坐席组（0=不限组）
	Priority      string
	Notes         string
}

type CancelWaitingCommand struct {
	SessionID string
	Reason    string
}

type MarkWaitingTransferredCommand struct {
	SessionID  string
	AssignedTo uint
	AssignedAt time.Time
}

type AssignmentDTO struct {
	SessionID      string    `json:"session_id"`
	FromAgentID    *uint     `json:"from_agent_id,omitempty"`
	ToAgentID      uint      `json:"to_agent_id"`
	Reason         string    `json:"reason,omitempty"`
	Notes          string    `json:"notes,omitempty"`
	SessionSummary string    `json:"session_summary,omitempty"`
	AssignedAt     time.Time `json:"assigned_at"`
}

type TransferRecordDTO struct {
	SessionID      string    `json:"session_id"`
	FromAgentID    *uint     `json:"from_agent_id,omitempty"`
	ToAgentID      *uint     `json:"to_agent_id,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Notes          string    `json:"notes,omitempty"`
	SessionSummary string    `json:"session_summary,omitempty"`
	TransferredAt  time.Time `json:"transferred_at"`
}

type QueueEntryDTO struct {
	SessionID     string     `json:"session_id"`
	Reason        string     `json:"reason,omitempty"`
	TargetSkills  []string   `json:"target_skills,omitempty"`
	TargetGroupID uint       `json:"target_group_id,omitempty"`
	Priority      string     `json:"priority,omitempty"`
	Notes         string     `json:"notes,omitempty"`
	Status        string     `json:"status"`
	QueuedAt      time.Time  `json:"queued_at"`
	AssignedAt    *time.Time `json:"assigned_at,omitempty"`
	AssignedTo    *uint      `json:"assigned_to,omitempty"`
}

func MapAssignment(item domain.Assignment) AssignmentDTO {
	return AssignmentDTO{
		SessionID:      item.SessionID,
		FromAgentID:    item.FromAgentID,
		ToAgentID:      item.ToAgentID,
		Reason:         item.Reason,
		Notes:          item.Notes,
		SessionSummary: item.SessionSummary,
		AssignedAt:     item.AssignedAt,
	}
}

func MapTransferRecord(item domain.TransferRecord) TransferRecordDTO {
	return TransferRecordDTO{
		SessionID:      item.SessionID,
		FromAgentID:    item.FromAgentID,
		ToAgentID:      item.ToAgentID,
		Reason:         item.Reason,
		Notes:          item.Notes,
		SessionSummary: item.SessionSummary,
		TransferredAt:  item.TransferredAt,
	}
}

func MapQueueEntry(item domain.QueueEntry) QueueEntryDTO {
	return QueueEntryDTO{
		SessionID:     item.SessionID,
		Reason:        item.Reason,
		TargetSkills:  append([]string(nil), item.TargetSkills...),
		TargetGroupID: item.TargetGroupID,
		Priority:      item.Priority,
		Notes:         item.Notes,
		Status:        string(item.Status),
		QueuedAt:      item.QueuedAt,
		AssignedAt:    item.AssignedAt,
		AssignedTo:    item.AssignedTo,
	}
}
