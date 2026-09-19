package infra

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
	workspaceapp "servify/apps/server/internal/modules/workspace/application"

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

func (r *GormRepository) CountActiveSessions(ctx context.Context) (int64, error) {
	var n int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Session{}), ctx).
		Where("status = ?", "active").
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *GormRepository) CountWaitingSessions(ctx context.Context) (int64, error) {
	var n int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Session{}), ctx).
		Where("status = ? AND agent_id IS NULL", "active").
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *GormRepository) AggregateChannels(ctx context.Context) ([]workspaceapp.ChannelRow, error) {
	var rows []workspaceapp.ChannelRow
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Session{}), ctx).
		Select("COALESCE(platform, 'unknown') AS platform, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) AS active, SUM(CASE WHEN status = 'active' AND agent_id IS NULL THEN 1 ELSE 0 END) AS waiting").
		Group("platform").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *GormRepository) CountOnlineAgents(ctx context.Context) (int64, error) {
	var n int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).
		Where("status = ?", "online").
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *GormRepository) CountBusyAgents(ctx context.Context) (int64, error) {
	var n int64
	if err := applyScopeFilter(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).
		Where("status = ? OR current_load >= max_concurrent", "busy").
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *GormRepository) RecentSessions(ctx context.Context, limit int) ([]workspaceapp.WorkspaceSession, error) {
	var sessions []workspaceapp.WorkspaceSession
	query := r.db.WithContext(ctx).Table("sessions")
	tenantID, workspaceID := platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
	if tenantID != "" {
		query = query.Where("sessions.tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		query = query.Where("sessions.workspace_id = ?", workspaceID)
	}
	if err := query.
		Select(`sessions.id, COALESCE(sessions.platform, 'unknown') AS platform, sessions.status, sessions.agent_id, sessions.started_at,
				customers.user_id AS customer_id, cu.name AS customer_name, au.name AS agent_name`).
		Joins("LEFT JOIN tickets t ON t.id = sessions.ticket_id AND t.tenant_id = sessions.tenant_id AND t.workspace_id = sessions.workspace_id").
		Joins("LEFT JOIN customers ON customers.user_id = t.customer_id AND customers.tenant_id = sessions.tenant_id AND customers.workspace_id = sessions.workspace_id").
		Joins("LEFT JOIN users cu ON cu.id = customers.user_id").
		Joins("LEFT JOIN agents ag ON ag.user_id = sessions.agent_id AND ag.tenant_id = sessions.tenant_id AND ag.workspace_id = sessions.workspace_id").
		Joins("LEFT JOIN users au ON au.id = ag.user_id").
		Order("sessions.created_at DESC").
		Limit(limit).
		Scan(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *GormRepository) AvgAgentResponseTime(ctx context.Context) float64 {
	var avg float64
	_ = applyScopeFilter(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Select("AVG(avg_response_time)").Row().Scan(&avg)
	return avg
}
