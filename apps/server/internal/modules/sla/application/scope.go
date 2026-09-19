package application

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// tenantAndWorkspace/applyScopeFilter 自 services/scope.go 复刻（sla 为最后一个用户，services 原件随本刀删除）：scope 过滤
// 与本模块查询语义交织，本包与服务语义一致，直接内联。

func tenantAndWorkspace(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}

func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID, workspaceID := tenantAndWorkspace(ctx)
	if tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}
