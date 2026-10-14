package application

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"
)

// tenantAndWorkspace 自 services/scope.go 复刻：scope 取值只服务
// contextWithRecordScope 的落库前回填；查询侧过滤已随 GORM 写归属下沉
// infra/applyScopeFilter（与 assist 等模块同口径）。

func tenantAndWorkspace(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}
