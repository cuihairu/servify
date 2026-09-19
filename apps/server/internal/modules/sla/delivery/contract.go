package delivery

import (
	"context"

	"servify/apps/server/internal/models"
)

// SLAService 是 HTTP handler 层消费 SLA 业务的契约，
// 由 application.Service 实现。
type SLAService interface {
	CreateSLAConfig(ctx context.Context, req *SLAConfigCreateRequest) (*models.SLAConfig, error)
	GetSLAConfig(ctx context.Context, id uint) (*models.SLAConfig, error)
	ListSLAConfigs(ctx context.Context, req *SLAConfigListRequest) ([]models.SLAConfig, int64, error)
	UpdateSLAConfig(ctx context.Context, id uint, req *SLAConfigUpdateRequest) (*models.SLAConfig, error)
	DeleteSLAConfig(ctx context.Context, id uint) error
	GetSLAConfigByPriority(ctx context.Context, priority string, customerTier string) (*models.SLAConfig, error)
	ListSLAViolations(ctx context.Context, req *SLAViolationListRequest) ([]models.SLAViolation, int64, error)
	ResolveSLAViolation(ctx context.Context, id uint) error
	GetSLAStats(ctx context.Context) (*SLAStatsResponse, error)
	CheckSLAViolation(ctx context.Context, ticket *models.Ticket) (*models.SLAViolation, error)
}
