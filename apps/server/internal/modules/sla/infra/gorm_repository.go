package infra

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	slapp "servify/apps/server/internal/modules/sla/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// GormRepository 基于 GORM 的 SLA Repository 实现。
type GormRepository struct {
	db *gorm.DB
}

var _ slapp.Repository = (*GormRepository)(nil)

// NewGormRepository 创建 GORM 仓储实例。
func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// tenantAndWorkspace 从上下文读取 scope。
func tenantAndWorkspace(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}

// applyScopeFilter 按 scope 加租户/工作空间过滤（空值不加条件）。
func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID, workspaceID := tenantAndWorkspace(ctx)
	if tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

// scopeAwareSLAViolationPreloads 关联预加载带 scope 过滤。
func scopeAwareSLAViolationPreloads(db *gorm.DB, ctx context.Context) *gorm.DB {
	return db.
		Preload("Ticket", func(tx *gorm.DB) *gorm.DB {
			return applyScopeFilter(tx, ctx)
		}).
		Preload("SLAConfig", func(tx *gorm.DB) *gorm.DB {
			return applyScopeFilter(tx, ctx)
		})
}

// ---- sla_configs ----

func (r *GormRepository) CreateConfig(ctx context.Context, config *models.SLAConfig) error {
	return r.db.WithContext(ctx).Create(config).Error
}

func (r *GormRepository) SaveConfig(ctx context.Context, config *models.SLAConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

func (r *GormRepository) GetConfigScoped(ctx context.Context, id uint) (*models.SLAConfig, error) {
	var config models.SLAConfig
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&config, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, slapp.ErrSLAConfigNotFound
		}
		return nil, err
	}
	return &config, nil
}

func (r *GormRepository) DeleteConfig(ctx context.Context, id uint) (int64, error) {
	result := applyScopeFilter(r.db.WithContext(ctx), ctx).Delete(&models.SLAConfig{}, id)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *GormRepository) CountConfigsFiltered(ctx context.Context, req *slapp.SLAConfigListRequest) (int64, error) {
	var total int64
	if err := slaConfigFilterQuery(r.db, ctx, req).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *GormRepository) ListConfigs(ctx context.Context, req *slapp.SLAConfigListRequest) ([]models.SLAConfig, error) {
	query := slaConfigFilterQuery(r.db, ctx, req).Order(slaOrder(req.SortBy, req.SortOrder))
	if req.PageSize > 0 {
		query = query.Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize)
	}
	var configs []models.SLAConfig
	if err := query.Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// slaConfigFilterQuery 构建配置列表的 scope+筛选查询（count 与 find 共用同一
// 筛选集，排序与分页只在 find 侧追加）。
func slaConfigFilterQuery(db *gorm.DB, ctx context.Context, req *slapp.SLAConfigListRequest) *gorm.DB {
	query := applyScopeFilter(db.WithContext(ctx).Model(&models.SLAConfig{}), ctx)
	if len(req.Priority) > 0 {
		query = query.Where("priority IN ?", req.Priority)
	}
	if len(req.CustomerTier) > 0 {
		query = query.Where("(customer_tier = '' OR customer_tier IN ?)", req.CustomerTier)
	}
	if req.Active != nil {
		query = query.Where("active = ?", *req.Active)
	}
	return query
}

// slaOrder 归一化排序字段与方向（与原始内联逻辑一致）。
func slaOrder(sortBy, sortOrder string) string {
	sortField := sortBy
	if sortField == "" {
		sortField = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}
	return fmt.Sprintf("%s %s", sortField, sortOrder)
}

func (r *GormRepository) FindConfigByPriorityTier(ctx context.Context, priority, tier string) (*models.SLAConfig, error) {
	var existingConfig models.SLAConfig
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("priority = ? AND customer_tier = ?", priority, tier).
		First(&existingConfig).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &existingConfig, nil
}

func (r *GormRepository) FindConfigByPriorityExceptID(ctx context.Context, priority string, excludeID uint) (*models.SLAConfig, error) {
	var existingConfig models.SLAConfig
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("priority = ? AND id != ?", priority, excludeID).
		First(&existingConfig).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &existingConfig, nil
}

