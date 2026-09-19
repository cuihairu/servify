package delivery

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
)

// SatisfactionService 是 HTTP handler 层消费满意度业务的契约，
// 由 application.Service 实现。
type SatisfactionService interface {
	CreateSatisfaction(ctx context.Context, req *SatisfactionCreateRequest) (*models.CustomerSatisfaction, error)
	GetSatisfaction(ctx context.Context, id uint) (*models.CustomerSatisfaction, error)
	ListSatisfactions(ctx context.Context, req *SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error)
	ListSurveys(ctx context.Context, req *SatisfactionSurveyListRequest) ([]models.SatisfactionSurvey, int64, error)
	ResendSurvey(ctx context.Context, id uint) (*models.SatisfactionSurvey, error)
	GetSatisfactionByTicket(ctx context.Context, ticketID uint) (*models.CustomerSatisfaction, error)
	GetSatisfactionStats(ctx context.Context, dateFrom, dateTo *time.Time) (*SatisfactionStatsResponse, error)
	UpdateSatisfaction(ctx context.Context, id uint, comment string) (*models.CustomerSatisfaction, error)
	DeleteSatisfaction(ctx context.Context, id uint) error
	GetSurveyPreviewByToken(ctx context.Context, token string) (*SatisfactionSurveyPreview, error)
	RespondSurvey(ctx context.Context, token string, rating int, comment string) (*models.CustomerSatisfaction, error)
}
