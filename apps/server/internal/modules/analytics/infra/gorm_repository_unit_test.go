package infra

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	analyticsapp "servify/apps/server/internal/modules/analytics/application"
)

func TestGormRepositoryDashboardStatsGlobal(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	today := time.Now().Truncate(24 * time.Hour)

	users := []models.User{
		{ID: 51, Username: "cust-1", Email: "cust-1@example.com", Role: "customer"},
		{ID: 52, Username: "cust-2", Email: "cust-2@example.com", Role: "customer"},
		{ID: 53, Username: "admin-1", Email: "admin-1@example.com", Role: "admin"},
		{ID: 54, Username: "agent-1", Email: "agent-1@example.com", Role: "agent"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Create(&models.Agent{
		UserID:          54,
		Department:      "support",
		Status:          "online",
		AvgResponseTime: 60,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := db.Create(&models.Agent{
		UserID:          55,
		Department:      "support",
		Status:          "busy",
		AvgResponseTime: 60,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	tickets := []models.Ticket{
		{ID: 501, Title: "open", Status: "open", CreatedAt: today.Add(time.Hour), UpdatedAt: today.Add(time.Hour)},
		{ID: 502, Title: "assigned", Status: "assigned", CreatedAt: today, UpdatedAt: today},
		{ID: 503, Title: "resolved", Status: "resolved", CreatedAt: today.Add(2 * time.Hour), ResolvedAt: timePtr(today.Add(3 * time.Hour)), UpdatedAt: today.Add(3 * time.Hour)},
		{ID: 504, Title: "closed", Status: "closed", CreatedAt: today.Add(-48 * time.Hour), UpdatedAt: today.Add(-47 * time.Hour)},
	}
	if err := db.Create(&tickets).Error; err != nil {
		t.Fatalf("create tickets: %v", err)
	}
	if err := db.Create(&models.Session{
		ID:        "sess-global",
		UserID:    51,
		Status:    "active",
		StartedAt: today.Add(time.Hour),
		CreatedAt: today.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&models.Message{
		SessionID: "sess-global",
		Content:   "hi",
		Type:      "text",
		Sender:    "customer",
		CreatedAt: today.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	satisfactions := []models.CustomerSatisfaction{
		{TicketID: 501, CustomerID: 51, Rating: 4, Category: "overall", CreatedAt: today.Add(time.Hour)},
		{TicketID: 502, CustomerID: 52, Rating: 5, Category: "overall", CreatedAt: today.Add(time.Hour)},
	}
	if err := db.Create(&satisfactions).Error; err != nil {
		t.Fatalf("create satisfactions: %v", err)
	}
	if err := db.Create(&models.DailyStats{
		Date:              today,
		AIUsageCount:      7,
		WeKnoraUsageCount: 3,
	}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	stats, err := repo.GetDashboardStats(ctx)
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats.TotalCustomers != 2 {
		t.Errorf("TotalCustomers = %d, want 2", stats.TotalCustomers)
	}
	if stats.TotalAgents != 2 {
		t.Errorf("TotalAgents = %d, want 2", stats.TotalAgents)
	}
	if stats.TotalTickets != 4 {
		t.Errorf("TotalTickets = %d, want 4", stats.TotalTickets)
	}
	if stats.TodayTickets != 3 {
		t.Errorf("TodayTickets = %d, want 3", stats.TodayTickets)
	}
	if stats.OpenTickets != 1 || stats.AssignedTickets != 1 || stats.ResolvedTickets != 1 || stats.ClosedTickets != 1 {
		t.Errorf("status counts unexpected: %+v", stats)
	}
	if stats.OnlineAgents != 1 || stats.BusyAgents != 1 {
		t.Errorf("agent status counts unexpected: %+v", stats)
	}
	if stats.ActiveSessions != 1 || stats.TotalSessions != 1 || stats.TodaySessions != 1 || stats.TodayMessages != 1 {
		t.Errorf("session counts unexpected: %+v", stats)
	}
	if stats.AvgResponseTime != 60 {
		t.Errorf("AvgResponseTime = %v, want 60", stats.AvgResponseTime)
	}
	if math.Abs(stats.AvgResolutionTime-3600) > 1 {
		t.Errorf("AvgResolutionTime = %v, want ~3600", stats.AvgResolutionTime)
	}
	if math.Abs(stats.CustomerSatisfaction-4.5) > 0.0001 {
		t.Errorf("CustomerSatisfaction = %v, want 4.5", stats.CustomerSatisfaction)
	}
	if stats.AIUsageToday != 7 || stats.KnowledgeProviderUsageToday != 3 || stats.WeKnoraUsageToday != 3 {
		t.Errorf("daily usage counts unexpected: %+v", stats)
	}
}

func TestGormRepositoryDashboardStatsGlobalWithoutDailyStats(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)

	stats, err := repo.GetDashboardStats(context.Background())
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats == nil {
		t.Fatal("expected stats instance")
	}
	if stats.TotalCustomers != 0 || stats.TotalAgents != 0 || stats.TotalTickets != 0 {
		t.Fatalf("expected zero counts on empty db, got %+v", stats)
	}
	if stats.AIUsageToday != 0 || stats.WeKnoraUsageToday != 0 {
		t.Fatalf("expected zero usage counts without daily stats, got %+v", stats)
	}
}

func TestGormRepositoryTimeRangeStatsGlobal(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	day1 := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	if err := db.Create(&models.Ticket{
		ID: 601, Title: "day1-ticket", Status: "open",
		CreatedAt: day1.Add(time.Hour), UpdatedAt: day1.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if err := db.Create(&models.Ticket{
		ID: 602, Title: "day1-resolved", Status: "resolved",
		CreatedAt: day1.Add(-24 * time.Hour), UpdatedAt: day1.Add(2 * time.Hour),
		ResolvedAt: timePtr(day1.Add(2 * time.Hour)),
	}).Error; err != nil {
		t.Fatalf("create resolved ticket: %v", err)
	}
	if err := db.Create(&models.Session{
		ID: "sess-day1", UserID: 61, Status: "ended",
		StartedAt: day1.Add(time.Hour), CreatedAt: day1.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&models.Message{
		SessionID: "sess-day1", Content: "msg", Type: "text", Sender: "customer",
		CreatedAt: day1.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := db.Create(&models.DailyStats{
		Date:                 day1,
		AvgResponseTime:      120,
		CustomerSatisfaction: 4.5,
	}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	items, err := repo.GetTimeRangeStats(ctx, day1, day2)
	if err != nil {
		t.Fatalf("GetTimeRangeStats: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(items), items)
	}
	first := items[0]
	if first.Date != "2026-04-08" {
		t.Errorf("first row date = %q, want 2026-04-08", first.Date)
	}
	if first.Tickets != 1 || first.Sessions != 1 || first.Messages != 1 || first.ResolvedTickets != 1 {
		t.Errorf("day1 counts unexpected: %+v", first)
	}
	if first.AvgResponseTime != 120 {
		t.Errorf("day1 AvgResponseTime = %v, want 120", first.AvgResponseTime)
	}
	if math.Abs(first.CustomerSatisfaction-4.5) > 0.0001 {
		t.Errorf("day1 CustomerSatisfaction = %v, want 4.5", first.CustomerSatisfaction)
	}
	second := items[1]
	if second.Date != "2026-04-09" || second.Tickets != 0 || second.AvgResponseTime != 0 {
		t.Errorf("day2 (no daily stats) should be zero: %+v", second)
	}
}

func TestGormRepositoryTimeRangeStatsEmptyRange(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)

	start := time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	items, err := repo.GetTimeRangeStats(context.Background(), start, end)
	if err != nil {
		t.Fatalf("GetTimeRangeStats: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no rows for inverted range, got %+v", items)
	}
}

func TestGormRepositoryAgentPerformanceStatsGlobalAndLimit(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	users := []models.User{
		{ID: 71, Username: "agent-x", Email: "agent-x@example.com", Name: "Agent X", Role: "agent"},
		{ID: 72, Username: "agent-y", Email: "agent-y@example.com", Name: "Agent Y", Role: "agent"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	agents := []models.Agent{
		{UserID: 71, Department: "support", AvgResponseTime: 10, Rating: 4.5},
		{UserID: 72, Department: "sales", AvgResponseTime: 20, Rating: 4.0},
	}
	if err := db.Create(&agents).Error; err != nil {
		t.Fatalf("create agents: %v", err)
	}
	agentID := uint(71)
	if err := db.Create(&[]models.Ticket{
		{
			ID: 701, Title: "t1", Status: "resolved", AgentID: &agentID,
			CreatedAt: start.Add(time.Hour), ResolvedAt: timePtr(start.Add(2 * time.Hour)), UpdatedAt: start.Add(2 * time.Hour),
		},
		{
			ID: 702, Title: "t2", Status: "open", AgentID: &agentID,
			CreatedAt: start.Add(3 * time.Hour), UpdatedAt: start.Add(3 * time.Hour),
		},
		{
			ID: 703, Title: "t3", Status: "open", AgentID: &agentID,
			CreatedAt: start.Add(48 * time.Hour), UpdatedAt: start.Add(48 * time.Hour),
		},
	}).Error; err != nil {
		t.Fatalf("create tickets: %v", err)
	}

	limited, err := repo.GetAgentPerformanceStats(ctx, start, end, 1)
	if err != nil {
		t.Fatalf("GetAgentPerformanceStats limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 row with limit 1, got %+v", limited)
	}
	if limited[0].AgentID != 71 || limited[0].AgentName != "Agent X" || limited[0].Department != "support" {
		t.Fatalf("unexpected limited row: %+v", limited[0])
	}
	if limited[0].TotalTickets != 2 || limited[0].ResolvedTickets != 1 {
		t.Fatalf("unexpected aggregates: %+v", limited[0])
	}
	if limited[0].AvgResponseTime != 10 || limited[0].Rating != 4.5 {
		t.Fatalf("unexpected agent fields: %+v", limited[0])
	}
	if math.Abs(limited[0].AvgResolutionTime-3600) > 1 {
		t.Fatalf("AvgResolutionTime = %v, want ~3600", limited[0].AvgResolutionTime)
	}

	all, err := repo.GetAgentPerformanceStats(ctx, start, end, 0)
	if err != nil {
		t.Fatalf("GetAgentPerformanceStats unlimited: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows without limit, got %+v", all)
	}
}

func TestGormRepositoryAgentPerformanceStatsQueryError(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}

	_, err := repo.GetAgentPerformanceStats(context.Background(), time.Now(), time.Now(), 5)
	if err == nil || !strings.Contains(err.Error(), "failed to get agent performance stats") {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestGormRepositoryTicketStatsQueryErrors(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}

	if _, err := repo.GetTicketCategoryStats(ctx, start, end); err == nil || !strings.Contains(err.Error(), "failed to get category stats") {
		t.Fatalf("expected category stats error, got %v", err)
	}
	if _, err := repo.GetTicketPriorityStats(ctx, start, end); err == nil || !strings.Contains(err.Error(), "failed to get priority stats") {
		t.Fatalf("expected priority stats error, got %v", err)
	}

	sourceDB := newAnalyticsScopeTestDB(t)
	sourceRepo := NewGormRepository(sourceDB)
	if err := sourceDB.Migrator().DropTable(&models.Customer{}); err != nil {
		t.Fatalf("drop customers: %v", err)
	}
	if _, err := sourceRepo.GetCustomerSourceStats(ctx); err == nil || !strings.Contains(err.Error(), "failed to get customer source stats") {
		t.Fatalf("expected customer source stats error, got %v", err)
	}
}

func TestGormRepositoryUpdateDailyStatsCreatesRow(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	day := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	nextDay := day.Add(24 * time.Hour)

	if err := db.Create(&models.Agent{UserID: 81, AvgResponseTime: 30}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := db.Create(&models.Session{
		ID: "sess-stats", UserID: 81, Status: "ended",
		StartedAt: day.Add(time.Hour), CreatedAt: day.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&[]models.Message{
		{SessionID: "sess-stats", Content: "m1", Type: "text", Sender: "customer", CreatedAt: day.Add(time.Hour)},
		{SessionID: "sess-stats", Content: "m2", Type: "text", Sender: "customer", CreatedAt: day.Add(2 * time.Hour)},
	}).Error; err != nil {
		t.Fatalf("create messages: %v", err)
	}
	if err := db.Create(&models.Ticket{
		ID: 801, Title: "created-today", Status: "open",
		CreatedAt: day.Add(3 * time.Hour), UpdatedAt: day.Add(3 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if err := db.Create(&models.Ticket{
		ID: 802, Title: "resolved-today", Status: "resolved",
		CreatedAt: day.Add(-2 * time.Hour), UpdatedAt: day.Add(2 * time.Hour),
		ResolvedAt: timePtr(day.Add(2 * time.Hour)),
	}).Error; err != nil {
		t.Fatalf("create resolved ticket: %v", err)
	}
	if err := db.Create(&models.CustomerSatisfaction{
		TicketID: 801, CustomerID: 81, Rating: 4, Category: "overall", CreatedAt: day.Add(4 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create satisfaction: %v", err)
	}

	if err := repo.UpdateDailyStats(context.Background(), day.Add(5*time.Hour)); err != nil {
		t.Fatalf("UpdateDailyStats: %v", err)
	}

	var stored models.DailyStats
	if err := db.Where("date = ?", day).First(&stored).Error; err != nil {
		t.Fatalf("load daily stats: %v", err)
	}
	if stored.ID == 0 {
		t.Fatal("expected daily stats row to be created")
	}
	if stored.TotalSessions != 1 || stored.TotalMessages != 2 || stored.TotalTickets != 1 || stored.ResolvedTickets != 1 {
		t.Fatalf("unexpected counters: %+v", stored)
	}
	if stored.AvgResponseTime != 30 {
		t.Fatalf("AvgResponseTime = %d, want 30", stored.AvgResponseTime)
	}
	if math.Abs(float64(stored.AvgResolutionTime)-14400) > 1 {
		t.Fatalf("AvgResolutionTime = %d, want ~14400", stored.AvgResolutionTime)
	}
	if math.Abs(stored.CustomerSatisfaction-4) > 0.0001 {
		t.Fatalf("CustomerSatisfaction = %v, want 4", stored.CustomerSatisfaction)
	}
	if !stored.Date.Equal(day) {
		t.Fatalf("Date = %v, want %v", stored.Date, day)
	}
	_ = nextDay
}

func TestGormRepositoryUpdateDailyStatsUpdatesExistingRow(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	day := time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC)

	seeded := models.DailyStats{Date: day, TotalSessions: 99, TotalMessages: 99, TotalTickets: 99, ResolvedTickets: 99}
	if err := db.Create(&seeded).Error; err != nil {
		t.Fatalf("seed daily stats: %v", err)
	}

	if err := repo.UpdateDailyStats(context.Background(), day); err != nil {
		t.Fatalf("UpdateDailyStats: %v", err)
	}

	var rows []models.DailyStats
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("list daily stats: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected single daily stats row, got %d", len(rows))
	}
	if rows[0].ID != seeded.ID {
		t.Fatalf("expected existing row %d to be updated, got %d", seeded.ID, rows[0].ID)
	}
	if rows[0].TotalSessions != 0 || rows[0].TotalMessages != 0 || rows[0].TotalTickets != 0 || rows[0].ResolvedTickets != 0 {
		t.Fatalf("expected counters to be overwritten, got %+v", rows[0])
	}
}

func TestGormRepositoryUpdateDailyStatsQueryError(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable(&models.DailyStats{}); err != nil {
		t.Fatalf("drop daily stats: %v", err)
	}

	err := repo.UpdateDailyStats(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "failed to query daily stats") {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestGormRepositoryUpdateDailyStatsSaveError(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Exec("CREATE TRIGGER block_daily_insert BEFORE INSERT ON daily_stats BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err := repo.UpdateDailyStats(context.Background(), time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "failed to save daily stats") {
		t.Fatalf("expected save error, got %v", err)
	}
}

func TestGormRepositoryIncrementDailyStatAllKinds(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	if err := db.Create(&models.DailyStats{Date: day}).Error; err != nil {
		t.Fatalf("seed daily stats: %v", err)
	}

	cases := []struct {
		kind   analyticsapp.IncrementKind
		column string
	}{
		{analyticsapp.IncrementSessions, "total_sessions"},
		{analyticsapp.IncrementMessages, "total_messages"},
		{analyticsapp.IncrementTickets, "total_tickets"},
		{analyticsapp.IncrementResolved, "resolved_tickets"},
		{analyticsapp.IncrementAIUsage, "ai_usage_count"},
		{analyticsapp.IncrementKnowledgeProvider, "we_knora_usage_count"},
		{analyticsapp.IncrementWeKnora, "we_knora_usage_count"},
		{analyticsapp.IncrementSLA, "sla_violations"},
	}
	for _, tc := range cases {
		// pass a non-midnight date to verify truncation onto the seeded row
		if err := repo.IncrementDailyStat(context.Background(), analyticsapp.IncrementEvent{Date: day.Add(7 * time.Hour), Kind: tc.kind}); err != nil {
			t.Fatalf("IncrementDailyStat(%s): %v", tc.kind, err)
		}
	}

	var stored models.DailyStats
	if err := db.Where("date = ?", day).First(&stored).Error; err != nil {
		t.Fatalf("load daily stats: %v", err)
	}
	expected := map[string]int{
		"total_sessions":       1,
		"total_messages":       1,
		"total_tickets":        1,
		"resolved_tickets":     1,
		"ai_usage_count":       1,
		"we_knora_usage_count": 2,
		"sla_violations":       1,
	}
	got := map[string]int{
		"total_sessions":       stored.TotalSessions,
		"total_messages":       stored.TotalMessages,
		"total_tickets":        stored.TotalTickets,
		"resolved_tickets":     stored.ResolvedTickets,
		"ai_usage_count":       stored.AIUsageCount,
		"we_knora_usage_count": stored.WeKnoraUsageCount,
		"sla_violations":       stored.SLAViolations,
	}
	for column, want := range expected {
		if got[column] != want {
			t.Errorf("%s = %d, want %d", column, got[column], want)
		}
	}
}

func TestGormRepositoryIncrementDailyStatUnknownKindIsNoop(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	day := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	if err := repo.IncrementDailyStat(context.Background(), analyticsapp.IncrementEvent{Date: day, Kind: analyticsapp.IncrementKind("bogus")}); err != nil {
		t.Fatalf("IncrementDailyStat(unknown): %v", err)
	}

	var count int64
	if err := db.Model(&models.DailyStats{}).Where("date = ?", day).Count(&count).Error; err != nil {
		t.Fatalf("count daily stats: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no row for unknown kind, got %d", count)
	}
}

func TestGormRepositoryIncrementDailyStatCreatesRowWhenMissing(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	day := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)

	if err := repo.IncrementDailyStat(context.Background(), analyticsapp.IncrementEvent{Date: day.Add(3 * time.Hour), Kind: analyticsapp.IncrementTickets}); err != nil {
		t.Fatalf("IncrementDailyStat: %v", err)
	}

	var stored models.DailyStats
	if err := db.Where("date = ?", day).First(&stored).Error; err != nil {
		t.Fatalf("load daily stats: %v", err)
	}
	if stored.TotalTickets != 1 {
		t.Fatalf("TotalTickets = %d, want 1", stored.TotalTickets)
	}
	if stored.TotalSessions != 0 || stored.AIUsageCount != 0 {
		t.Fatalf("unexpected extra counters: %+v", stored)
	}
}

func TestGormRepositoryIncrementDailyStatQueryError(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable(&models.DailyStats{}); err != nil {
		t.Fatalf("drop daily stats: %v", err)
	}

	err := repo.IncrementDailyStat(context.Background(), analyticsapp.IncrementEvent{Date: time.Now(), Kind: analyticsapp.IncrementTickets})
	if err == nil {
		t.Fatal("expected query error from IncrementDailyStat")
	}
}

func TestGormRepositoryIncrementDailyStatCreateError(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Exec("CREATE TRIGGER block_increment_insert BEFORE INSERT ON daily_stats BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err := repo.IncrementDailyStat(context.Background(), analyticsapp.IncrementEvent{
		Date: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		Kind: analyticsapp.IncrementSessions,
	})
	if err == nil || !strings.Contains(err.Error(), "insert blocked") {
		t.Fatalf("expected create error, got %v", err)
	}
}

type stubDialector struct {
	gorm.Dialector
	name string
}

func (d stubDialector) Name() string { return d.name }

func TestAvgDurationExprDialectVariants(t *testing.T) {
	db := newAnalyticsScopeTestDB(t)

	if got, want := AvgDurationExpr(db, "t.resolved_at", "t.created_at"),
		"((julianday(t.resolved_at) - julianday(t.created_at)) * 86400.0)"; got != want {
		t.Fatalf("sqlite expr = %q, want %q", got, want)
	}

	cfg := *db.Config
	cfg.Dialector = stubDialector{name: "postgres"}
	postgresDB := &gorm.DB{Config: &cfg}
	if got, want := AvgDurationExpr(postgresDB, "resolved_at", "created_at"),
		"EXTRACT(epoch FROM (resolved_at - created_at))"; got != want {
		t.Fatalf("postgres expr = %q, want %q", got, want)
	}

	cfg2 := *db.Config
	cfg2.Dialector = stubDialector{name: "mysql"}
	mysqlDB := &gorm.DB{Config: &cfg2}
	if got, want := AvgDurationExpr(mysqlDB, "ended_at", "started_at"),
		"EXTRACT(epoch FROM (ended_at - started_at))"; got != want {
		t.Fatalf("mysql expr = %q, want %q", got, want)
	}
}
