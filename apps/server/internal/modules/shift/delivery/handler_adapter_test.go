package delivery

// HandlerServiceAdapter 纯转发对账 + 错误传播。

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	shiftapp "servify/apps/server/internal/modules/shift/application"
)

type adapterStubRepo struct {
	err         error
	agentExists bool
}

func (r *adapterStubRepo) AgentExistsByUserID(ctx context.Context, userID uint) (bool, error) {
	return r.agentExists, r.err
}

func (r *adapterStubRepo) CreateShift(ctx context.Context, shift *models.ShiftSchedule) error {
	return r.err
}

func (r *adapterStubRepo) CountShifts(ctx context.Context, req *shiftapp.ShiftListRequest) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) ListShifts(ctx context.Context, req *shiftapp.ShiftListRequest) ([]models.ShiftSchedule, error) {
	return nil, r.err
}

func (r *adapterStubRepo) GetShift(ctx context.Context, id uint) (*models.ShiftSchedule, error) {
	return nil, r.err
}

func (r *adapterStubRepo) SaveShift(ctx context.Context, shift *models.ShiftSchedule) error {
	return r.err
}

func (r *adapterStubRepo) DeleteShift(ctx context.Context, id uint) error {
	return r.err
}

func (r *adapterStubRepo) ShiftAggregates(ctx context.Context, now time.Time) (*shiftapp.ShiftAggregates, error) {
	return nil, r.err
}

func TestHandlerServiceAdapter(t *testing.T) {
	ctx := context.Background()
	want := errors.New("boom")
	repo := &adapterStubRepo{err: want}
	svc := shiftapp.NewService(repo)
	var handler HandlerService = NewHandlerServiceAdapter(svc)

	start := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)

	if _, err := handler.CreateShift(ctx, &ShiftCreateRequest{AgentID: 1, StartTime: start, EndTime: start.Add(time.Hour)}); !errors.Is(err, want) {
		t.Fatalf("CreateShift() err = %v, want %v", err, want)
	}
	if _, _, err := handler.ListShifts(ctx, &ShiftListRequest{}); !errors.Is(err, want) {
		t.Fatalf("ListShifts() err = %v, want %v", err, want)
	}
	if _, err := handler.UpdateShift(ctx, 1, &ShiftUpdateRequest{}); !errors.Is(err, want) {
		t.Fatalf("UpdateShift() err = %v, want %v", err, want)
	}
	if err := handler.DeleteShift(ctx, 1); !errors.Is(err, want) {
		t.Fatalf("DeleteShift() err = %v, want %v", err, want)
	}
	if _, err := handler.GetShiftStats(ctx); !errors.Is(err, want) {
		t.Fatalf("GetShiftStats() err = %v, want %v", err, want)
	}

	// 无错误时纯转发
	repo.err = nil
	repo.agentExists = true
	shift, err := handler.CreateShift(ctx, &ShiftCreateRequest{AgentID: 1, StartTime: start, EndTime: start.Add(time.Hour)})
	if err != nil || shift == nil || shift.Status != "scheduled" {
		t.Fatalf("CreateShift() = %+v, %v", shift, err)
	}
	shifts, total, err := handler.ListShifts(ctx, &ShiftListRequest{})
	if err != nil || total != 0 || len(shifts) != 0 {
		t.Fatalf("ListShifts() = %+v, %d, %v", shifts, total, err)
	}
}
