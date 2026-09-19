package infra

// shift 模块基础设施层：班次 GORM 仓储（自 services/shift_service.go 迁入）。
// 统计聚合的错误包裹消息保持 services 原文案，供上层文本断言对账。

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
	shiftapp "servify/apps/server/internal/modules/shift/application"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// applyScopeFilter 按上下文租户/工作区过滤（与模块内其他仓库一致）。
func applyScopeFilter(q *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	return q
}

// scopeAwareShiftPreloads 预加载 Agent 时保持租户×工作区 JOIN 级隔离。
func scopeAwareShiftPreloads(db *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	return db.Preload("Agent", func(tx *gorm.DB) *gorm.DB {
		if tenantID == "" && workspaceID == "" {
			return tx
		}
		tx = tx.Joins("JOIN agents ON agents.user_id = users.id")
		if tenantID != "" {
			tx = tx.Where("agents.tenant_id = ?", tenantID)
		}
		if workspaceID != "" {
			tx = tx.Where("agents.workspace_id = ?", workspaceID)
		}
		return tx
	})
}

func (r *GormRepository) AgentExistsByUserID(ctx context.Context, userID uint) (bool, error) {
	var agent models.Agent
	err := applyScopeFilter(r.db.WithContext(ctx), ctx).Where("user_id = ?", userID).First(&agent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *GormRepository) CreateShift(ctx context.Context, shift *models.ShiftSchedule) error {
	return r.db.WithContext(ctx).Create(shift).Error
}

// shiftQuery 组装 scope、preload 与列表过滤条件。
func (r *GormRepository) shiftQuery(ctx context.Context, req *shiftapp.ShiftListRequest) *gorm.DB {
	query := applyScopeFilter(scopeAwareShiftPreloads(r.db.WithContext(ctx).Model(&models.ShiftSchedule{}), ctx), ctx)

	if req.AgentID != nil {
		query = query.Where("agent_id = ?", *req.AgentID)
	}
	if len(req.ShiftType) > 0 {
		query = query.Where("shift_type IN ?", req.ShiftType)
	}
	if len(req.Status) > 0 {
		query = query.Where("status IN ?", req.Status)
	}
	if req.DateFrom != nil {
		query = query.Where("date >= ?", req.DateFrom)
	}
	if req.DateTo != nil {
		query = query.Where("date <= ?", req.DateTo)
	}
	return query
}

func (r *GormRepository) CountShifts(ctx context.Context, req *shiftapp.ShiftListRequest) (int64, error) {
	var total int64
	if err := r.shiftQuery(ctx, req).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *GormRepository) ListShifts(ctx context.Context, req *shiftapp.ShiftListRequest) ([]models.ShiftSchedule, error) {
	sortField := req.SortBy
	if sortField == "" {
		sortField = "start_time"
	}
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "asc"
	}

	query := r.shiftQuery(ctx, req).Order(fmt.Sprintf("%s %s", sortField, sortOrder))

	if req.PageSize > 0 {
		offset := (req.Page - 1) * req.PageSize
		query = query.Offset(offset).Limit(req.PageSize)
	}

	var shifts []models.ShiftSchedule
	if err := query.Find(&shifts).Error; err != nil {
		return nil, err
	}
	return shifts, nil
}

func (r *GormRepository) GetShift(ctx context.Context, id uint) (*models.ShiftSchedule, error) {
	var shift models.ShiftSchedule
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&shift, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, shiftapp.ErrShiftNotFound
		}
		return nil, err
	}
	return &shift, nil
}

func (r *GormRepository) SaveShift(ctx context.Context, shift *models.ShiftSchedule) error {
	return r.db.WithContext(ctx).Save(shift).Error
}

func (r *GormRepository) DeleteShift(ctx context.Context, id uint) error {
	result := applyScopeFilter(r.db.WithContext(ctx), ctx).Delete(&models.ShiftSchedule{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return shiftapp.ErrShiftNotFound
	}
	return nil
}

func (r *GormRepository) ShiftAggregates(ctx context.Context, now time.Time) (*shiftapp.ShiftAggregates, error) {
	agg := &shiftapp.ShiftAggregates{
		ByType:   make(map[string]int),
		ByStatus: make(map[string]int),
	}

	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.ShiftSchedule{}), ctx).Count(&agg.Total).Error; err != nil {
		return nil, fmt.Errorf("failed to count shifts: %w", err)
	}

	var byType []struct {
		ShiftType string
		Count     int
	}
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.ShiftSchedule{}), ctx).
		Select("shift_type, COUNT(*) as count").
		Group("shift_type").Scan(&byType).Error; err != nil {
		return nil, fmt.Errorf("failed to aggregate by shift_type: %w", err)
	}
	for _, item := range byType {
		agg.ByType[item.ShiftType] = item.Count
	}

	var byStatus []struct {
		Status string
		Count  int
	}
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.ShiftSchedule{}), ctx).
		Select("status, COUNT(*) as count").
		Group("status").Scan(&byStatus).Error; err != nil {
		return nil, fmt.Errorf("failed to aggregate by status: %w", err)
	}
	for _, item := range byStatus {
		agg.ByStatus[item.Status] = item.Count
	}

	// upcoming: start in the future
	var upcoming int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.ShiftSchedule{}), ctx).
		Where("start_time > ?", now).
		Count(&upcoming).Error; err != nil {
		return nil, fmt.Errorf("failed to count upcoming shifts: %w", err)
	}
	agg.Upcoming = upcoming

	// today active: overlapping today
	dayStart := now.Truncate(24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)
	var todayActive int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.ShiftSchedule{}), ctx).
		Where("start_time < ? AND end_time > ?", dayEnd, dayStart).
		Count(&todayActive).Error; err != nil {
		return nil, fmt.Errorf("failed to count today active shifts: %w", err)
	}
	agg.TodayActive = todayActive

	return agg, nil
}
