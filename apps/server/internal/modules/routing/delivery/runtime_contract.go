package delivery

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
	routingapplication "servify/apps/server/internal/modules/routing/application"

	"gorm.io/gorm"
)

type AssignAgentCommand struct {
	SessionID      string
	AgentID        uint
	FromAgentID    *uint
	Reason         string
	Notes          string
	SessionSummary string
	AssignedAt     time.Time
	// Scoring 可选评分审计（B2-1），直通 application 层落 routing_assignments。
	Scoring *routingapplication.ScoringDetail
}

// RuntimeService is the routing contract used by session-transfer runtime glue.
type RuntimeService interface {
	AddToWaitingQueue(ctx context.Context, tx *gorm.DB, sessionID string, reason string, targetSkills []string, targetGroupID uint, priority string, notes string) (*models.WaitingRecord, error)
	AssignAgent(ctx context.Context, tx *gorm.DB, cmd AssignAgentCommand) (*models.TransferRecord, error)
	GetTransferHistory(ctx context.Context, sessionID string) ([]models.TransferRecord, error)
	ListRecentTransferHistory(ctx context.Context, limit int) ([]models.TransferRecord, error)
	ListWaitingRecords(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error)
	GetWaitingRecord(ctx context.Context, sessionID string) (*models.WaitingRecord, error)
	CancelWaiting(ctx context.Context, tx *gorm.DB, sessionID string, reason string) (*models.WaitingRecord, error)
	MarkWaitingTransferred(ctx context.Context, tx *gorm.DB, sessionID string, agentID uint, assignedAt time.Time) (*models.WaitingRecord, error)
	// ClaimWaitingRecords 原子认领到期等待记录（claim-then-process，租约防双发）。
	ClaimWaitingRecords(ctx context.Context, now, leaseBefore time.Time, limit int) ([]models.WaitingRecord, error)
	// ReleaseWaitingClaim 处理失败归还租约。
	ReleaseWaitingClaim(ctx context.Context, sessionID string) error
	// ListRoutingAssignments 评分审计读口（B2-1，管理面展示分配理由）。
	ListRoutingAssignments(ctx context.Context, sessionID string, limit int) ([]routingapplication.RoutingAssignmentDTO, error)
}
