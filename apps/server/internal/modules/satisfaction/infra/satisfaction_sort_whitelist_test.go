package infra

import (
	"testing"

	"servify/apps/server/internal/models"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"
)

// 排序字段白名单回归（审核 P0-1）：sort_by 由查询参数直达 List 的 Order
// 拼接，未白名单任意串（如 "(CASE WHEN ...)"）即盲注面。修复后非白名单值
// 一律回退 created_at。
func TestNormalizeSatisfactionSortBy(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "created_at"},
		{"created_at", "created_at"},
		{"  RATING  ", "rating"},
		{"Category", "category"},
		{"ticket_id", "ticket_id"},
		{"customer_id", "customer_id"},
		{"agent_id", "agent_id"},
		{"id", "id"},
		// 注入形态与未知列一律回退默认列
		{"(CASE WHEN (SELECT 1) THEN id ELSE rating END)", "created_at"},
		{"rating; DROP TABLE customer_satisfactions", "created_at"},
		{"1=1) --", "created_at"},
		{"bogus_column", "created_at"},
	}
	for _, c := range cases {
		if got := normalizeSatisfactionSortBy(c.in); got != c.want {
			t.Errorf("normalizeSatisfactionSortBy(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 端到端：注入形态 sort_by 走 List 全链路必须不报错、行数不损、表完好。
func TestListSatisfactionsInjectionSortByFallsBack(t *testing.T) {
	_, db := newSatisfactionTestService(t)
	repo := NewGormRepository(db)
	customerID, agentID, ticketID := seedSatisfactionData(t, db)
	if err := db.Create(&models.CustomerSatisfaction{
		TenantID: "t1", WorkspaceID: "w1",
		TicketID: ticketID, CustomerID: customerID, AgentID: &agentID,
		Rating: 5, Category: "overall",
	}).Error; err != nil {
		t.Fatalf("seed satisfaction: %v", err)
	}
	ctx := unitScopedContext("t1", "w1")

	injections := []string{
		"(CASE WHEN (SELECT 1) THEN id ELSE rating END)",
		"rating; DROP TABLE customer_satisfactions",
	}
	for _, sortBy := range injections {
		items, total, err := repo.ListSatisfactions(ctx, &satisfapp.SatisfactionListRequest{
			Page: 1, PageSize: 10, SortBy: sortBy, SortOrder: "desc",
		})
		if err != nil {
			t.Fatalf("ListSatisfactions(sort_by=%q): %v", sortBy, err)
		}
		if total != 1 || len(items) != 1 {
			t.Fatalf("sort_by=%q: total=%d items=%d, want 1 row intact", sortBy, total, len(items))
		}
	}
	var n int64
	if err := db.Table("customer_satisfactions").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("table intact check: n=%d err=%v", n, err)
	}
}
