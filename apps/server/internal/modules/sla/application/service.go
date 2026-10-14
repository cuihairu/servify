package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SLAService SLA配置和监控服务
type SLAService struct {
	repo             Repository
	logger           *logrus.Logger
	tracer           trace.Tracer
	automationModule *automationapp.Service
}

// NewService 创建SLA服务
func NewService(repo Repository, logger *logrus.Logger) *SLAService {
	if logger == nil {
		logger = logrus.New()
	}

	return &SLAService{
		repo:   repo,
		logger: logger,
		tracer: otel.Tracer("servify.sla"),
	}
}

// SLAMonitor 是后台 SLA 违约扫描所需的窄接口
// （app/worker 的 SLAMonitorWorker 周期驱动单轮扫描，循环在 worker 侧）。
type SLAMonitor interface {
	RunMonitorOnce(ctx context.Context) error
}

// SetAutomationModule 注入 automation module 实例，用于在违约时触发规则
func (s *SLAService) SetAutomationModule(module *automationapp.Service) {
	s.automationModule = module
}

// SLAConfigCreateRequest 创建SLA配置请求
type SLAConfigCreateRequest struct {
	Name              string   `json:"name" binding:"required"`
	Priority          string   `json:"priority" binding:"required"`                          // low, normal, high, urgent
	CustomerTier      string   `json:"customer_tier"`                                        // 针对特定客户级别（可选）
	Tags              []string `json:"tags"`                                                 // 标签条件
	WarningThreshold  *int     `json:"warning_threshold" binding:"omitempty,min=50,max=100"` // 触发预警的百分比，默认80
	FirstResponseTime int      `json:"first_response_time" binding:"required,min=1"`         // 分钟
	ResolutionTime    int      `json:"resolution_time" binding:"required,min=1"`             // 分钟
	EscalationTime    int      `json:"escalation_time" binding:"required,min=1"`             // 分钟
	BusinessHoursOnly bool     `json:"business_hours_only"`
	Active            *bool    `json:"active"`
}

// SLAConfigUpdateRequest 更新SLA配置请求
type SLAConfigUpdateRequest struct {
	Name              *string  `json:"name"`
	Priority          *string  `json:"priority"`
	CustomerTier      *string  `json:"customer_tier"`
	Tags              []string `json:"tags"`
	WarningThreshold  *int     `json:"warning_threshold" binding:"omitempty,min=50,max=100"`
	FirstResponseTime *int     `json:"first_response_time"`
	ResolutionTime    *int     `json:"resolution_time"`
	EscalationTime    *int     `json:"escalation_time"`
	BusinessHoursOnly *bool    `json:"business_hours_only"`
	Active            *bool    `json:"active"`
}

// SLAConfigListRequest SLA配置列表请求
type SLAConfigListRequest struct {
	Page         int      `form:"page,default=1"`
	PageSize     int      `form:"page_size,default=20"`
	Priority     []string `form:"priority"`
	CustomerTier []string `form:"customer_tier"`
	Active       *bool    `form:"active"`
	SortBy       string   `form:"sort_by,default=created_at"`
	SortOrder    string   `form:"sort_order,default=desc"`
}

// SLAViolationListRequest SLA违约列表请求
type SLAViolationListRequest struct {
	Page          int        `form:"page,default=1"`
	PageSize      int        `form:"page_size,default=20"`
	TicketID      *uint      `form:"ticket_id"`
	SLAConfigID   *uint      `form:"sla_config_id"`
	ViolationType []string   `form:"violation_type"`
	Resolved      *bool      `form:"resolved"`
	DateFrom      *time.Time `form:"date_from"`
	DateTo        *time.Time `form:"date_to"`
	SortBy        string     `form:"sort_by,default=created_at"`
	SortOrder     string     `form:"sort_order,default=desc"`
}

// SLAStatsResponse SLA统计响应
type SLAStatsResponse struct {
	TotalConfigs         int                  `json:"total_configs"`
	ActiveConfigs        int                  `json:"active_configs"`
	TotalViolations      int                  `json:"total_violations"`
	UnresolvedViolations int                  `json:"unresolved_violations"`
	ViolationsByType     map[string]int       `json:"violations_by_type"`
	ViolationsByPriority map[string]int       `json:"violations_by_priority"`
	ViolationsByTier     map[string]int       `json:"violations_by_tier"`
	ComplianceRate       float64              `json:"compliance_rate"` // 合规率
	TrendData            []SLAComplianceTrend `json:"trend_data"`
}

