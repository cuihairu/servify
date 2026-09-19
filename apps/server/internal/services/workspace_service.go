package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"

	"gorm.io/gorm"
)

// WorkspaceService 汇总全渠道代理工作台数据
type WorkspaceService struct {
	db           *gorm.DB
	agentService workspaceAgentReader
}

type WorkspaceOverviewReader interface {
	GetOverview(ctx context.Context, limit int) (*WorkspaceOverview, error)
}

type workspaceAgentReader interface {
	GetOnlineAgents(ctx context.Context) []*agentdelivery.AgentInfo
}

func NewWorkspaceService(db *gorm.DB, agentService workspaceAgentReader) *WorkspaceService {
	return &WorkspaceService{
		db:           db,
		agentService: agentService,
	}
}

// ChannelSummary 渠道汇总
type ChannelSummary struct {
	Platform        string  `json:"platform"`
	ActiveSessions  int64   `json:"active_sessions"`
	WaitingSessions int64   `json:"waiting_sessions"`
	AvgResponseTime float64 `json:"avg_response_time"`
}

// AgentStatsOverview 可分配客服概览
type AgentStatsOverview struct {
	AvailableAgents []AgentBasicInfo `json:"available_agents"`
}

// AgentBasicInfo 客服基本信息
type AgentBasicInfo struct {
	ID   uint   `json:"id"`
	Name string `json:"name,omitempty"`
}

// WorkspaceOverview 全渠道视图
type WorkspaceOverview struct {
	TotalActiveSessions int64               `json:"total_active_sessions"`
	WaitingQueue        int64               `json:"waiting_queue"`
	OnlineAgents        int64               `json:"online_agents"`
	BusyAgents          int64               `json:"busy_agents"`
	Channels            []ChannelSummary    `json:"channels"`
	RecentSessions      []WorkspaceSession  `json:"recent_sessions"`
	AgentStats          *AgentStatsOverview `json:"agent_stats,omitempty"`
}

// WorkspaceSession 最近会话摘要
type WorkspaceSession struct {
	ID           string    `json:"id"`
	Platform     string    `json:"platform"`
	Status       string    `json:"status"`
	AgentID      *uint     `json:"agent_id"`
	AgentName    string    `json:"agent_name"`
	CustomerID   *uint     `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	StartedAt    time.Time `json:"started_at"`
}

// GetOverview 汇总全渠道工作台所需的数据
func (s *WorkspaceService) GetOverview(ctx context.Context, limit int) (*WorkspaceOverview, error) {
	if limit <= 0 {
		limit = 10
	}

	overview := &WorkspaceOverview{}

	if err := applyScopeFilter(s.db.WithContext(ctx).Model(&models.Session{}), ctx).
		Where("status = ?", "active").
		Count(&overview.TotalActiveSessions).Error; err != nil {
		return nil, fmt.Errorf("count active sessions: %w", err)
	}

	if err := applyScopeFilter(s.db.WithContext(ctx).Model(&models.Session{}), ctx).
		Where("status = ? AND agent_id IS NULL", "active").
		Count(&overview.WaitingQueue).Error; err != nil {
		return nil, fmt.Errorf("count waiting sessions: %w", err)
	}

	var channelRows []struct {
		Platform string
		Active   int64
		Waiting  int64
	}
	if err := applyScopeFilter(s.db.WithContext(ctx).Model(&models.Session{}), ctx).
		Select("COALESCE(platform, 'unknown') AS platform, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) AS active, SUM(CASE WHEN status = 'active' AND agent_id IS NULL THEN 1 ELSE 0 END) AS waiting").
		Group("platform").
		Scan(&channelRows).Error; err != nil {
		return nil, fmt.Errorf("aggregate channels: %w", err)
	}

	for _, row := range channelRows {
		overview.Channels = append(overview.Channels, ChannelSummary{
			Platform:        row.Platform,
			ActiveSessions:  row.Active,
			WaitingSessions: row.Waiting,
			AvgResponseTime: s.getAvgResponseTime(ctx),
		})
	}

	if err := applyScopeFilter(s.db.WithContext(ctx).Model(&models.Agent{}), ctx).
		Where("status = ?", "online").
		Count(&overview.OnlineAgents).Error; err != nil {
		return nil, fmt.Errorf("count online agents: %w", err)
	}
	if err := applyScopeFilter(s.db.WithContext(ctx).Model(&models.Agent{}), ctx).
		Where("status = ? OR current_load >= max_concurrent", "busy").
		Count(&overview.BusyAgents).Error; err != nil {
		return nil, fmt.Errorf("count busy agents: %w", err)
	}

	var sessions []WorkspaceSession
	query := s.db.WithContext(ctx).Table("sessions")
	tenantID, workspaceID := tenantAndWorkspace(ctx)
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
		return nil, fmt.Errorf("load recent sessions: %w", err)
	}
	overview.RecentSessions = sessions

	// 填充可分配客服列表
	if s.agentService != nil {
		onlineAgents := s.agentService.GetOnlineAgents(ctx)
		if len(onlineAgents) > 0 {
			available := make([]AgentBasicInfo, 0, len(onlineAgents))
			for _, a := range onlineAgents {
				if a != nil {
					available = append(available, AgentBasicInfo{
						ID:   a.UserID,
						Name: firstNonEmpty(a.Name, a.Username),
					})
				}
			}
			overview.AgentStats = &AgentStatsOverview{AvailableAgents: available}
		}
	}

	return overview, nil
}

func (s *WorkspaceService) getAvgResponseTime(ctx context.Context) float64 {
	var avg float64
	_ = applyScopeFilter(s.db.WithContext(ctx).Model(&models.Agent{}), ctx).Select("AVG(avg_response_time)").Row().Scan(&avg)
	return avg
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
