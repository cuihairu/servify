package application

// 全渠道代理工作台数据汇总（自 services/workspace_service.go 下沉）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
)

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

// ChannelRow 渠道聚合行（infra 扫描结果）
type ChannelRow struct {
	Platform string
	Active   int64
	Waiting  int64
}

// workspaceAgentReader 在线客服读取的窄接口（装配时传 agent 模块 adapter）。
type workspaceAgentReader interface {
	GetOnlineAgents(ctx context.Context) []*agentdelivery.AgentInfo
}

type Repository interface {
	CountActiveSessions(ctx context.Context) (int64, error)
	CountWaitingSessions(ctx context.Context) (int64, error)
	AggregateChannels(ctx context.Context) ([]ChannelRow, error)
	CountOnlineAgents(ctx context.Context) (int64, error)
	CountBusyAgents(ctx context.Context) (int64, error)
	RecentSessions(ctx context.Context, limit int) ([]WorkspaceSession, error)
	// AvgAgentResponseTime 读平均响应时长；读不到按 0 处理（原语义忽略错误）。
	AvgAgentResponseTime(ctx context.Context) float64
}

type Service struct {
	repo         Repository
	agentService workspaceAgentReader
}

func NewService(repo Repository, agentService workspaceAgentReader) *Service {
	return &Service{repo: repo, agentService: agentService}
}

// GetOverview 汇总全渠道工作台所需的数据
func (s *Service) GetOverview(ctx context.Context, limit int) (*WorkspaceOverview, error) {
	if limit <= 0 {
		limit = 10
	}

	overview := &WorkspaceOverview{}

	totalActive, err := s.repo.CountActiveSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("count active sessions: %w", err)
	}
	overview.TotalActiveSessions = totalActive

	waiting, err := s.repo.CountWaitingSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("count waiting sessions: %w", err)
	}
	overview.WaitingQueue = waiting

	channelRows, err := s.repo.AggregateChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("aggregate channels: %w", err)
	}
	for _, row := range channelRows {
		overview.Channels = append(overview.Channels, ChannelSummary{
			Platform:        row.Platform,
			ActiveSessions:  row.Active,
			WaitingSessions: row.Waiting,
			AvgResponseTime: s.repo.AvgAgentResponseTime(ctx),
		})
	}

	online, err := s.repo.CountOnlineAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("count online agents: %w", err)
	}
	overview.OnlineAgents = online

	busy, err := s.repo.CountBusyAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("count busy agents: %w", err)
	}
	overview.BusyAgents = busy

	sessions, err := s.repo.RecentSessions(ctx, limit)
	if err != nil {
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
