package application

// shift 模块应用层：班次管理编排（自 services/shift_service.go 迁入）。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

// 哨兵错误（消息保持原字符串，handler 按文本匹配 HTTP 状态码）。
var (
	ErrShiftNotFound    = errors.New("shift not found")
	ErrAgentNotFound    = errors.New("agent not found")
	ErrInvalidTimeRange = errors.New("end_time must be after start_time")
)

// ShiftCreateRequest 创建班次请求
type ShiftCreateRequest struct {
	AgentID   uint      `json:"agent_id" binding:"required"`
	ShiftType string    `json:"shift_type" binding:"required"` // morning, afternoon, evening, night
	StartTime time.Time `json:"start_time" binding:"required"`
	EndTime   time.Time `json:"end_time" binding:"required"`
	Status    string    `json:"status"` // scheduled, active, completed, cancelled
}

// ShiftListRequest 班次列表请求
type ShiftListRequest struct {
	Page      int        `form:"page,default=1"`
	PageSize  int        `form:"page_size,default=20"`
	AgentID   *uint      `form:"agent_id"`
	ShiftType []string   `form:"shift_type"`
	Status    []string   `form:"status"`
	DateFrom  *time.Time `form:"date_from"`
	DateTo    *time.Time `form:"date_to"`
	SortBy    string     `form:"sort_by,default=start_time"`
	SortOrder string     `form:"sort_order,default=asc"`
}

// ShiftUpdateRequest 更新班次请求
type ShiftUpdateRequest struct {
	ShiftType *string    `json:"shift_type"`
	StartTime *time.Time `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
	Status    *string    `json:"status"`
}

// ShiftStatsResponse 班次统计响应
type ShiftStatsResponse struct {
	Total       int            `json:"total"`
	ByType      map[string]int `json:"by_type"`
	ByStatus    map[string]int `json:"by_status"`
	Upcoming    int            `json:"upcoming"`
	TodayActive int            `json:"today_active"`
}

// ShiftAggregates 统计聚合的仓储侧原始结果。
type ShiftAggregates struct {
	Total       int64
	ByType      map[string]int
	ByStatus    map[string]int
	Upcoming    int64
	TodayActive int64
}

// Repository 班次持久化契约。
type Repository interface {
	AgentExistsByUserID(ctx context.Context, userID uint) (bool, error)
	CreateShift(ctx context.Context, shift *models.ShiftSchedule) error
	CountShifts(ctx context.Context, req *ShiftListRequest) (int64, error)
	ListShifts(ctx context.Context, req *ShiftListRequest) ([]models.ShiftSchedule, error)
	GetShift(ctx context.Context, id uint) (*models.ShiftSchedule, error)
	SaveShift(ctx context.Context, shift *models.ShiftSchedule) error
	DeleteShift(ctx context.Context, id uint) error
	ShiftAggregates(ctx context.Context, now time.Time) (*ShiftAggregates, error)
}

// Service 班次管理服务
type Service struct {
	repo Repository
}

// NewService creates a new shift module service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateShift 创建班次
func (s *Service) CreateShift(ctx context.Context, req *ShiftCreateRequest) (*models.ShiftSchedule, error) {
	// basic validation: end > start
	if !req.EndTime.After(req.StartTime) {
		return nil, ErrInvalidTimeRange
	}

	exists, err := s.repo.AgentExistsByUserID(ctx, req.AgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate agent: %w", err)
	}
	if !exists {
		return nil, ErrAgentNotFound
	}

	shift := &models.ShiftSchedule{
		AgentID:   req.AgentID,
		ShiftType: req.ShiftType,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Status:    "scheduled",
		Date:      req.StartTime.Truncate(24 * time.Hour),
	}
	shift.TenantID, shift.WorkspaceID = tenantAndWorkspace(ctx)
	if req.Status != "" {
		shift.Status = req.Status
	}

	if err := s.repo.CreateShift(ctx, shift); err != nil {
		return nil, fmt.Errorf("failed to create shift: %w", err)
	}

	return shift, nil
}

// ListShifts 获取班次列表
func (s *Service) ListShifts(ctx context.Context, req *ShiftListRequest) ([]models.ShiftSchedule, int64, error) {
	total, err := s.repo.CountShifts(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count shifts: %w", err)
	}

	shifts, err := s.repo.ListShifts(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list shifts: %w", err)
	}

	return shifts, total, nil
}

// UpdateShift 更新班次
func (s *Service) UpdateShift(ctx context.Context, id uint, req *ShiftUpdateRequest) (*models.ShiftSchedule, error) {
	shift, err := s.repo.GetShift(ctx, id)
	if err != nil {
		if errors.Is(err, ErrShiftNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to get shift: %w", err)
	}

	if req.ShiftType != nil {
		shift.ShiftType = *req.ShiftType
	}
	if req.StartTime != nil {
		shift.StartTime = *req.StartTime
		shift.Date = req.StartTime.Truncate(24 * time.Hour)
	}
	if req.EndTime != nil {
		shift.EndTime = *req.EndTime
	}
	if req.Status != nil {
		shift.Status = *req.Status
	}

	if !shift.EndTime.After(shift.StartTime) {
		return nil, ErrInvalidTimeRange
	}

	if err := s.repo.SaveShift(ctx, shift); err != nil {
		return nil, fmt.Errorf("failed to update shift: %w", err)
	}

	return shift, nil
}

// DeleteShift 删除班次
func (s *Service) DeleteShift(ctx context.Context, id uint) error {
	return s.repo.DeleteShift(ctx, id)
}

// GetShiftStats 获取班次统计
func (s *Service) GetShiftStats(ctx context.Context) (*ShiftStatsResponse, error) {
	agg, err := s.repo.ShiftAggregates(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	return &ShiftStatsResponse{
		Total:       int(agg.Total),
		ByType:      agg.ByType,
		ByStatus:    agg.ByStatus,
		Upcoming:    int(agg.Upcoming),
		TodayActive: int(agg.TodayActive),
	}, nil
}

// tenantAndWorkspace 从上下文取租户/工作区。
func tenantAndWorkspace(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}
