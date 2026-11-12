package infra

import (
	"testing"
	"time"

	"servify/apps/server/internal/models"
	shiftapp "servify/apps/server/internal/modules/shift/application"
)

// 排序字段白名单回归（审核 P0-1 同类）：shift 列表 sort_by 由查询参数直达
// ListShifts 的 Order 拼接，未白名单任意串即盲注面。修复后非白名单值一律
// 回退 start_time（与 form tag default 一致），方向仍按 asc/desc 归一。
func TestNormalizeShiftSortBy(t *testing.T) {
	cases := []struct {
		sortBy, want string
	}{
		{"", "start_time"},
		{"start_time", "start_time"},
		{" Agent_ID ", "agent_id"},
		{"shift_type", "shift_type"},
		{"end_time", "end_time"},
		{"date", "date"},
		{"status", "status"},
		{"created_at", "created_at"},
		{"updated_at", "updated_at"},
		// 注入形态、未知列与 scope 隔离列（tenant_id 不可控排序）一律回退
		{"(CASE WHEN (SELECT 1) THEN id ELSE date END)", "start_time"},
		{"shift_type; DROP TABLE shift_schedules", "start_time"},
		{"1=1) --", "start_time"},
		{"tenant_id", "start_time"},
	}
	for _, c := range cases {
		if got := normalizeShiftSortBy(c.sortBy); got != c.want {
			t.Errorf("normalizeShiftSortBy(%q) = %q, want %q", c.sortBy, got, c.want)
		}
	}
}

// 端到端：注入形态 sort_by 走 ListShifts 全链路必须不报错、行数不损、表完好。
func TestListShiftsInjectionSortByFallsBack(t *testing.T) {
	db := newShiftTestDB(t)
	repo := NewGormRepository(db)
	ctx := scopedContext("t1", "w1")

	base := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := db.Create(&models.ShiftSchedule{
			TenantID: "t1", WorkspaceID: "w1", AgentID: uint(i + 1),
			ShiftType: "morning",
			StartTime: base.Add(time.Duration(i) * time.Hour),
			EndTime:   base.Add(time.Duration(i)*time.Hour + 4*time.Hour),
			Date:      base,
			Status:    "scheduled",
		}).Error; err != nil {
			t.Fatalf("seed shift: %v", err)
		}
	}

	injections := []string{
		"(CASE WHEN (SELECT 1) THEN id ELSE date END)",
		"shift_type; DROP TABLE shift_schedules",
	}
	for _, sortBy := range injections {
		items, err := repo.ListShifts(ctx, &shiftapp.ShiftListRequest{
			Page: 1, PageSize: 10, SortBy: sortBy, SortOrder: "desc",
		})
		if err != nil {
			t.Fatalf("ListShifts(sort_by=%q): %v", sortBy, err)
		}
		if len(items) != 2 {
			t.Fatalf("sort_by=%q: items=%d, want 2 rows intact", sortBy, len(items))
		}
	}
	var n int64
	if err := db.Table("shift_schedules").Count(&n).Error; err != nil || n != 2 {
		t.Fatalf("table intact check: n=%d err=%v", n, err)
	}
}
