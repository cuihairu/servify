package delivery

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type TransferSession struct {
	ID           string
	CustomerID   uint
	AgentID      *uint
	TicketID     *uint
	Status       string
	Platform     string
	UserName     string
	UserUsername string
}

type RuntimeService interface {
	LoadTransferSession(ctx context.Context, sessionID string) (*TransferSession, error)
	// LockedTransferAgentID 事务内锁会话行读库内 agent_id（转派负载增减以此为准，
	// 不以此前事务外预读为准——并发转派同会话不能按同一 stale 值各减一次）。
	// 行不存在返回 ErrRecordNotFound；未指派会话返回 (nil, nil)。
	LockedTransferAgentID(ctx context.Context, tx *gorm.DB, sessionID string) (*uint, error)
	SyncTransferAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint, agentID uint) error
	SyncWaitingAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint) error
	AppendSystemMessage(ctx context.Context, tx *gorm.DB, sessionID string, content string, createdAt time.Time) error
}
