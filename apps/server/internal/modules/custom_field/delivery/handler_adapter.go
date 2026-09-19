package delivery

// handler 依赖的最小契约 + 纯转发适配器（不含业务逻辑）。

import (
	"context"

	"servify/apps/server/internal/models"
	customfieldapp "servify/apps/server/internal/modules/custom_field/application"
)

// HandlerService 是 custom_field handler 消费的契约。
type HandlerService interface {
	List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error)
	Get(ctx context.Context, id uint) (*models.CustomField, error)
	Create(ctx context.Context, req *CustomFieldCreateRequest) (*models.CustomField, error)
	Update(ctx context.Context, id uint, req *CustomFieldUpdateRequest) (*models.CustomField, error)
	Delete(ctx context.Context, id uint) error
}

// HandlerServiceAdapter 把契约转发到 application.Service。
type HandlerServiceAdapter struct {
	svc *customfieldapp.Service
}

func NewHandlerServiceAdapter(svc *customfieldapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{svc: svc}
}

func (a *HandlerServiceAdapter) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	return a.svc.List(ctx, resource, activeOnly)
}

func (a *HandlerServiceAdapter) Get(ctx context.Context, id uint) (*models.CustomField, error) {
	return a.svc.Get(ctx, id)
}

func (a *HandlerServiceAdapter) Create(ctx context.Context, req *CustomFieldCreateRequest) (*models.CustomField, error) {
	return a.svc.Create(ctx, req)
}

func (a *HandlerServiceAdapter) Update(ctx context.Context, id uint, req *CustomFieldUpdateRequest) (*models.CustomField, error) {
	return a.svc.Update(ctx, id, req)
}

func (a *HandlerServiceAdapter) Delete(ctx context.Context, id uint) error {
	return a.svc.Delete(ctx, id)
}
