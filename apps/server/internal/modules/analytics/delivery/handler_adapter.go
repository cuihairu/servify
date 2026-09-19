package delivery

import (
	"context"
	"time"

	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
)

// HandlerServiceAdapter 把 analytics module 适配为 HTTP 侧的
// HandlerService 契约；module 返回类型即 contract 类型（别名），纯转发。
type HandlerServiceAdapter struct {
	module *analyticsapp.Service
}

func NewHandlerServiceAdapter(module *analyticsapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{module: module}
}

var (
	_ HandlerService = (*HandlerServiceAdapter)(nil)
)

func (a *HandlerServiceAdapter) GetDashboardStats(ctx context.Context) (*analyticscontract.DashboardStats, error) {
	return a.module.GetDashboardStats(ctx)
}

func (a *HandlerServiceAdapter) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.TimeRangeStats, error) {
	return a.module.GetTimeRangeStats(ctx, startDate, endDate)
}

func (a *HandlerServiceAdapter) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticscontract.AgentPerformanceStats, error) {
	return a.module.GetAgentPerformanceStats(ctx, startDate, endDate, limit)
}

func (a *HandlerServiceAdapter) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	return a.module.GetTicketCategoryStats(ctx, startDate, endDate)
}

func (a *HandlerServiceAdapter) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	return a.module.GetTicketPriorityStats(ctx, startDate, endDate)
}

func (a *HandlerServiceAdapter) GetCustomerSourceStats(ctx context.Context) ([]analyticscontract.CategoryStats, error) {
	return a.module.GetCustomerSourceStats(ctx)
}

func (a *HandlerServiceAdapter) GetRemoteAssistTicketStats(ctx context.Context) (*analyticscontract.RemoteAssistTicketStats, error) {
	return a.module.GetRemoteAssistTicketStats(ctx)
}

func (a *HandlerServiceAdapter) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return a.module.UpdateDailyStats(ctx, date)
}
