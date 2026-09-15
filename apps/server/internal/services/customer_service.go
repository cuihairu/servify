package services

import (
	"context"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"
	customerinfra "servify/apps/server/internal/modules/customer/infra"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// CustomerService 客户管理服务兼容层。
type CustomerService struct {
	db     *gorm.DB
	logger *logrus.Logger
	module *customerapp.Service
}

// NewCustomerService 创建客户服务。
func NewCustomerService(db *gorm.DB, logger *logrus.Logger) *CustomerService {
	if logger == nil {
		logger = logrus.New()
	}
	return &CustomerService{
		db:     db,
		logger: logger,
		module: customerapp.NewService(customerinfra.NewGormRepository(db)),
	}
}

// CustomerCreateRequest 创建客户请求。契约定义已迁至 modules/customer/application，
// 此处保留类型别名供 legacy 引用方使用。
type CustomerCreateRequest = customerapp.CustomerCreateRequest

// CustomerUpdateRequest 更新客户请求。
type CustomerUpdateRequest = customerapp.CustomerUpdateRequest

// CustomerListRequest 客户列表请求。
type CustomerListRequest = customerapp.CustomerListRequest

func (s *CustomerService) CreateCustomer(ctx context.Context, req *CustomerCreateRequest) (*models.User, error) {
	return s.module.CreateCustomer(ctx, customerapp.CreateCustomerCommand{
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

func (s *CustomerService) GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error) {
	return s.module.GetCustomerByID(ctx, customerID)
}

func (s *CustomerService) UpdateCustomer(ctx context.Context, customerID uint, req *CustomerUpdateRequest) (*models.User, error) {
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
	return s.module.UpdateCustomer(ctx, customerID, cmd)
}

func (s *CustomerService) ListCustomers(ctx context.Context, req *CustomerListRequest) ([]CustomerInfo, int64, error) {
	items, total, err := s.module.ListCustomers(ctx, customerapp.ListCustomersQuery{
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
	out := make([]CustomerInfo, 0, len(items))
	for _, item := range items {
		out = append(out, customerInfoFromDTO(item))
	}
	return out, total, nil
}

func (s *CustomerService) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*CustomerActivity, error) {
	activity, err := s.module.GetCustomerActivity(ctx, customerID, limit)
	if err != nil {
		return nil, err
	}
	return customerActivityFromDTO(activity), nil
}

func (s *CustomerService) AddCustomerNote(ctx context.Context, customerID uint, note string, userID uint) error {
	return s.module.AddNote(ctx, customerID, note, userID)
}

func (s *CustomerService) UpdateCustomerTags(ctx context.Context, customerID uint, tags []string) error {
	return s.module.UpdateTags(ctx, customerID, tags)
}

func (s *CustomerService) GetCustomerStats(ctx context.Context) (*CustomerStats, error) {
	stats, err := s.module.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return customerStatsFromDTO(stats), nil
}

func (s *CustomerService) RevokeCustomerTokens(ctx context.Context, customerID uint) (int, error) {
	version, err := s.module.RevokeCustomerTokens(ctx, customerID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	s.logger.Infof("Revoked tokens for customer %d, new token version %d", customerID, version)
	return version, nil
}

// CustomerInfo 客户信息（用于列表显示）。
type CustomerInfo = customerapp.CustomerInfo

// CustomerActivity 客户活动记录。
type CustomerActivity = customerapp.CustomerActivity

// CustomerStats 客户统计信息。
type CustomerStats = customerapp.CustomerStats

type CustomerSourceCount = customerapp.CustomerSourceCount

type CustomerIndustryCount = customerapp.CustomerIndustryCount

type CustomerPriorityCount = customerapp.CustomerPriorityCount

func customerInfoFromDTO(dto customerapp.CustomerInfoDTO) CustomerInfo {
	return CustomerInfo{
		User:     dto.User,
		Company:  dto.Company,
		Industry: dto.Industry,
		Source:   dto.Source,
		Tags:     dto.Tags,
		Notes:    dto.Notes,
		Priority: dto.Priority,
	}
}

func customerActivityFromDTO(dto *customerapp.CustomerActivityDTO) *CustomerActivity {
	if dto == nil {
		return nil
	}
	return &CustomerActivity{
		CustomerID:     dto.CustomerID,
		RecentSessions: dto.RecentSessions,
		RecentTickets:  dto.RecentTickets,
		RecentMessages: dto.RecentMessages,
	}
}

func customerStatsFromDTO(dto *customerapp.CustomerStatsDTO) *CustomerStats {
	if dto == nil {
		return nil
	}
	return &CustomerStats{
		Total:       dto.Total,
		Active:      dto.Active,
		NewThisWeek: dto.NewThisWeek,
		BySource:    sourceCountsFromDTO(dto.BySource),
		ByIndustry:  industryCountsFromDTO(dto.ByIndustry),
		ByPriority:  priorityCountsFromDTO(dto.ByPriority),
	}
}

func sourceCountsFromDTO(items []customerapp.SourceCount) []CustomerSourceCount {
	out := make([]CustomerSourceCount, 0, len(items))
	for _, item := range items {
		out = append(out, CustomerSourceCount{
			Source: item.Source,
			Count:  item.Count,
		})
	}
	return out
}

func industryCountsFromDTO(items []customerapp.IndustryCount) []CustomerIndustryCount {
	out := make([]CustomerIndustryCount, 0, len(items))
	for _, item := range items {
		out = append(out, CustomerIndustryCount{
			Industry: item.Industry,
			Count:    item.Count,
		})
	}
	return out
}

func priorityCountsFromDTO(items []customerapp.PriorityCount) []CustomerPriorityCount {
	out := make([]CustomerPriorityCount, 0, len(items))
	for _, item := range items {
		out = append(out, CustomerPriorityCount{
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
