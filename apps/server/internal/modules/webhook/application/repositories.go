package application

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
)

// DeliveryListQuery 投递日志分页查询。
type DeliveryListQuery struct {
	EndpointID uint
	Status     string
	Page       int
	PageSize   int
}

type Repository interface {
	// 端点管理
	ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error)
	GetEndpoint(ctx context.Context, id uint) (*models.WebhookEndpoint, error)
	CreateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	UpdateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	DeleteEndpoint(ctx context.Context, id uint) error

	// 投递日志
	CreateDelivery(ctx context.Context, d *models.WebhookDelivery) error
	ListDeliveries(ctx context.Context, query DeliveryListQuery) ([]models.WebhookDelivery, int64, error)
	GetDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error)
	ResetDeliveryForRedeliver(ctx context.Context, id uint) error
	ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]models.WebhookDelivery, error)
	MarkDeliverySuccess(ctx context.Context, id uint, httpStatus int, durationMs int64, deliveredAt time.Time) error
	MarkDeliveryFailure(ctx context.Context, id uint, attempt int, httpStatus int, durationMs int64, lastError string, dead bool, nextRetryAt *time.Time) error

	// 载荷回查：bus 事件载荷不可跨 Redis 透传，订阅者按 AggregateID 前缀回查快照
	GetTicketSnapshot(ctx context.Context, ticketID uint) (*models.Ticket, error)
	GetSessionSnapshot(ctx context.Context, sessionID string) (*models.Session, error)
	GetCallSnapshot(ctx context.Context, callID string) (*models.VoiceCall, error)
}
