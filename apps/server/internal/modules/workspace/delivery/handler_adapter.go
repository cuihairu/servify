package delivery

// handler 依赖的最小契约 + 纯转发适配器（不含业务逻辑）。

import (
	"context"

	workspaceapp "servify/apps/server/internal/modules/workspace/application"
)

// HandlerService 是 workspace handler 消费的契约。
type HandlerService interface {
	GetOverview(ctx context.Context, limit int) (*workspaceapp.WorkspaceOverview, error)
}

// HandlerServiceAdapter 把契约转发到 application.Service。
type HandlerServiceAdapter struct {
	svc *workspaceapp.Service
}

func NewHandlerServiceAdapter(svc *workspaceapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{svc: svc}
}

func (a *HandlerServiceAdapter) GetOverview(ctx context.Context, limit int) (*workspaceapp.WorkspaceOverview, error) {
	return a.svc.GetOverview(ctx, limit)
}
