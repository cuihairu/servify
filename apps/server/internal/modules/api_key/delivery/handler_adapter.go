package delivery

// HTTP 交付契约：handlers 依赖本接口而非 module 具体类型，
// adapter 由 application.Service 适配。

import (
	"context"

	apikeyapp "servify/apps/server/internal/modules/api_key/application"

	"servify/apps/server/internal/models"
)

// HandlerService 管理面能力契约（签发/吊销/删除）。
type HandlerService interface {
	List(ctx context.Context) ([]models.APIKey, error)
	Create(ctx context.Context, req *apikeyapp.APIKeyCreateRequest, createdBy string) (*models.APIKey, string, error)
	Revoke(ctx context.Context, id uint) (*models.APIKey, error)
	Delete(ctx context.Context, id uint) error
}

// HandlerServiceAdapter 把 application.Service 适配为 HandlerService。
type HandlerServiceAdapter struct {
	module *apikeyapp.Service
}

func NewHandlerServiceAdapter(module *apikeyapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{module: module}
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)

func (a *HandlerServiceAdapter) List(ctx context.Context) ([]models.APIKey, error) {
	return a.module.List(ctx)
}

func (a *HandlerServiceAdapter) Create(ctx context.Context, req *apikeyapp.APIKeyCreateRequest, createdBy string) (*models.APIKey, string, error) {
	return a.module.Create(ctx, req, createdBy)
}

func (a *HandlerServiceAdapter) Revoke(ctx context.Context, id uint) (*models.APIKey, error) {
	return a.module.Revoke(ctx, id)
}

func (a *HandlerServiceAdapter) Delete(ctx context.Context, id uint) error {
	return a.module.Delete(ctx, id)
}