// SLAComplianceTrend SLA合规趋势
type SLAComplianceTrend struct {
	Date           string  `json:"date"`
	TotalTickets   int     `json:"total_tickets"`
	Violations     int     `json:"violations"`
	ComplianceRate float64 `json:"compliance_rate"`
	Tier           string  `json:"tier,omitempty"`
}

// CreateSLAConfig 创建SLA配置
func (s *SLAService) CreateSLAConfig(ctx context.Context, req *SLAConfigCreateRequest) (*models.SLAConfig, error) {
	ctx, span := s.tracer.Start(ctx, "sla.create_config")
	defer span.End()

	span.SetAttributes(
		attribute.String("sla.config.name", req.Name),
		attribute.String("sla.config.priority", req.Priority),
		attribute.Int("sla.config.first_response_time", req.FirstResponseTime),
		attribute.Int("sla.config.resolution_time", req.ResolutionTime),
	)

	// 验证优先级
	validPriorities := map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
	if !validPriorities[req.Priority] {
		return nil, fmt.Errorf("invalid priority: %s", req.Priority)
	}

	// 验证时间逻辑
	if req.FirstResponseTime >= req.ResolutionTime {
		return nil, fmt.Errorf("first response time must be less than resolution time")
	}

	if req.EscalationTime >= req.ResolutionTime {
		return nil, fmt.Errorf("escalation time must be less than resolution time")
	}

	tier := normalizeTier(req.CustomerTier)
	warning := defaultWarning(req.WarningThreshold)

	// 检查相同优先级 + 客户级别是否已有配置
	if existingConfig, err := s.repo.FindConfigByPriorityTier(ctx, req.Priority, tier); err != nil {
		return nil, fmt.Errorf("failed to check existing config: %w", err)
	} else if existingConfig != nil {
		return nil, fmt.Errorf("SLA config for priority '%s' and tier '%s' already exists", req.Priority, tier)
	}

	// 设置默认值
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	// 创建SLA配置
	tenantID, workspaceID := tenantAndWorkspace(ctx)
	config := &models.SLAConfig{
		TenantID:          tenantID,
		WorkspaceID:       workspaceID,
		Name:              req.Name,
		Priority:          req.Priority,
		CustomerTier:      tier,
		WarningThreshold:  warning,
		Tags:              joinTags(req.Tags),
		FirstResponseTime: req.FirstResponseTime,
		ResolutionTime:    req.ResolutionTime,
		EscalationTime:    req.EscalationTime,
		BusinessHoursOnly: req.BusinessHoursOnly,
		Active:            active,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	if err := s.repo.CreateConfig(ctx, config); err != nil {
		span.RecordError(err)
		s.logger.Errorf("Failed to create SLA config: %v", err)
		return nil, fmt.Errorf("failed to create SLA config: %w", err)
	}

	s.logger.Infof("Created SLA config: name=%s, priority=%s, first_response=%dm, resolution=%dm",
		req.Name, req.Priority, req.FirstResponseTime, req.ResolutionTime)

	return config, nil
}

// GetSLAConfig 获取SLA配置
func (s *SLAService) GetSLAConfig(ctx context.Context, id uint) (*models.SLAConfig, error) {
	ctx, span := s.tracer.Start(ctx, "sla.get_config")
	defer span.End()

	span.SetAttributes(attribute.Int64("sla.config.id", int64(id)))

	config, err := s.repo.GetConfigScoped(ctx, id)
	if err != nil {
		if errors.Is(err, ErrSLAConfigNotFound) {
			return nil, ErrSLAConfigNotFound
		}
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get SLA config: %w", err)
	}

	return config, nil
}

// ListSLAConfigs 获取SLA配置列表
func (s *SLAService) ListSLAConfigs(ctx context.Context, req *SLAConfigListRequest) ([]models.SLAConfig, int64, error) {
	ctx, span := s.tracer.Start(ctx, "sla.list_configs")
	defer span.End()

	total, err := s.repo.CountConfigsFiltered(ctx, req)
	if err != nil {
		span.RecordError(err)
		return nil, 0, fmt.Errorf("failed to count SLA configs: %w", err)
	}

	configs, err := s.repo.ListConfigs(ctx, req)
	if err != nil {
		span.RecordError(err)
		return nil, 0, fmt.Errorf("failed to list SLA configs: %w", err)
	}

	span.SetAttributes(attribute.Int64("sla.configs.total", total))
	return configs, total, nil
}

// UpdateSLAConfig 更新SLA配置
func (s *SLAService) UpdateSLAConfig(ctx context.Context, id uint, req *SLAConfigUpdateRequest) (*models.SLAConfig, error) {
	ctx, span := s.tracer.Start(ctx, "sla.update_config")
	defer span.End()

	span.SetAttributes(attribute.Int64("sla.config.id", int64(id)))

	config, err := s.repo.GetConfigScoped(ctx, id)
	if err != nil {
		if errors.Is(err, ErrSLAConfigNotFound) {
			return nil, ErrSLAConfigNotFound
		}
		span.RecordError(err)
		return nil, fmt.Errorf("failed to find SLA config: %w", err)
	}

	// 更新字段
	if req.Name != nil {
		config.Name = *req.Name
	}
	if req.Priority != nil {
		// 验证优先级
		validPriorities := map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
		if !validPriorities[*req.Priority] {
			return nil, fmt.Errorf("invalid priority: %s", *req.Priority)
		}

		// 检查是否有其他配置使用这个优先级
		if *req.Priority != config.Priority {
			existingConfig, err := s.repo.FindConfigByPriorityExceptID(ctx, *req.Priority, id)
			if err != nil {
				return nil, fmt.Errorf("failed to check existing config: %w", err)
			}
			if existingConfig != nil {
				return nil, fmt.Errorf("SLA config for priority '%s' already exists", *req.Priority)
			}
		}
		config.Priority = *req.Priority
	}
	if req.CustomerTier != nil {
		config.CustomerTier = normalizeTier(*req.CustomerTier)
	}
	if req.Tags != nil {
		config.Tags = joinTags(req.Tags)
	}
	if req.WarningThreshold != nil {
		config.WarningThreshold = defaultWarning(req.WarningThreshold)
	}
	if req.FirstResponseTime != nil {
		if *req.FirstResponseTime < 1 {
			return nil, fmt.Errorf("first response time must be at least 1 minute")
		}
		config.FirstResponseTime = *req.FirstResponseTime
	}
	if req.ResolutionTime != nil {
		if *req.ResolutionTime < 1 {
			return nil, fmt.Errorf("resolution time must be at least 1 minute")
		}
		config.ResolutionTime = *req.ResolutionTime
	}
	if req.EscalationTime != nil {
		if *req.EscalationTime < 1 {
			return nil, fmt.Errorf("escalation time must be at least 1 minute")
		}
		config.EscalationTime = *req.EscalationTime
	}
	if req.BusinessHoursOnly != nil {
		config.BusinessHoursOnly = *req.BusinessHoursOnly
	}
	if req.Active != nil {
		config.Active = *req.Active
	}

	// 验证时间逻辑
	if config.FirstResponseTime >= config.ResolutionTime {
		return nil, fmt.Errorf("first response time must be less than resolution time")
	}
	if config.EscalationTime >= config.ResolutionTime {
		return nil, fmt.Errorf("escalation time must be less than resolution time")
	}

	// 组合唯一性：优先级 + 客户级别
	conflict, err := s.repo.FindConfigByPriorityTierExceptID(ctx, config.Priority, config.CustomerTier, id)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing config: %w", err)
	}
	if conflict != nil {
		return nil, fmt.Errorf("SLA config for priority '%s' and tier '%s' already exists", config.Priority, config.CustomerTier)
	}

	config.UpdatedAt = time.Now()

	if err := s.repo.SaveConfig(ctx, config); err != nil {
		span.RecordError(err)
		s.logger.Errorf("Failed to update SLA config: %v", err)
		return nil, fmt.Errorf("failed to update SLA config: %w", err)
	}

	s.logger.Infof("Updated SLA config: id=%d, name=%s", id, config.Name)
	return config, nil
}

