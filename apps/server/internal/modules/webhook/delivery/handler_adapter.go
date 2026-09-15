package delivery

import (
	"context"
	webhookdomain "servify/apps/server/internal/modules/webhook/domain"

	"servify/apps/server/internal/modules/webhook/application"
)

// HandlerServiceAdapter 把 application.Service 暴露为 handlers 使用的 HandlerService。
type HandlerServiceAdapter struct {
	service *application.Service
}

func NewHandlerServiceAdapter(service *application.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

// Service 返回底层应用服务（供接线注入 deliverer 等）。
func (a *HandlerServiceAdapter) Service() *application.Service {
	return a.service
}

func (a *HandlerServiceAdapter) ListEndpoints(ctx context.Context) ([]webhookdomain.WebhookEndpoint, error) {
	return a.service.ListEndpoints(ctx)
}

func (a *HandlerServiceAdapter) CreateEndpoint(ctx context.Context, req *EndpointCreateRequest) (*webhookdomain.WebhookEndpoint, string, error) {
	if req == nil {
		return nil, "", application.ErrNilRequest
	}
	return a.service.CreateEndpoint(ctx, *req)
}

func (a *HandlerServiceAdapter) UpdateEndpoint(ctx context.Context, id uint, req *EndpointUpdateRequest) (*webhookdomain.WebhookEndpoint, error) {
	if req == nil {
		return nil, application.ErrNilRequest
	}
	return a.service.UpdateEndpoint(ctx, id, *req)
}

func (a *HandlerServiceAdapter) DeleteEndpoint(ctx context.Context, id uint) error {
	return a.service.DeleteEndpoint(ctx, id)
}

func (a *HandlerServiceAdapter) RotateEndpointSecret(ctx context.Context, id uint) (*webhookdomain.WebhookEndpoint, string, error) {
	return a.service.RotateEndpointSecret(ctx, id)
}

func (a *HandlerServiceAdapter) TestEndpoint(ctx context.Context, id uint) (*webhookdomain.WebhookDelivery, error) {
	return a.service.TestEndpoint(ctx, id)
}

func (a *HandlerServiceAdapter) ListDeliveries(ctx context.Context, query application.DeliveryListQuery) ([]webhookdomain.WebhookDelivery, int64, error) {
	return a.service.ListDeliveries(ctx, query)
}

func (a *HandlerServiceAdapter) RedeliverDelivery(ctx context.Context, id uint) (*webhookdomain.WebhookDelivery, error) {
	return a.service.RedeliverDelivery(ctx, id)
}

func (a *HandlerServiceAdapter) SupportedEvents() []string {
	return application.SupportedEvents()
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)
