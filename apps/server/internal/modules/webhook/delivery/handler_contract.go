package delivery

import (
	"context"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"
)

// EndpointCreateRequest / EndpointUpdateRequest 与 application.EndpointRequest 同形。
type EndpointCreateRequest = application.EndpointRequest
type EndpointUpdateRequest = application.EndpointRequest

// HandlerService 面向 HTTP handlers 的 webhook 管理能力。
type HandlerService interface {
	ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error)
	CreateEndpoint(ctx context.Context, req *EndpointCreateRequest) (*models.WebhookEndpoint, string, error)
	UpdateEndpoint(ctx context.Context, id uint, req *EndpointUpdateRequest) (*models.WebhookEndpoint, error)
	DeleteEndpoint(ctx context.Context, id uint) error
	RotateEndpointSecret(ctx context.Context, id uint) (*models.WebhookEndpoint, string, error)
	TestEndpoint(ctx context.Context, id uint) (*models.WebhookDelivery, error)
	ListDeliveries(ctx context.Context, query application.DeliveryListQuery) ([]models.WebhookDelivery, int64, error)
	RedeliverDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error)
	SupportedEvents() []string
}