// DeleteSLAConfig 删除SLA配置
func (s *SLAService) DeleteSLAConfig(ctx context.Context, id uint) error {
	ctx, span := s.tracer.Start(ctx, "sla.delete_config")
	defer span.End()

	span.SetAttributes(attribute.Int64("sla.config.id", int64(id)))

	// 检查是否有关联的违约记录
	violationCount, err := s.repo.CountViolationsByConfig(ctx, id)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to check SLA violations: %w", err)
	}

	if violationCount > 0 {
		return fmt.Errorf("cannot delete SLA config: it has %d associated violations", violationCount)
	}

	rows, err := s.repo.DeleteConfig(ctx, id)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete SLA config: %w", err)
	}
	if rows == 0 {
		return ErrSLAConfigNotFound
	}

	s.logger.Infof("Deleted SLA config: id=%d", id)
	return nil
}

// GetSLAConfigByPriority 根据优先级(可选客户级别)获取SLA配置
func (s *SLAService) GetSLAConfigByPriority(ctx context.Context, priority string, customerTier string) (*models.SLAConfig, error) {
	ctx, span := s.tracer.Start(ctx, "sla.get_config_by_priority")
	defer span.End()

	span.SetAttributes(
		attribute.String("sla.config.priority", priority),
		attribute.String("sla.config.customer_tier", customerTier),
	)

	tier := normalizeTier(customerTier)

	// 优先匹配特定客户级别
	if tier != "" {
		config, err := s.repo.FindConfigByPriorityTierActive(ctx, priority, tier)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to get SLA config by priority and tier: %w", err)
		}
		if config != nil {
			return config, nil
		}
	}

	// 回退默认配置（未指定客户级别）
	config, err := s.repo.FindDefaultConfigByPriorityActive(ctx, priority)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get SLA config by priority: %w", err)
	}
	if config == nil {
		return nil, nil // 未找到配置
	}

	return config, nil
}

