package contract

import "time"

// TransferRequest is the handler-facing request for human transfer.
type TransferRequest struct {
	SessionID     string   `json:"session_id" binding:"required"`
	Reason        string   `json:"reason"`
	TargetSkills  []string `json:"target_skills"`
	TargetGroupID uint     `json:"target_group_id"` // 指定坐席组（0=不限组）
	Priority      string   `json:"priority"`
	Notes         string   `json:"notes"`
}

// TransferResult is the handler-facing result for session transfer flows.
type TransferResult struct {
	Success       bool       `json:"success"`
	SessionID     string     `json:"session_id"`
	NewAgentID    uint       `json:"new_agent_id,omitempty"`
	IsWaiting     bool       `json:"is_waiting,omitempty"`
	QueuedAt      *time.Time `json:"queued_at,omitempty"`
	TransferredAt time.Time  `json:"transferred_at,omitempty"`
	Summary       string     `json:"summary"`
}
