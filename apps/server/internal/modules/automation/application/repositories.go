package application

import (
	"context"
	automationdomain "servify/apps/server/internal/modules/automation/domain"
	"time"

	"servify/apps/server/internal/models"
)

type Repository interface {
	ListTriggers(ctx context.Context) ([]automationdomain.AutomationTrigger, error)
	ListActiveTriggersByEvent(ctx context.Context, event string) ([]automationdomain.AutomationTrigger, error)
	CreateTrigger(ctx context.Context, req TriggerRequest) (*automationdomain.AutomationTrigger, error)
	DeleteTrigger(ctx context.Context, id uint) error
	ListRuns(ctx context.Context, query RunListQuery) ([]automationdomain.AutomationRun, int64, error)
	RecordRun(ctx context.Context, triggerID uint, ticketID uint, status, message string) error
	GetTicket(ctx context.Context, ticketID uint) (*models.Ticket, error)
	UpdateTicketPriority(ctx context.Context, ticketID uint, priority string) error
	UpdateTicketTags(ctx context.Context, ticketID uint, tags string) error
	CreateTicketComment(ctx context.Context, ticketID uint, content string) error
	// CreateTimer 为 delay 动作入队一张到期执行单。
	CreateTimer(ctx context.Context, timer *automationdomain.AutomationTimer) error
	// ClaimDueTimers 返回到期且仍为 pending 的执行单（只读阶段，
	// 执行前必须经 CompleteTimer 乐观抢占，多实例并发下恰好一次）。
	ClaimDueTimers(ctx context.Context, now time.Time, limit int) ([]automationdomain.AutomationTimer, error)
	// CompleteTimer 把执行单从 pending 翻转为 done；返回 false 表示已被其他实例抢先。
	CompleteTimer(ctx context.Context, id uint, now time.Time) bool
	// UpdateTimerLastError 记录执行失败原因（不自动重试，供人工排查与重放）。
	UpdateTimerLastError(ctx context.Context, id uint, message string) error
}
