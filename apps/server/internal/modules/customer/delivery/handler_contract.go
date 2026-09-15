package delivery

import (
	"context"

	"servify/apps/server/internal/models"
	customerapi "servify/apps/server/internal/modules/customer/api"
)

// HandlerService is the only customer contract that HTTP handlers should depend on.
type HandlerService interface {
	CreateCustomer(ctx context.Context, req *customerapi.CustomerCreateRequest) (*models.User, error)
	GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error)
	UpdateCustomer(ctx context.Context, customerID uint, req *customerapi.CustomerUpdateRequest) (*models.User, error)
	ListCustomers(ctx context.Context, req *customerapi.CustomerListRequest) ([]customerapi.CustomerInfo, int64, error)
	GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*customerapi.CustomerActivity, error)
	AddCustomerNote(ctx context.Context, customerID uint, note string, userID uint) error
	UpdateCustomerTags(ctx context.Context, customerID uint, tags []string) error
	GetCustomerStats(ctx context.Context) (*customerapi.CustomerStats, error)
	RevokeCustomerTokens(ctx context.Context, customerID uint) (int, error)
}