// CheckSLAViolation 检查工单是否违反SLA
func (s *SLAService) CheckSLAViolation(ctx context.Context, ticket *models.Ticket) (*models.SLAViolation, error) {
	ctx, span := s.tracer.Start(ctx, "sla.check_violation")
	defer span.End()
	if ticket == nil {
		return nil, fmt.Errorf("ticket required")
	}

	span.SetAttributes(
		attribute.Int64("sla.ticket.id", int64(ticket.ID)),
		attribute.String("sla.ticket.priority", ticket.Priority),
		attribute.String("sla.ticket.status", ticket.Status),
	)

	scopeCtx := slaRecordScopeContext(ctx, ticket.TenantID, ticket.WorkspaceID)
	scopedTicket, err := s.repo.GetTicketScoped(scopeCtx, ticket.ID)
	if err != nil {
		if errors.Is(err, ErrTicketNotFound) {
			return nil, ErrTicketNotFound
		}
		return nil, fmt.Errorf("failed to validate ticket scope: %w", err)
	}

	customerTier := s.resolveCustomerTier(scopeCtx, scopedTicket.CustomerID)

	// 获取对应的SLA配置（优先匹配客户级别）
	slaConfig, err := s.GetSLAConfigByPriority(scopeCtx, scopedTicket.Priority, customerTier)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get SLA config: %w", err)
	}
	if slaConfig == nil {
		s.logger.Debugf("No SLA config for priority: %s", scopedTicket.Priority)
		return nil, nil // 没有SLA配置，不检查违约
	}

	now := time.Now()
	violation := s.detectViolation(scopedTicket, slaConfig, now)
	if violation == nil {
		return nil, nil // 没有违约
	}

	// 检查同类型的违约是否已经存在
	existingViolation, err := s.repo.FindViolationByTicketConfigType(scopeCtx, scopedTicket.ID, slaConfig.ID, violation.ViolationType)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to check existing violation: %w", err)
	}
	if existingViolation != nil {
		return existingViolation, nil // 已存在相同类型的违约记录
	}

	// 创建违约记录
	if err := s.repo.CreateViolation(ctx, violation); err != nil {
		span.RecordError(err)
		s.logger.Errorf("Failed to create SLA violation: %v", err)
		return nil, fmt.Errorf("failed to create SLA violation: %w", err)
	}

	createdViolation, err := s.repo.GetViolationScoped(scopeCtx, violation.ID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to load created SLA violation: %w", err)
	}

	s.logger.Warnf("SLA violation detected: ticket=%d, type=%s, deadline=%s",
		scopedTicket.ID, violation.ViolationType, violation.Deadline.Format(time.RFC3339))

	// 触发自动化
	if s.automationModule != nil {
		go s.automationModule.HandleEvent(context.Background(), automationapp.Event{
			Type:     "sla_violation",
			TicketID: scopedTicket.ID,
			Payload:  violation,
		})
	}

	return createdViolation, nil
}

