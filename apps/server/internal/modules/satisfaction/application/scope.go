package application

import (
	"context"

	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// tenantAndWorkspace/applyScopeFilter 自 services/scope.go 复刻：scope 过滤
// 与本模块查询语义交织，services 原件待 sla 迁移后一并删除。

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
