package delivery

import (
	"context"

	macroapp "servify/apps/server/internal/modules/macro/application"

	"servify/apps/server/internal/models"
)

// HandlerService 宏/模板管理面契约。
type HandlerService interface {
	List(ctx context.Context) ([]models.Macro, error)
	Create(ctx context.Context, req *MacroCreateRequest) (*models.Macro, error)
	Update(ctx context.Context, id uint, req *MacroUpdateRequest) (*models.Macro, error)
	Delete(ctx context.Context, id uint) error
	ApplyToTicket(ctx context.Context, macroID, ticketID, actorID uint) (*models.TicketComment, error)
}

// HandlerServiceAdapter 把 application.Service 适配为 HandlerService。
type HandlerServiceAdapter struct {
	module *macroapp.Service
}

func NewHandlerServiceAdapter(module *macroapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{module: module}
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)

func (a *HandlerServiceAdapter) List(ctx context.Context) ([]models.Macro, error) {
	return a.module.List(ctx)
}

func (a *HandlerServiceAdapter) Create(ctx context.Context, req *MacroCreateRequest) (*models.Macro, error) {
	return a.module.Create(ctx, req)
}

func (a *HandlerServiceAdapter) Update(ctx context.Context, id uint, req *MacroUpdateRequest) (*models.Macro, error) {
	return a.module.Update(ctx, id, req)
}

func (a *HandlerServiceAdapter) Delete(ctx context.Context, id uint) error {
	return a.module.Delete(ctx, id)
}

func (a *HandlerServiceAdapter) ApplyToTicket(ctx context.Context, macroID, ticketID, actorID uint) (*models.TicketComment, error) {
	return a.module.ApplyToTicket(ctx, macroID, ticketID, actorID)
}