// detectViolation 检测具体的违约类型
func (s *SLAService) detectViolation(ticket *models.Ticket, slaConfig *models.SLAConfig, now time.Time) *models.SLAViolation {
	createdAt := ticket.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	// 计算截止时间
	firstResponseDeadline := createdAt.Add(time.Duration(slaConfig.FirstResponseTime) * time.Minute)
	resolutionDeadline := createdAt.Add(time.Duration(slaConfig.ResolutionTime) * time.Minute)

	// 检查首次响应时间违约（尚未分配坐席）
	if ticket.AgentID == nil && now.After(firstResponseDeadline) {
		return &models.SLAViolation{
			TenantID:      ticket.TenantID,
			WorkspaceID:   ticket.WorkspaceID,
			TicketID:      ticket.ID,
			SLAConfigID:   slaConfig.ID,
			ViolationType: "first_response",
			Deadline:      firstResponseDeadline,
			ViolatedAt:    now,
			Resolved:      false,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}

	// 检查解决时间违约
	if ticket.Status != "resolved" && ticket.Status != "closed" && now.After(resolutionDeadline) {
		return &models.SLAViolation{
			TenantID:      ticket.TenantID,
			WorkspaceID:   ticket.WorkspaceID,
			TicketID:      ticket.ID,
			SLAConfigID:   slaConfig.ID,
			ViolationType: "resolution",
			Deadline:      resolutionDeadline,
			ViolatedAt:    now,
			Resolved:      false,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}

	return nil
}

// CreateSLAViolation 创建SLA违约记录
func (s *SLAService) CreateSLAViolation(ctx context.Context, violation *models.SLAViolation) error {
	ctx, span := s.tracer.Start(ctx, "sla.create_violation")
	defer span.End()
	if violation == nil {
		return fmt.Errorf("violation required")
	}

	span.SetAttributes(
		attribute.Int64("sla.violation.ticket_id", int64(violation.TicketID)),
		attribute.Int64("sla.violation.sla_config_id", int64(violation.SLAConfigID)),
		attribute.String("sla.violation.type", violation.ViolationType),
	)

	scopeCtx := slaRecordScopeContext(ctx, violation.TenantID, violation.WorkspaceID)
	ticket, err := s.repo.GetTicketScoped(scopeCtx, violation.TicketID)
	if err != nil {
		if errors.Is(err, ErrTicketNotFound) {
			return ErrTicketNotFound
		}
		return fmt.Errorf("failed to validate violation ticket: %w", err)
	}
	if _, err := s.repo.GetConfigScoped(scopeCtx, violation.SLAConfigID); err != nil {
		if errors.Is(err, ErrSLAConfigNotFound) {
			return fmt.Errorf("sla config not found")
		}
		return fmt.Errorf("failed to validate violation config: %w", err)
	}

	now := time.Now()
	violation.CreatedAt = now
	violation.UpdatedAt = now
	violation.TenantID = ticket.TenantID
	violation.WorkspaceID = ticket.WorkspaceID

	if err := s.repo.CreateViolation(ctx, violation); err != nil {
		span.RecordError(err)
		s.logger.Errorf("Failed to create SLA violation: %v", err)
		return fmt.Errorf("failed to create SLA violation: %w", err)
	}

	s.logger.Infof("Created SLA violation: ticket=%d, type=%s", violation.TicketID, violation.ViolationType)
	return nil
}

// ListSLAViolations 获取SLA违约列表
func (s *SLAService) ListSLAViolations(ctx context.Context, req *SLAViolationListRequest) ([]models.SLAViolation, int64, error) {
	ctx, span := s.tracer.Start(ctx, "sla.list_violations")
	defer span.End()

	total, err := s.repo.CountViolationsFiltered(ctx, req)
	if err != nil {
		span.RecordError(err)
		return nil, 0, fmt.Errorf("failed to count SLA violations: %w", err)
	}

	violations, err := s.repo.ListViolations(ctx, req)
	if err != nil {
		span.RecordError(err)
		return nil, 0, fmt.Errorf("failed to list SLA violations: %w", err)
	}

	span.SetAttributes(attribute.Int64("sla.violations.total", total))
	return violations, total, nil
}

// ResolveSLAViolation 标记SLA违约为已解决
func (s *SLAService) ResolveSLAViolation(ctx context.Context, id uint) error {
	ctx, span := s.tracer.Start(ctx, "sla.resolve_violation")
	defer span.End()

	span.SetAttributes(attribute.Int64("sla.violation.id", int64(id)))

	rows, err := s.repo.ResolveViolation(ctx, id)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to resolve SLA violation: %w", err)
	}
	if rows == 0 {
		return ErrSLAViolationNotFound
	}

	s.logger.Infof("Resolved SLA violation: id=%d", id)
	return nil
}

// ResolveViolationsByTicket 按工单批量解决违约
func (s *SLAService) ResolveViolationsByTicket(ctx context.Context, ticketID uint, violationTypes []string) error {
	ctx, span := s.tracer.Start(ctx, "sla.resolve_ticket_violations")
	defer span.End()

	if _, err := s.repo.GetTicketScoped(ctx, ticketID); err != nil {
		if errors.Is(err, ErrTicketNotFound) {
			return ErrTicketNotFound
		}
		span.RecordError(err)
		return fmt.Errorf("failed to validate ticket: %w", err)
	}

	rows, err := s.repo.ResolveViolationsByTicket(ctx, ticketID, violationTypes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to resolve SLA violations: %w", err)
	}

	span.SetAttributes(
		attribute.Int64("sla.violation.ticket_id", int64(ticketID)),
		attribute.Int64("sla.violation.rows", rows),
	)
	return nil
}

// GetSLAStats 获取SLA统计信息
func (s *SLAService) GetSLAStats(ctx context.Context) (*SLAStatsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "sla.get_stats")
	defer span.End()

	stats := &SLAStatsResponse{
		ViolationsByType:     make(map[string]int),
		ViolationsByPriority: make(map[string]int),
		ViolationsByTier:     make(map[string]int),
		TrendData:            []SLAComplianceTrend{},
	}

	// 统计SLA配置数量
	totalConfigs, err := s.repo.CountConfigs(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to count SLA configs: %w", err)
	}
	stats.TotalConfigs = int(totalConfigs)

	activeConfigs, err := s.repo.CountActiveConfigs(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to count active SLA configs: %w", err)
	}
	stats.ActiveConfigs = int(activeConfigs)

	// 统计违约数量
	totalViolations, err := s.repo.CountViolations(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to count total violations: %w", err)
	}
	stats.TotalViolations = int(totalViolations)

	unresolvedViolations, err := s.repo.CountUnresolvedViolations(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to count unresolved violations: %w", err)
	}
	stats.UnresolvedViolations = int(unresolvedViolations)

	// 按类型统计违约
	byType, err := s.repo.ViolationsByType(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get violation type stats: %w", err)
	}
	for typ, count := range byType {
		stats.ViolationsByType[typ] = count
	}

	// 按优先级统计违约（通过SLA配置关联）
	byPriority, err := s.repo.ViolationsByPriority(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get violation priority stats: %w", err)
	}
	for priority, count := range byPriority {
		stats.ViolationsByPriority[priority] = count
	}

	// 按客户级别统计违约（查询失败沿用旧行为：静默跳过）
	if byTier, err := s.repo.ViolationsByTier(ctx); err == nil {
		for tier, count := range byTier {
			stats.ViolationsByTier[tier] = count
		}
	}

	// 计算合规率
	totalTickets, err := s.repo.CountTickets(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to count total tickets: %w", err)
	}

	if totalTickets > 0 {
		stats.ComplianceRate = float64(totalTickets-totalViolations) / float64(totalTickets) * 100
	} else {
		stats.ComplianceRate = 100.0
	}

	// 生成趋势数据（最近7天）
	trendData, err := s.getSLATrendData(ctx, 7)
	if err != nil {
		s.logger.Errorf("Failed to get SLA trend data: %v", err)
	} else {
		stats.TrendData = trendData
	}

	span.SetAttributes(
		attribute.Int("sla.stats.total_configs", stats.TotalConfigs),
		attribute.Int("sla.stats.total_violations", stats.TotalViolations),
		attribute.Float64("sla.stats.compliance_rate", stats.ComplianceRate),
	)

	return stats, nil
}

