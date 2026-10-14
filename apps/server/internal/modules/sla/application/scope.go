package application

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"
)

// tenantAndWorkspace 自 services/scope.go 复刻（GORM 写归属下沉 infra 后仅保留
// 上下文读取；SQL 侧 applyScopeFilter 随仓储下沉 infra/gorm_repository.go）。

func tenantAndWorkspace(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}
