package delivery

import (
	"context"

	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
	suggestioninfra "servify/apps/server/internal/modules/suggestion/infra"

	"gorm.io/gorm"
)

type HandlerServiceAdapter struct {
	service *suggestionapp.Service
}

func NewHandlerService(db *gorm.DB) *HandlerServiceAdapter {
	return NewHandlerServiceAdapter(suggestionapp.NewService(suggestioninfra.NewGormRepository(db)))
}

func NewHandlerServiceAdapter(service *suggestionapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) Suggest(ctx context.Context, req *suggestioncontract.SuggestionRequest) (*suggestioncontract.SuggestionResponse, error) {
	return a.service.Suggest(ctx, req)
}

func (a *HandlerServiceAdapter) InitialQuestions(ctx context.Context, req *suggestioncontract.InitialQuestionsRequest) (*suggestioncontract.InitialQuestionsResponse, error) {
	return a.service.InitialQuestions(ctx, req)
}

func (a *HandlerServiceAdapter) NextQuestions(ctx context.Context, req *suggestioncontract.NextQuestionsRequest) (*suggestioncontract.NextQuestionsResponse, error) {
	return a.service.NextQuestions(ctx, req)
}

func (a *HandlerServiceAdapter) ExposureSummary(ctx context.Context) (*suggestioncontract.ExposureSummaryResponse, error) {
	summary, err := a.service.ExposureSummary(ctx)
	if err != nil {
		return nil, err
	}
	byKind := make([]suggestioncontract.ExposureKindSummary, 0, len(summary.ByKind))
	for _, kind := range summary.ByKind {
		byKind = append(byKind, suggestioncontract.ExposureKindSummary{
			Kind:               kind.Kind,
			TotalExposures:     kind.TotalExposures,
			ConvertedExposures: kind.ConvertedExposures,
		})
	}
	return &suggestioncontract.ExposureSummaryResponse{
		TotalExposures:     summary.TotalExposures,
		ConvertedExposures: summary.ConvertedExposures,
		ByKind:             byKind,
	}, nil
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)