// getSLATrendData 获取SLA合规趋势数据
func (s *SLAService) getSLATrendData(ctx context.Context, days int) ([]SLAComplianceTrend, error) {
	trendData := make([]SLAComplianceTrend, 0, days)

	for i := days - 1; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")

		// 统计当天创建的工单数
		totalTickets, err := s.repo.CountTicketsByDate(ctx, dateStr)
		if err != nil {
			return nil, fmt.Errorf("failed to count tickets for date %s: %w", dateStr, err)
		}

		// 统计当天的违约数
		violations, err := s.repo.CountViolationsByDate(ctx, dateStr)
		if err != nil {
			return nil, fmt.Errorf("failed to count violations for date %s: %w", dateStr, err)
		}

		// 计算合规率
		complianceRate := 100.0
		if totalTickets > 0 {
			complianceRate = float64(totalTickets-violations) / float64(totalTickets) * 100
		}

		trendData = append(trendData, SLAComplianceTrend{
			Date:           dateStr,
			TotalTickets:   int(totalTickets),
			Violations:     int(violations),
			ComplianceRate: complianceRate,
		})
	}

	return trendData, nil
}

// normalizeTier 统一客户等级字符串
func normalizeTier(tier string) string {
	return strings.ToLower(strings.TrimSpace(tier))
}

