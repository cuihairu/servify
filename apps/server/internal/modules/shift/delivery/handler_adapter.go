package delivery

// shift 模块交付层：handler 侧契约与纯转发适配器。

import (
	"context"

	"servify/apps/server/internal/models"
	shiftapp "servify/apps/server/internal/modules/shift/application"
)

// HandlerService 班次管理处理器契约。
type HandlerService interface {
	CreateShift(ctx context.Context, req *ShiftCreateRequest) (*models.ShiftSchedule, error)
	ListShifts(ctx context.Context, req *ShiftListRequest) ([]models.ShiftSchedule, int64, error)
	UpdateShift(ctx context.Context, id uint, req *ShiftUpdateRequest) (*models.ShiftSchedule, error)
	DeleteShift(ctx context.Context, id uint) error
	GetShiftStats(ctx context.Context) (*ShiftStatsResponse, error)
}

// HandlerServiceAdapter 将模块服务适配为 HandlerService 契约（纯转发）。
type HandlerServiceAdapter struct {
	svc *shiftapp.Service
}

// NewHandlerServiceAdapter creates a new adapter.
func NewHandlerServiceAdapter(svc *shiftapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{svc: svc}
}

func (a *HandlerServiceAdapter) CreateShift(ctx context.Context, req *ShiftCreateRequest) (*models.ShiftSchedule, error) {
	return a.svc.CreateShift(ctx, req)
}

func (a *HandlerServiceAdapter) ListShifts(ctx context.Context, req *ShiftListRequest) ([]models.ShiftSchedule, int64, error) {
	return a.svc.ListShifts(ctx, req)
}

func (a *HandlerServiceAdapter) UpdateShift(ctx context.Context, id uint, req *ShiftUpdateRequest) (*models.ShiftSchedule, error) {
	return a.svc.UpdateShift(ctx, id, req)
}

func (a *HandlerServiceAdapter) DeleteShift(ctx context.Context, id uint) error {
	return a.svc.DeleteShift(ctx, id)
}

func (a *HandlerServiceAdapter) GetShiftStats(ctx context.Context) (*ShiftStatsResponse, error) {
	return a.svc.GetShiftStats(ctx)
}
