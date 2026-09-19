package application

// Service 编排与错误包裹语义（自 services/shift_service*_test.go 下沉）。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

type stubRepo struct {
	agentExists bool
	agentErr    error
	shift       *models.ShiftSchedule
	shifts      []models.ShiftSchedule
	total       int64
	agg         *ShiftAggregates

	createErr, countErr, listErr, getErr, saveErr, deleteErr, aggErr error

	created *models.ShiftSchedule
	saved   *models.ShiftSchedule
	deleted uint
}

func (r *stubRepo) AgentExistsByUserID(ctx context.Context, userID uint) (bool, error) {
	return r.agentExists, r.agentErr
}

func (r *stubRepo) CreateShift(ctx context.Context, shift *models.ShiftSchedule) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = shift
	return nil
}

func (r *stubRepo) CountShifts(ctx context.Context, req *ShiftListRequest) (int64, error) {
	return r.total, r.countErr
}

func (r *stubRepo) ListShifts(ctx context.Context, req *ShiftListRequest) ([]models.ShiftSchedule, error) {
	return r.shifts, r.listErr
}

func (r *stubRepo) GetShift(ctx context.Context, id uint) (*models.ShiftSchedule, error) {
	return r.shift, r.getErr
}

func (r *stubRepo) SaveShift(ctx context.Context, shift *models.ShiftSchedule) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = shift
	return nil
}

func (r *stubRepo) DeleteShift(ctx context.Context, id uint) error {
	r.deleted = id
	return r.deleteErr
}

func (r *stubRepo) ShiftAggregates(ctx context.Context, now time.Time) (*ShiftAggregates, error) {
	return r.agg, r.aggErr
}