// defaultWarning 计算默认预警阈值
func defaultWarning(threshold *int) int {
	if threshold == nil {
		return 80
	}
	val := *threshold
	if val < 50 {
		return 50
	}
	if val > 100 {
		return 100
	}
	return val
}

// joinTags 将标签切片合并为字符串
func joinTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	clean := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			clean = append(clean, tag)
		}
	}
	return strings.Join(clean, ",")
}

func slaRecordScopeContext(ctx context.Context, tenantID, workspaceID string) context.Context {
	currentTenant, currentWorkspace := tenantAndWorkspace(ctx)
	if currentTenant == "" {
		currentTenant = tenantID
	}
	if currentWorkspace == "" {
		currentWorkspace = workspaceID
	}
	return platformauth.ContextWithScope(ctx, currentTenant, currentWorkspace)
}

// resolveCustomerTier 根据工单的客户ID提取客户等级（复用客户优先级字段）
func (s *SLAService) resolveCustomerTier(ctx context.Context, customerUserID uint) string {
	if customerUserID == 0 {
		return ""
	}
	priority, err := s.repo.GetCustomerPriorityScoped(ctx, customerUserID)
	if err != nil {
		return ""
	}
	return normalizeTier(priority)
}

// RunMonitorOnce 执行一轮 SLA 违约扫描并返回错误（周期循环由 app/worker 驱动）。
func (s *SLAService) RunMonitorOnce(ctx context.Context) error {
	return s.monitorSLAViolations(ctx)
}

// monitorSLAViolations 监控SLA违约
func (s *SLAService) monitorSLAViolations(ctx context.Context) error {
	ctx, span := s.tracer.Start(ctx, "sla.monitor_violations")
	defer span.End()

	// 获取所有未关闭的工单
	tickets, err := s.repo.ListActiveTickets(ctx)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get active tickets: %w", err)
	}

	violationCount := 0
	for _, ticket := range tickets {
		violation, err := s.CheckSLAViolation(ctx, &ticket)
		if err != nil {
			s.logger.Errorf("Failed to check SLA violation for ticket %d: %v", ticket.ID, err)
			continue
		}
		if violation != nil {
			violationCount++
		}
	}

	s.logger.Infof("SLA monitoring completed: checked %d tickets, found %d violations", len(tickets), violationCount)
	span.SetAttributes(
		attribute.Int("sla.monitor.tickets_checked", len(tickets)),
		attribute.Int("sla.monitor.violations_found", violationCount),
	)

	return nil
}
