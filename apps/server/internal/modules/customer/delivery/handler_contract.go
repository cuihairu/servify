package delivery

import (
	"context"

	"servify/apps/server/internal/models"
	customerapi "servify/apps/server/internal/modules/customer/api"
	customerapp "servify/apps/server/internal/modules/customer/application"
)

// ErrCustomerNotFound 数据主体不存在（application 层错误透传，handler 据此 404，
// 避免直引 modules/*/application 违反 handler 边界）。
var ErrCustomerNotFound = customerapp.ErrCustomerNotFound

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
	// ExportCustomerData 数据主体 PII 导出（B2-2，管理面合规口）。
	ExportCustomerData(ctx context.Context, customerID uint) (*customerapp.CustomerDataExport, error)
	// EraseCustomerData 数据主体 PII 删除（含关联擦除，B2-2）。
	EraseCustomerData(ctx context.Context, customerID uint) (*customerapp.CustomerDataEraseResult, error)
}
