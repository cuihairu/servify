package delivery

// app_integration 模块交付层：handler 侧契约与纯转发适配器。

import (
	"context"

	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"
)

// HandlerService 应用市场处理器契约。
type HandlerService interface {
	List(ctx context.Context, req *AppIntegrationListRequest) ([]*AppIntegration, int64, error)
	Create(ctx context.Context, req *AppIntegrationCreateRequest) (*AppIntegration, error)
	Update(ctx context.Context, id uint, req *AppIntegrationUpdateRequest) (*AppIntegration, error)
	Delete(ctx context.Context, id uint) error
}

// HandlerServiceAdapter 将模块服务适配为 HandlerService 契约（纯转发）。
type HandlerServiceAdapter struct {
	svc *appintegrationapp.Service
}

// NewHandlerServiceAdapter creates a new adapter.
func NewHandlerServiceAdapter(svc *appintegrationapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{svc: svc}
}

func (a *HandlerServiceAdapter) List(ctx context.Context, req *AppIntegrationListRequest) ([]*AppIntegration, int64, error) {
	return a.svc.List(ctx, req)
}

func (a *HandlerServiceAdapter) Create(ctx context.Context, req *AppIntegrationCreateRequest) (*AppIntegration, error) {
	return a.svc.Create(ctx, req)
}

func (a *HandlerServiceAdapter) Update(ctx context.Context, id uint, req *AppIntegrationUpdateRequest) (*AppIntegration, error) {
	return a.svc.Update(ctx, id, req)
}

func (a *HandlerServiceAdapter) Delete(ctx context.Context, id uint) error {
	return a.svc.Delete(ctx, id)
}
