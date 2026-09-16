package delivery

import (
	"context"
	qualitydomain "servify/apps/server/internal/modules/quality/domain"

	"servify/apps/server/internal/modules/quality/application"
)

// ReviewListQuery / ConfirmCommand 与 application 同形，
// handlers 经 delivery 契约引用，不直接 import application 层。
type ReviewListQuery = application.ReviewListQuery
type ConfirmCommand = application.ConfirmCommand

// 契约层错误转发（handlers 以 errors.Is 判定）。
var (
	ErrNotFound            = application.ErrNotFound
	ErrNotScoreable        = application.ErrNotScoreable
	ErrConfirmedNeedsForce = application.ErrConfirmedNeedsForce
)

// ReviewReviewResult rescore 触发后的当前记录。
type ReviewReviewResult = qualitydomain.QualityReview

// HandlerService 面向 HTTP handlers 的质检管理能力。
type HandlerService interface {
	ListReviews(ctx context.Context, query ReviewListQuery) ([]qualitydomain.QualityReview, int64, error)
	GetReview(ctx context.Context, sessionID string) (*qualitydomain.QualityReview, error)
	ConfirmReview(ctx context.Context, sessionID string, cmd ConfirmCommand) error
	RescoreReview(ctx context.Context, sessionID string, force bool) error
	ScorerEnabled() bool
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)

// HandlerServiceAdapter 把 *application.QualityService 适配为 HandlerService。
type HandlerServiceAdapter struct {
	svc *application.QualityService
}

func NewHandlerServiceAdapter(svc *application.QualityService) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{svc: svc}
}

func (a *HandlerServiceAdapter) ListReviews(ctx context.Context, query ReviewListQuery) ([]qualitydomain.QualityReview, int64, error) {
	return a.svc.ListReviews(ctx, query)
}

func (a *HandlerServiceAdapter) GetReview(ctx context.Context, sessionID string) (*qualitydomain.QualityReview, error) {
	return a.svc.GetReviewBySession(ctx, sessionID)
}

func (a *HandlerServiceAdapter) ConfirmReview(ctx context.Context, sessionID string, cmd ConfirmCommand) error {
	return a.svc.ConfirmReview(ctx, sessionID, cmd)
}

func (a *HandlerServiceAdapter) RescoreReview(ctx context.Context, sessionID string, force bool) error {
	return a.svc.RescoreReview(ctx, sessionID, force)
}

func (a *HandlerServiceAdapter) ScorerEnabled() bool { return a.svc.ScorerEnabled() }
