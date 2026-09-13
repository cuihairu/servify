package infra

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	platformauth "servify/apps/server/internal/platform/auth"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) GetDashboardStats(ctx context.Context) (*analyticsapp.DashboardStats, error) {
	stats := &analyticsapp.DashboardStats{}
	today := time.Now().Truncate(24 * time.Hour)

	customerScope(r.db.WithContext(ctx).Model(&models.User{}), ctx).Where("role = ?", "customer").Count(&stats.TotalCustomers)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Count(&stats.TotalAgents)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Count(&stats.TotalTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Session{}), ctx).Count(&stats.TotalSessions)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("created_at >= ?", today).Count(&stats.TodayTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Session{}), ctx).Where("created_at >= ?", today).Count(&stats.TodaySessions)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Message{}), ctx).Where("created_at >= ?", today).Count(&stats.TodayMessages)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("status = ?", "open").Count(&stats.OpenTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("status = ?", "assigned").Count(&stats.AssignedTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("status = ?", "resolved").Count(&stats.ResolvedTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("status = ?", "closed").Count(&stats.ClosedTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("status = ?", "online").Count(&stats.OnlineAgents)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("status = ?", "busy").Count(&stats.BusyAgents)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Session{}), ctx).Where("status = ?", "active").Count(&stats.ActiveSessions)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Select("AVG(avg_response_time)").Row().Scan(&stats.AvgResponseTime)

	var avgResolution float64
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("resolved_at IS NOT NULL").Select(avgResolutionExpr(r.db, "resolved_at", "created_at")).Row().Scan(&avgResolution)
	stats.AvgResolutionTime = avgResolution

	// Real satisfaction: average rating from customer_satisfactions
	var avgSatisfaction float64
	applyEntityScope(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx).
		Select("COALESCE(AVG(rating), 0)").Row().Scan(&avgSatisfaction)
	stats.CustomerSatisfaction = avgSatisfaction

	var dailyStat models.DailyStats
	if shouldUseGlobalDailyStats(ctx) && r.db.WithContext(ctx).Where("date = ?", today).First(&dailyStat).Error == nil {
		stats.AIUsageToday = int64(dailyStat.AIUsageCount)
		stats.KnowledgeProviderUsageToday = int64(dailyStat.WeKnoraUsageCount)
		stats.WeKnoraUsageToday = int64(dailyStat.WeKnoraUsageCount)
	}
	return stats, nil
}

// GetTimeRangeStats 按日聚合区间统计。原先逐日发起 5 条 COUNT（N+1），
// 现改为每指标一条 GROUP BY 聚合，再在 Go 侧按日零填充组装；输出口径与
// 旧实现逐字段一致（UTC 日界，Date 为 YYYY-MM-DD，区间内每一天都有行）。
func (r *GormRepository) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticsapp.TimeRangeStats, error) {
	current := startDate.Truncate(24 * time.Hour)
	end := endDate.Truncate(24 * time.Hour)
	if end.Before(current) {
		return nil, nil
	}
	exclusiveEnd := end.Add(24 * time.Hour)

	days := make([]string, 0, 32)
	byDate := make(map[string]*analyticsapp.TimeRangeStats, 32)
	for d := current; d.Before(end) || d.Equal(end); d = d.Add(24 * time.Hour) {
		key := d.Format("2006-01-02")
		days = append(days, key)
		byDate[key] = &analyticsapp.TimeRangeStats{Date: key}
	}

	type dailyRow struct {
		Day   string
		Total int64
	}
	countInto := func(model interface{}, column string, dest func(*analyticsapp.TimeRangeStats) *int64) error {
		day := dayExpr(r.db, column)
		var rows []dailyRow
		if err := applyEntityScope(r.db.WithContext(ctx).Model(model), ctx).
			Where(column+" >= ? AND "+column+" < ?", current, exclusiveEnd).
			Select(day + " AS day, COUNT(*) AS total").
			Group(day).
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if stat, ok := byDate[row.Day]; ok {
				*dest(stat) = row.Total
			}
		}
		return nil
	}

	if err := countInto(&models.Ticket{}, "created_at", func(s *analyticsapp.TimeRangeStats) *int64 { return &s.Tickets }); err != nil {
		return nil, err
	}
	if err := countInto(&models.Session{}, "created_at", func(s *analyticsapp.TimeRangeStats) *int64 { return &s.Sessions }); err != nil {
		return nil, err
	}
	if err := countInto(&models.Message{}, "created_at", func(s *analyticsapp.TimeRangeStats) *int64 { return &s.Messages }); err != nil {
		return nil, err
	}
	if err := countInto(&models.Ticket{}, "resolved_at", func(s *analyticsapp.TimeRangeStats) *int64 { return &s.ResolvedTickets }); err != nil {
		return nil, err
	}

	if shouldUseGlobalDailyStats(ctx) {
		var dailies []models.DailyStats
		if err := r.db.WithContext(ctx).
			Where("date >= ? AND date <= ?", current, end).
			Find(&dailies).Error; err != nil {
			return nil, err
		}
		for _, daily := range dailies {
			if stat, ok := byDate[daily.Date.Format("2006-01-02")]; ok {
				stat.AvgResponseTime = float64(daily.AvgResponseTime)
				stat.CustomerSatisfaction = daily.CustomerSatisfaction
			}
		}
	} else {
		// DailyStats is currently system-scoped; for scoped requests only derive
		// values that can be safely recomputed from scoped primary data.
		column := "created_at"
		day := dayExpr(r.db, column)
		var rows []struct {
			Day       string
			AvgRating float64
		}
		if err := applyEntityScope(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx).
			Where(column+" >= ? AND "+column+" < ?", current, exclusiveEnd).
			Select(day + " AS day, COALESCE(AVG(rating), 0) AS avg_rating").
			Group(day).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if stat, ok := byDate[row.Day]; ok {
				stat.CustomerSatisfaction = row.AvgRating
			}
		}
	}

	stats := make([]analyticsapp.TimeRangeStats, 0, len(days))
	for _, key := range days {
		stats = append(stats, *byDate[key])
	}
	return stats, nil
}

