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

// RoutingAssignment 是评分审计值对象（B2-1，落 models.RoutingAssignment）：
// transfer_records 记分配事实，本对象记评分视角（分数/因子/理由/策略）。
type RoutingAssignment struct {
	SessionID   string
	FromAgentID *uint
	ToAgentID   uint
	TotalScore  float64
	Factors     map[string]float64
	Reasons     []string
	Strategy    string
	AssignedAt  time.Time
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