func (r *GormRepository) FindConfigByPriorityTierExceptID(ctx context.Context, priority, tier string, excludeID uint) (*models.SLAConfig, error) {
	var conflict models.SLAConfig
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("priority = ? AND customer_tier = ? AND id != ?", priority, tier, excludeID).
		First(&conflict).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &conflict, nil
}

func (r *GormRepository) FindConfigByPriorityTierActive(ctx context.Context, priority, tier string) (*models.SLAConfig, error) {
	var config models.SLAConfig
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("priority = ? AND customer_tier = ? AND active = true", priority, tier).
		First(&config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

func (r *GormRepository) FindDefaultConfigByPriorityActive(ctx context.Context, priority string) (*models.SLAConfig, error) {
	var config models.SLAConfig
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("priority = ? AND (customer_tier = '' OR customer_tier IS NULL) AND active = true", priority).
		First(&config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

func (r *GormRepository) CountConfigs(ctx context.Context) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAConfig{}), ctx).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) CountActiveConfigs(ctx context.Context) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAConfig{}), ctx).
		Where("active = true").
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ---- tickets ----

func (r *GormRepository) GetTicketScoped(ctx context.Context, id uint) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&ticket, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, slapp.ErrTicketNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

func (r *GormRepository) CountTickets(ctx context.Context) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) CountTicketsByDate(ctx context.Context, date string) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).
		Where("DATE(created_at) = ?", date).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) ListActiveTickets(ctx context.Context) ([]models.Ticket, error) {
	var tickets []models.Ticket
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("status NOT IN ?", []string{"resolved", "closed"}).
		Find(&tickets).Error; err != nil {
		return nil, err
	}
	return tickets, nil
}

// ---- customers ----

func (r *GormRepository) GetCustomerPriorityScoped(ctx context.Context, userID uint) (string, error) {
	var customer models.Customer
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("user_id = ?", userID).
		First(&customer).Error; err != nil {
		return "", err
	}
	return customer.Priority, nil
}

// ---- sla_violations ----

func (r *GormRepository) CreateViolation(ctx context.Context, violation *models.SLAViolation) error {
	return r.db.WithContext(ctx).Create(violation).Error
}

func (r *GormRepository) GetViolationScoped(ctx context.Context, id uint) (*models.SLAViolation, error) {
	var createdViolation models.SLAViolation
	if err := applyScopeFilter(scopeAwareSLAViolationPreloads(r.db.WithContext(ctx), ctx), ctx).
		First(&createdViolation, id).Error; err != nil {
		return nil, err
	}
	return &createdViolation, nil
}

func (r *GormRepository) FindViolationByTicketConfigType(ctx context.Context, ticketID, configID uint, violationType string) (*models.SLAViolation, error) {
	var existingViolation models.SLAViolation
	if err := applyScopeFilter(scopeAwareSLAViolationPreloads(r.db.WithContext(ctx), ctx), ctx).
		Where("ticket_id = ? AND sla_config_id = ? AND violation_type = ?", ticketID, configID, violationType).
		First(&existingViolation).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &existingViolation, nil
}

func (r *GormRepository) CountViolationsFiltered(ctx context.Context, req *slapp.SLAViolationListRequest) (int64, error) {
	var total int64
	if err := slaViolationFilterQuery(r.db, ctx, req).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *GormRepository) ListViolations(ctx context.Context, req *slapp.SLAViolationListRequest) ([]models.SLAViolation, error) {
	query := slaViolationFilterQuery(r.db, ctx, req).Order(slaOrder(req.SortBy, req.SortOrder))
	if req.PageSize > 0 {
		query = query.Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize)
	}
	var violations []models.SLAViolation
	if err := query.Find(&violations).Error; err != nil {
		return nil, err
	}
	return violations, nil
}

// slaViolationFilterQuery 构建违约列表的 scope+预加载+筛选查询（count 与 find
// 共用同一筛选集，排序与分页只在 find 侧追加）。
func slaViolationFilterQuery(db *gorm.DB, ctx context.Context, req *slapp.SLAViolationListRequest) *gorm.DB {
	query := applyScopeFilter(scopeAwareSLAViolationPreloads(db.WithContext(ctx).Model(&models.SLAViolation{}), ctx), ctx)
	if req.TicketID != nil {
		query = query.Where("ticket_id = ?", *req.TicketID)
	}
	if req.SLAConfigID != nil {
		query = query.Where("sla_config_id = ?", *req.SLAConfigID)
	}
	if len(req.ViolationType) > 0 {
		query = query.Where("violation_type IN ?", req.ViolationType)
	}
	if req.Resolved != nil {
		query = query.Where("resolved = ?", *req.Resolved)
	}
	if req.DateFrom != nil {
		query = query.Where("violated_at >= ?", *req.DateFrom)
	}
	if req.DateTo != nil {
		query = query.Where("violated_at <= ?", *req.DateTo)
	}
	return query
}

