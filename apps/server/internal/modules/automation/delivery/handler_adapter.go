package delivery

import (
	"context"
	automationdomain "servify/apps/server/internal/modules/automation/domain"

	automationapp "servify/apps/server/internal/modules/automation/application"
	automationinfra "servify/apps/server/internal/modules/automation/infra"

	"gorm.io/gorm"
)

// HandlerServiceAdapter exposes module-backed automation operations to HTTP handlers.
type HandlerServiceAdapter struct {
	service *automationapp.Service
}

func NewHandlerService(db *gorm.DB) *HandlerServiceAdapter {
	return NewHandlerServiceAdapter(automationapp.NewService(automationinfra.NewGormRepository(db)))
}

func NewHandlerServiceAdapter(service *automationapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) ListTriggers(ctx context.Context) ([]automationdomain.AutomationTrigger, error) {
	return a.service.ListTriggers(ctx)
}

func (a *HandlerServiceAdapter) CreateTrigger(ctx context.Context, req *automationapp.TriggerRequest) (*automationdomain.AutomationTrigger, error) {
	if req == nil {
		return nil, nil
	}
	return a.service.CreateTrigger(ctx, *req)
}

func (a *HandlerServiceAdapter) DeleteTrigger(ctx context.Context, id uint) error {
	return a.service.DeleteTrigger(ctx, id)
}

func (a *HandlerServiceAdapter) ListRuns(ctx context.Context, req *automationapp.RunListQuery) ([]automationdomain.AutomationRun, int64, error) {
	query := automationapp.RunListQuery{}
	if req != nil {
		query = *req
	}
	return a.service.ListRuns(ctx, query)
}

func (a *HandlerServiceAdapter) BatchRun(ctx context.Context, req *automationapp.BatchRunRequest) (*automationapp.BatchRunResponse, error) {
	if req == nil {
		return nil, nil
	}
	return a.service.BatchRun(ctx, *req)
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)
