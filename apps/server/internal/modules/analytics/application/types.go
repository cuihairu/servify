package application

import (
	"time"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
)

// 统计形状与 HTTP 契约字段全同：直接别名 contract 类型，避免双份
// struct 与 DTO→contract 的纯复制转换（HTTP 面只认 contract 类型）。
type (
	DashboardStats          = analyticscontract.DashboardStats
	TimeRangeStats          = analyticscontract.TimeRangeStats
	AgentPerformanceStats   = analyticscontract.AgentPerformanceStats
	CategoryStats           = analyticscontract.CategoryStats
	RemoteAssistTicketStats = analyticscontract.RemoteAssistTicketStats
)

type IncrementKind string

const (
	IncrementSessions          IncrementKind = "sessions"
	IncrementMessages          IncrementKind = "messages"
	IncrementTickets           IncrementKind = "tickets"
	IncrementResolved          IncrementKind = "resolved"
	IncrementAIUsage           IncrementKind = "ai_usage"
	IncrementKnowledgeProvider IncrementKind = "knowledge_provider_usage"
	IncrementWeKnora           IncrementKind = "weknora_usage"
	IncrementSLA               IncrementKind = "sla_violations"
)

type IncrementEvent struct {
	Date time.Time
	Kind IncrementKind
}
