package infra

import (
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	slapp "servify/apps/server/internal/modules/sla/application"
)

// 排序字段白名单回归（审核 P0-1）：sort_by 由查询参数直达 slaOrder 的
// Order 拼接，未白名单任意串即盲注面。修复后非白名单值一律回退
// created_at，方向仍按 asc/desc 归一。
func TestSLAOrderWhitelist(t *testing.T) {
	cases := []struct {
		sortBy, sortOrder, want string
	}{
		{"", "", "created_at desc"},
		{"created_at", "asc", "created_at asc"},
		{"Warning_Threshold", "desc", "warning_threshold desc"},
		// 方向保持原语义：仅精确 asc/desc，其余（含大小写变体）回退 desc
		{" Priority ", "ASC", "priority desc"},
		{"resolution_time", "bogus", "resolution_time desc"},
		// 注入形态与未知列一律回退默认列
		{"(CASE WHEN (SELECT 1) THEN id ELSE active END)", "desc", "created_at desc"},
		{"name; DROP TABLE sla_configs", "asc", "created_at asc"},
		{"1=1) --", "", "created_at desc"},
	}
	for _, c := range cases {
		if got := slaOrder(c.sortBy, c.sortOrder); got != c.want {
			t.Errorf("slaOrder(%q, %q) = %q, want %q", c.sortBy, c.sortOrder, got, c.want)
		}
	}
}

// 违约列表排序走独立白名单：合法违约列（violated_at/deadline/violation_type）
// 不得被配置列白名单误回退；注入形态回退 created_at。
func TestSLAViolationOrderWhitelist(t *testing.T) {
	cases := []struct {
		sortBy, sortOrder, want string
	}{
		{"violated_at", "desc", "violated_at desc"},
		{"deadline", "asc", "deadline asc"},
		{"violation_type", "desc", "violation_type desc"},
		{"resolved", "asc", "resolved asc"},
		// 配置列不属违约白名单 → 回退
		{"priority", "desc", "created_at desc"},
		{"(CASE WHEN (SELECT 1) THEN id ELSE created_at END)", "", "created_at desc"},
		{"violation_type; DROP TABLE sla_violations", "", "created_at desc"},
	}
	for _, c := range cases {
		if got := slaViolationOrder(c.sortBy, c.sortOrder); got != c.want {
			t.Errorf("slaViolationOrder(%q, %q) = %q, want %q", c.sortBy, c.sortOrder, got, c.want)
		}
	}
}

// 端到端：注入形态 sort_by 走 ListViolations 全链路不报错、表完好。
func TestListViolationsInjectionSortByFallsBack(t *testing.T) {
	db := newSLATestDB(t, &models.SLAConfig{}, &models.SLAViolation{})
	repo := NewGormRepository(db)
	if err := db.Create(&models.SLAViolation{
		TenantID: "t1", WorkspaceID: "w1", TicketID: 1, SLAConfigID: 1,
		ViolationType: "first_response", Deadline: time.Now(), ViolatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed violation: %v", err)
	}
	ctx := unitScopedContext("t1", "w1")

	injections := []string{
		"(CASE WHEN (SELECT 1) THEN id ELSE created_at END)",
		"violation_type; DROP TABLE sla_violations",
	}
	for _, sortBy := range injections {
		items, err := repo.ListViolations(ctx, &slapp.SLAViolationListRequest{
			Page: 1, PageSize: 10, SortBy: sortBy, SortOrder: "desc",
		})
		if err != nil {
			t.Fatalf("ListViolations(sort_by=%q): %v", sortBy, err)
		}
		if len(items) != 1 {
			t.Fatalf("sort_by=%q: items=%d, want 1 row intact", sortBy, len(items))
		}
	}
	var n int64
	if err := db.Table("sla_violations").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("table intact check: n=%d err=%v", n, err)
	}
}

// 端到端：注入形态 sort_by 走 ListConfigs 全链路必须不报错、行数不损、表完好。
func TestListConfigsInjectionSortByFallsBack(t *testing.T) {
	db := newSLATestDB(t, &models.SLAConfig{})
	repo := NewGormRepository(db)
	now := time.Now()
	for i := 0; i < 2; i++ {
		if err := db.Create(&models.SLAConfig{
			Name: fmt.Sprintf("cfg-%d", i), Priority: "high", CustomerTier: "",
			FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
			Active: true, TenantID: "t1", WorkspaceID: "w1",
			CreatedAt: now.Add(-time.Duration(i) * time.Minute), UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed config: %v", err)
		}
	}
	ctx := unitScopedContext("t1", "w1")

	injections := []string{
		"(CASE WHEN (SELECT 1) THEN id ELSE active END)",
		"name; DROP TABLE sla_configs",
	}
	for _, sortBy := range injections {
		items, err := repo.ListConfigs(ctx, &slapp.SLAConfigListRequest{
			Page: 1, PageSize: 10, SortBy: sortBy, SortOrder: "desc",
		})
		if err != nil {
			t.Fatalf("ListConfigs(sort_by=%q): %v", sortBy, err)
		}
		if len(items) != 2 {
			t.Fatalf("sort_by=%q: items=%d, want 2 rows intact", sortBy, len(items))
		}
	}
	var n int64
	if err := db.Table("sla_configs").Count(&n).Error; err != nil || n != 2 {
		t.Fatalf("table intact check: n=%d err=%v", n, err)
	}
}