func TestServiceCreateShift(t *testing.T) {
	start := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)

	// end <= start
	if _, err := NewService(&stubRepo{}).CreateShift(context.Background(),
		&ShiftCreateRequest{AgentID: 1, ShiftType: "morning", StartTime: start.Add(time.Hour), EndTime: start}); !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("end<=start err = %v", err)
	}

	// agent 校验错误包裹
	boom := errors.New("boom")
	if _, err := NewService(&stubRepo{agentErr: boom}).CreateShift(context.Background(),
		&ShiftCreateRequest{AgentID: 1, ShiftType: "morning", StartTime: start, EndTime: start.Add(time.Hour)}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to validate agent") {
		t.Fatalf("agent validate err = %v", err)
	}

	// agent 不存在
	if _, err := NewService(&stubRepo{}).CreateShift(context.Background(),
		&ShiftCreateRequest{AgentID: 1, ShiftType: "morning", StartTime: start, EndTime: start.Add(time.Hour)}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("agent missing err = %v", err)
	}

	// 成功：默认 scheduled、Status 覆盖、Date 截断
	repo := &stubRepo{agentExists: true}
	shift, err := NewService(repo).CreateShift(context.Background(),
		&ShiftCreateRequest{AgentID: 7, ShiftType: "morning", StartTime: start, EndTime: start.Add(8 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateShift: %v", err)
	}
	if shift.Status != "scheduled" || shift.AgentID != 7 || !shift.Date.Equal(start.Truncate(24*time.Hour)) {
		t.Fatalf("unexpected shift: %+v", shift)
	}

	// 显式 Status 覆盖默认值
	if _, err := NewService(repo).CreateShift(context.Background(),
		&ShiftCreateRequest{AgentID: 7, ShiftType: "morning", StartTime: start, EndTime: start.Add(8 * time.Hour), Status: "active"}); err != nil || repo.created.Status != "active" {
		t.Fatalf("status override: %+v, %v", repo.created, err)
	}

	// create 错误包裹
	if _, err := NewService(&stubRepo{agentExists: true, createErr: boom}).CreateShift(context.Background(),
		&ShiftCreateRequest{AgentID: 7, ShiftType: "morning", StartTime: start, EndTime: start.Add(time.Hour)}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to create shift") {
		t.Fatalf("create err = %v", err)
	}
}

func TestServiceListShifts(t *testing.T) {
	boom := errors.New("boom")
	want := []models.ShiftSchedule{{ID: 1}}

	if _, _, err := NewService(&stubRepo{countErr: boom}).ListShifts(context.Background(), &ShiftListRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to count shifts") {
		t.Fatalf("count err = %v", err)
	}
	if _, _, err := NewService(&stubRepo{listErr: boom}).ListShifts(context.Background(), &ShiftListRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to list shifts") {
		t.Fatalf("list err = %v", err)
	}
	shifts, total, err := NewService(&stubRepo{shifts: want, total: 1}).ListShifts(context.Background(), &ShiftListRequest{})
	if err != nil || total != 1 || len(shifts) != 1 {
		t.Fatalf("ListShifts() = %+v, %d, %v", shifts, total, err)
	}
}

func TestServiceUpdateShift(t *testing.T) {
	boom := errors.New("boom")
	start := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	existing := &models.ShiftSchedule{ID: 1, ShiftType: "morning", StartTime: start, EndTime: start.Add(4 * time.Hour), Status: "scheduled"}

	// not found 透传
	if _, err := NewService(&stubRepo{getErr: ErrShiftNotFound}).UpdateShift(context.Background(), 9, &ShiftUpdateRequest{}); !errors.Is(err, ErrShiftNotFound) {
		t.Fatalf("not found err = %v", err)
	}
	// 其他 get 错误包裹
	if _, err := NewService(&stubRepo{getErr: boom}).UpdateShift(context.Background(), 9, &ShiftUpdateRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to get shift") {
		t.Fatalf("get err = %v", err)
	}
	// end <= start
	if _, err := NewService(&stubRepo{shift: existing}).UpdateShift(context.Background(), 1,
		&ShiftUpdateRequest{EndTime: &start}); !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("end<=start err = %v", err)
	}
	// save 错误包裹（fresh shift：上面 end<=start 用例会 mutate 共享对象）
	fresh := &models.ShiftSchedule{ID: 1, StartTime: start, EndTime: start.Add(4 * time.Hour)}
	if _, err := NewService(&stubRepo{shift: fresh, saveErr: boom}).UpdateShift(context.Background(), 1,
		&ShiftUpdateRequest{Status: strPtr("active")}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to update shift") {
		t.Fatalf("save err = %v", err)
	}

	// 全字段 patch（StartTime 变更带动 Date 重算）
	repo := &stubRepo{shift: existing}
	newStart := start.Add(48 * time.Hour)
	newEnd := newStart.Add(2 * time.Hour)
	updated, err := NewService(repo).UpdateShift(context.Background(), 1, &ShiftUpdateRequest{
		ShiftType: strPtr("evening"),
		StartTime: &newStart,
		EndTime:   &newEnd,
		Status:    strPtr("active"),
	})
	if err != nil {
		t.Fatalf("UpdateShift: %v", err)
	}
	if updated.ShiftType != "evening" || updated.Status != "active" || !updated.Date.Equal(newStart.Truncate(24*time.Hour)) {
		t.Fatalf("unexpected patch: %+v", updated)
	}
	if repo.saved != updated {
		t.Fatal("expected saved shift to be the updated one")
	}
}

func TestServiceDeleteShift(t *testing.T) {
	repo := &stubRepo{deleteErr: ErrShiftNotFound}
	if err := NewService(repo).DeleteShift(context.Background(), 5); !errors.Is(err, ErrShiftNotFound) {
		t.Fatalf("DeleteShift() err = %v", err)
	}
	if repo.deleted != 5 {
		t.Fatalf("deleted id = %d", repo.deleted)
	}
}

func TestServiceGetShiftStats(t *testing.T) {
	boom := errors.New("boom")
	if _, err := NewService(&stubRepo{aggErr: boom}).GetShiftStats(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("agg err = %v", err)
	}

	agg := &ShiftAggregates{
		Total: 3, Upcoming: 1, TodayActive: 2,
		ByType:   map[string]int{"morning": 2, "night": 1},
		ByStatus: map[string]int{"scheduled": 3},
	}
	stats, err := NewService(&stubRepo{agg: agg}).GetShiftStats(context.Background())
	if err != nil {
		t.Fatalf("GetShiftStats: %v", err)
	}
	if stats.Total != 3 || stats.Upcoming != 1 || stats.TodayActive != 2 || stats.ByType["morning"] != 2 || stats.ByStatus["scheduled"] != 3 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func strPtr(s string) *string { return &s }