// dayExpr 把时间列格式化为 UTC 日界的 YYYY-MM-DD 文本；pg 用 AT TIME ZONE
// 固定 UTC 与 Truncate(24h) 口径一致，sqlite 直接 strftime。
func dayExpr(db *gorm.DB, column string) string {
	switch db.Dialector.Name() {
	case "sqlite":
		return fmt.Sprintf("strftime('%%Y-%%m-%%d', %s)", column)
	default:
		return fmt.Sprintf("TO_CHAR(%s AT TIME ZONE 'UTC', 'YYYY-MM-DD')", column)
	}
}

func (r *GormRepository) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticsapp.AgentPerformanceStats, error) {
	var stats []analyticsapp.AgentPerformanceStats
	query := fmt.Sprintf(`
		SELECT
			a.user_id as agent_id,
			u.name as agent_name,
			a.department,
			COUNT(t.id) as total_tickets,
			COUNT(CASE WHEN t.status = 'resolved' OR t.status = 'closed' THEN 1 END) as resolved_tickets,
			a.avg_response_time,
			AVG(CASE WHEN t.resolved_at IS NOT NULL
				THEN %s
				END) as avg_resolution_time,
			a.rating
		FROM agents a
		LEFT JOIN users u ON a.user_id = u.id
		LEFT JOIN tickets t ON a.user_id = t.agent_id
			AND t.created_at >= ? AND t.created_at <= ?
			AND (? = '' OR t.tenant_id = ?)
			AND (? = '' OR t.workspace_id = ?)
		WHERE (? = '' OR a.tenant_id = ?)
			AND (? = '' OR a.workspace_id = ?)
		GROUP BY a.user_id, u.name, a.department, a.avg_response_time, a.rating
		ORDER BY total_tickets DESC
	`, avgResolutionExpr(r.db, "t.resolved_at", "t.created_at"))
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	tenantID, workspaceID := scopeValues(ctx)
	if err := r.db.WithContext(ctx).Raw(query, startDate, endDate, tenantID, tenantID, workspaceID, workspaceID, tenantID, tenantID, workspaceID, workspaceID).Scan(&stats).Error; err != nil {
		return nil, fmt.Errorf("failed to get agent performance stats: %w", err)
	}
	return stats, nil
}

func (r *GormRepository) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticsapp.CategoryStats, error) {
	var stats []analyticsapp.CategoryStats
	err := applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Select("category, COUNT(*) as count").Where("created_at >= ? AND created_at <= ?", startDate, endDate).Group("category").Order("count DESC").Scan(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get category stats: %w", err)
	}
	return stats, nil
}

func (r *GormRepository) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticsapp.CategoryStats, error) {
	var stats []analyticsapp.CategoryStats
	err := applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Select("priority as category, COUNT(*) as count").Where("created_at >= ? AND created_at <= ?", startDate, endDate).Group("priority").Order("count DESC").Scan(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get priority stats: %w", err)
	}
	return stats, nil
}

func (r *GormRepository) GetCustomerSourceStats(ctx context.Context) ([]analyticsapp.CategoryStats, error) {
	var stats []analyticsapp.CategoryStats
	err := applyEntityScope(r.db.WithContext(ctx).Model(&models.Customer{}), ctx).Select("source as category, COUNT(*) as count").Group("source").Order("count DESC").Scan(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get customer source stats: %w", err)
	}
	return stats, nil
}