func (r *GormRepository) ResolveViolation(ctx context.Context, id uint) (int64, error) {
	result := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"resolved":    true,
			"resolved_at": time.Now(),
			"updated_at":  time.Now(),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *GormRepository) ResolveViolationsByTicket(ctx context.Context, ticketID uint, violationTypes []string) (int64, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).
		Where("ticket_id = ? AND resolved = false", ticketID)
	if len(violationTypes) > 0 {
		query = query.Where("violation_type IN ?", violationTypes)
	}

	result := query.Updates(map[string]interface{}{
		"resolved":    true,
		"resolved_at": time.Now(),
		"updated_at":  time.Now(),
	})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *GormRepository) CountViolations(ctx context.Context) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) CountViolationsByConfig(ctx context.Context, configID uint) (int64, error) {
	var violationCount int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).
		Where("sla_config_id = ?", configID).
		Count(&violationCount).Error; err != nil {
		return 0, err
	}
	return violationCount, nil
}

func (r *GormRepository) CountUnresolvedViolations(ctx context.Context) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).
		Where("resolved = false").
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) CountViolationsByDate(ctx context.Context, date string) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).
		Where("DATE(violated_at) = ?", date).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) ViolationsByType(ctx context.Context) (map[string]int, error) {
	var stats []struct {
		ViolationType string `json:"violation_type"`
		Count         int    `json:"count"`
	}
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SLAViolation{}), ctx).
		Select("violation_type, COUNT(*) as count").
		Group("violation_type").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int)
	for _, stat := range stats {
		out[stat.ViolationType] = stat.Count
	}
	return out, nil
}

func (r *GormRepository) ViolationsByPriority(ctx context.Context) (map[string]int, error) {
	var stats []struct {
		Priority string `json:"priority"`
		Count    int    `json:"count"`
	}
	query := r.db.WithContext(ctx).Table("sla_violations").
		Select("sla_configs.priority, COUNT(*) as count").
		Joins("JOIN sla_configs ON sla_violations.sla_config_id = sla_configs.id AND sla_configs.tenant_id = sla_violations.tenant_id AND sla_configs.workspace_id = sla_violations.workspace_id")
	if tenantID, workspaceID := tenantAndWorkspace(ctx); tenantID != "" || workspaceID != "" {
		if tenantID != "" {
			query = query.Where("sla_violations.tenant_id = ?", tenantID)
		}
		if workspaceID != "" {
			query = query.Where("sla_violations.workspace_id = ?", workspaceID)
		}
	}
	if err := query.Group("sla_configs.priority").Scan(&stats).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int)
	for _, stat := range stats {
		out[stat.Priority] = stat.Count
	}
	return out, nil
}

func (r *GormRepository) ViolationsByTier(ctx context.Context) (map[string]int, error) {
	var stats []struct {
		Tier  string `json:"customer_tier"`
		Count int    `json:"count"`
	}
	query := r.db.WithContext(ctx).Table("sla_violations").
		Select("COALESCE(sla_configs.customer_tier, '') as customer_tier, COUNT(*) as count").
		Joins("JOIN sla_configs ON sla_violations.sla_config_id = sla_configs.id AND sla_configs.tenant_id = sla_violations.tenant_id AND sla_configs.workspace_id = sla_violations.workspace_id")
	if tenantID, workspaceID := tenantAndWorkspace(ctx); tenantID != "" || workspaceID != "" {
		if tenantID != "" {
			query = query.Where("sla_violations.tenant_id = ?", tenantID)
		}
		if workspaceID != "" {
			query = query.Where("sla_violations.workspace_id = ?", workspaceID)
		}
	}
	if err := query.Group("sla_configs.customer_tier").Scan(&stats).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int)
	for _, stat := range stats {
		out[stat.Tier] = stat.Count
	}
	return out, nil
}
