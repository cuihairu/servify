package delivery

import (
	"context"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	customerapi "servify/apps/server/internal/modules/customer/api"
	customerapp "servify/apps/server/internal/modules/customer/application"
	customerinfra "servify/apps/server/internal/modules/customer/infra"

	"gorm.io/gorm"
)

// HandlerServiceAdapter exposes module-backed customer operations to HTTP handlers.
type HandlerServiceAdapter struct {
	service *customerapp.Service
}

func NewHandlerService(db *gorm.DB) *HandlerServiceAdapter {
	return NewHandlerServiceAdapter(customerapp.NewService(customerinfra.NewGormRepository(db)))
}

func NewHandlerServiceAdapter(service *customerapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) CreateCustomer(ctx context.Context, req *customerapi.CustomerCreateRequest) (*models.User, error) {
	return a.service.CreateCustomer(ctx, customerapp.CreateCustomerCommand{
		Username: req.Username,
		Email:    req.Email,
		Name:     req.Name,
		Phone:    req.Phone,
		Company:  req.Company,
		Industry: req.Industry,
		Source:   req.Source,
		Tags:     splitTags(req.Tags),
		Notes:    req.Notes,
		Priority: req.Priority,
	})
}

func (a *HandlerServiceAdapter) GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error) {
	return a.service.GetCustomerByID(ctx, customerID)
}

func (a *HandlerServiceAdapter) UpdateCustomer(ctx context.Context, customerID uint, req *customerapi.CustomerUpdateRequest) (*models.User, error) {
	cmd := customerapp.UpdateCustomerCommand{
		Name:     req.Name,
		Phone:    req.Phone,
		Company:  req.Company,
		Industry: req.Industry,
		Source:   req.Source,
		Notes:    req.Notes,
		Priority: req.Priority,
		Status:   req.Status,
	}
	if req.Tags != nil {
		tags := splitTags(*req.Tags)
		cmd.Tags = &tags
	}
	return a.service.UpdateCustomer(ctx, customerID, cmd)
}

func (a *HandlerServiceAdapter) ListCustomers(ctx context.Context, req *customerapi.CustomerListRequest) ([]customerapi.CustomerInfo, int64, error) {
	items, total, err := a.service.ListCustomers(ctx, customerapp.ListCustomersQuery{
		Page:      req.Page,
		PageSize:  req.PageSize,
		Search:    req.Search,
		Industry:  req.Industry,
		Source:    req.Source,
		Priority:  req.Priority,
		Status:    req.Status,
		Tags:      splitTags(req.Tags),
		SortBy:    req.SortBy,
		SortOrder: req.SortOrder,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]customerapi.CustomerInfo, 0, len(items))
	for _, item := range items {
		out = append(out, customerInfoFromDTO(item))
	}
	return out, total, nil
}

func (a *HandlerServiceAdapter) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*customerapi.CustomerActivity, error) {
	activity, err := a.service.GetCustomerActivity(ctx, customerID, limit)
	if err != nil {
		return nil, err
	}
	return customerActivityFromDTO(activity), nil
}

func (a *HandlerServiceAdapter) AddCustomerNote(ctx context.Context, customerID uint, note string, userID uint) error {
	return a.service.AddNote(ctx, customerID, note, userID)
}

func (a *HandlerServiceAdapter) UpdateCustomerTags(ctx context.Context, customerID uint, tags []string) error {
	return a.service.UpdateTags(ctx, customerID, tags)
}

func (a *HandlerServiceAdapter) GetCustomerStats(ctx context.Context) (*customerapi.CustomerStats, error) {
	stats, err := a.service.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return customerStatsFromDTO(stats), nil
}

func (a *HandlerServiceAdapter) RevokeCustomerTokens(ctx context.Context, customerID uint) (int, error) {
	return a.service.RevokeCustomerTokens(ctx, customerID, time.Now().UTC())
}

func customerInfoFromDTO(dto customerapp.CustomerInfoDTO) customerapi.CustomerInfo {
	return customerapi.CustomerInfo{
		User:     dto.User,
		Company:  dto.Company,
		Industry: dto.Industry,
		Source:   dto.Source,
		Tags:     dto.Tags,
		Notes:    dto.Notes,
		Priority: dto.Priority,
	}
}

func customerActivityFromDTO(dto *customerapp.CustomerActivityDTO) *customerapi.CustomerActivity {
	if dto == nil {
		return nil
	}
	return &customerapi.CustomerActivity{
		CustomerID:     dto.CustomerID,
		RecentSessions: dto.RecentSessions,
		RecentTickets:  dto.RecentTickets,
		RecentMessages: dto.RecentMessages,
	}
}

func customerStatsFromDTO(dto *customerapp.CustomerStatsDTO) *customerapi.CustomerStats {
	if dto == nil {
		return nil
	}
	return &customerapi.CustomerStats{
		Total:       dto.Total,
		Active:      dto.Active,
		NewThisWeek: dto.NewThisWeek,
		BySource:    sourceCountsFromDTO(dto.BySource),
		ByIndustry:  industryCountsFromDTO(dto.ByIndustry),
		ByPriority:  priorityCountsFromDTO(dto.ByPriority),
	}
}

func sourceCountsFromDTO(items []customerapp.SourceCount) []customerapi.CustomerSourceCount {
	out := make([]customerapi.CustomerSourceCount, 0, len(items))
	for _, item := range items {
		out = append(out, customerapi.CustomerSourceCount{
			Source: item.Source,
			Count:  item.Count,
		})
	}
	return out
}

func industryCountsFromDTO(items []customerapp.IndustryCount) []customerapi.CustomerIndustryCount {
	out := make([]customerapi.CustomerIndustryCount, 0, len(items))
	for _, item := range items {
		out = append(out, customerapi.CustomerIndustryCount{
			Industry: item.Industry,
			Count:    item.Count,
		})
	}
	return out
}

func priorityCountsFromDTO(items []customerapp.PriorityCount) []customerapi.CustomerPriorityCount {
	out := make([]customerapi.CustomerPriorityCount, 0, len(items))
	for _, item := range items {
		out = append(out, customerapi.CustomerPriorityCount{
			Priority: item.Priority,
			Count:    item.Count,
		})
	}
	return out
}

func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