func (r *GormRepository) UpdateDailyStats(ctx context.Context, date time.Time) error {
	date = date.Truncate(24 * time.Hour)
	nextDay := date.Add(24 * time.Hour)
	statsCtx := systemScopeContext(ctx)
	var dailyStats models.DailyStats
	err := r.db.WithContext(ctx).Where("date = ?", date).First(&dailyStats).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			dailyStats = models.DailyStats{Date: date}
		} else {
			return fmt.Errorf("failed to query daily stats: %w", err)
		}
	}
	var totalSessions, totalMessages, totalTickets, resolvedTickets int64
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Session{}), statsCtx).Where("created_at >= ? AND created_at < ?", date, nextDay).Count(&totalSessions)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Message{}), statsCtx).Where("created_at >= ? AND created_at < ?", date, nextDay).Count(&totalMessages)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), statsCtx).Where("created_at >= ? AND created_at < ?", date, nextDay).Count(&totalTickets)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), statsCtx).Where("resolved_at >= ? AND resolved_at < ?", date, nextDay).Count(&resolvedTickets)
	dailyStats.TotalSessions = int(totalSessions)
	dailyStats.TotalMessages = int(totalMessages)
	dailyStats.TotalTickets = int(totalTickets)
	dailyStats.ResolvedTickets = int(resolvedTickets)
	var avgResponseTime, avgResolutionTime float64
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Agent{}), statsCtx).Select("AVG(avg_response_time)").Row().Scan(&avgResponseTime)
	applyEntityScope(r.db.WithContext(ctx).Model(&models.Ticket{}), statsCtx).Where("resolved_at >= ? AND resolved_at < ? AND resolved_at IS NOT NULL", date, nextDay).Select(avgResolutionExpr(r.db, "resolved_at", "created_at")).Row().Scan(&avgResolutionTime)
	dailyStats.AvgResponseTime = int(avgResponseTime)
	dailyStats.AvgResolutionTime = int(avgResolutionTime)

	// Real satisfaction: average rating from customer_satisfactions for this day
	var avgSat float64
	applyEntityScope(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), statsCtx).
		Where("created_at >= ? AND created_at < ?", date, nextDay).
		Select("COALESCE(AVG(rating), 0)").Row().Scan(&avgSat)
	dailyStats.CustomerSatisfaction = avgSat

	if dailyStats.ID == 0 {
		err = r.db.WithContext(ctx).Create(&dailyStats).Error
	} else {
		err = r.db.WithContext(ctx).Save(&dailyStats).Error
	}
	if err != nil {
		return fmt.Errorf("failed to save daily stats: %w", err)
	}
	return nil
}

func scopeValues(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}

func applyEntityScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID, workspaceID := scopeValues(ctx)
	if tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	return db
}

func customerScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID, workspaceID := scopeValues(ctx)
	if tenantID == "" && workspaceID == "" {
		return db
	}
	db = db.Joins("JOIN customers ON customers.user_id = users.id")
	if tenantID != "" {
		db = db.Where("customers.tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		db = db.Where("customers.workspace_id = ?", workspaceID)
	}
	return db
}

func shouldUseGlobalDailyStats(ctx context.Context) bool {
	tenantID, workspaceID := scopeValues(ctx)
	return tenantID == "" && workspaceID == ""
}

func systemScopeContext(context.Context) context.Context {
	return context.Background()
}

func avgResolutionExpr(db *gorm.DB, resolvedColumn, createdColumn string) string {
	return AvgDurationExpr(db, resolvedColumn, createdColumn)
}

func AvgDurationExpr(db *gorm.DB, endColumn, startColumn string) string {
	switch db.Dialector.Name() {
	case "sqlite":
		return fmt.Sprintf("((julianday(%s) - julianday(%s)) * 86400.0)", endColumn, startColumn)
	default:
		return fmt.Sprintf("EXTRACT(epoch FROM (%s - %s))", endColumn, startColumn)
	}
}

func (r *GormRepository) IncrementDailyStat(ctx context.Context, event analyticsapp.IncrementEvent) error {
	date := event.Date.Truncate(24 * time.Hour)
	column := ""
	switch event.Kind {
	case analyticsapp.IncrementSessions:
		column = "total_sessions"
	case analyticsapp.IncrementMessages:
		column = "total_messages"
	case analyticsapp.IncrementTickets:
		column = "total_tickets"
	case analyticsapp.IncrementResolved:
		column = "resolved_tickets"
	case analyticsapp.IncrementAIUsage:
		column = "ai_usage_count"
	case analyticsapp.IncrementKnowledgeProvider, analyticsapp.IncrementWeKnora:
		column = "we_knora_usage_count"
	case analyticsapp.IncrementSLA:
		column = "sla_violations"
	default:
		return nil
	}
	var daily models.DailyStats
	if err := r.db.WithContext(ctx).Where("date = ?", date).First(&daily).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			daily = models.DailyStats{Date: date}
			if err := r.db.WithContext(ctx).Create(&daily).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return r.db.WithContext(ctx).Model(&daily).UpdateColumn(column, gorm.Expr(column+" + 1")).Error
}
