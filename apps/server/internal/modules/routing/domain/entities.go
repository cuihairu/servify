package domain

import "time"

type QueueStatus string

const (
	QueueStatusWaiting     QueueStatus = "waiting"
	QueueStatusTransferred QueueStatus = "transferred"
	QueueStatusCancelled   QueueStatus = "cancelled"
)

type Assignment struct {
	SessionID      string
	FromAgentID    *uint
	ToAgentID      uint
	Reason         string
	Notes          string
	SessionSummary string
	AssignedAt     time.Time
}

type QueueEntry struct {
	SessionID     string
	Reason        string
	TargetSkills  []string
	TargetGroupID uint // 指定坐席组（0=不限组）
	Priority      string
	Notes         string
	Status        QueueStatus
	QueuedAt      time.Time
	ClaimedAt     *time.Time // worker 认领租约起点
	AssignedAt    *time.Time
	AssignedTo    *uint
}

type TransferRecord struct {
	SessionID      string
	FromAgentID    *uint
	ToAgentID      *uint
	Reason         string
	Notes          string
	SessionSummary string
	TransferredAt  time.Time
}

type AgentAvailabilityPolicy struct {
	AllowStatuses   []string
	RequireCapacity bool
}

type SkillMatchPolicy struct {
	RequiredSkills []string
	Mode           string
}

type LoadBalancePolicy struct {
	Strategy string
}
