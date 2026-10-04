package bootstrap

import (
	"fmt"

	"gorm.io/gorm"
)

// V1.0 收敛 B2-4（docs/v1-convergence-plan.md §9.4/§10 P2）：核心业务表
// tenant_id/workspace_id 回填——default workspace 语义。历史版本写入的行
// 可能留空 scope（创建早于租户过滤接线、或写入路径无租户上下文），回填后
// 这些行获得确定的归属，可被 default 租户/工作区的过滤查询命中。
//
// 两条执行路径，表清单必须保持一致：
//   - PostgreSQL：migrations/000017_scope_backfill.up.sql（versioned SQL）；
//   - sqlite / AutoMigrate 路径：BackfillDefaultScope（本文件，启动与
//     cmd/migrate -auto-migrate 时执行）。
//
// 幂等：只改 NULL/'' 行，重复执行零改动；空库为天然 no-op（空库+既有库
// 两态可跑）。tenant_id 与 workspace_id 两列独立回填，保留部分已有的
// scope 值（如只缺 workspace 的行不会被改写租户）。

// DefaultScopeTenantID / DefaultScopeWorkspaceID 回填目标值（default
// workspace 语义，与计划书 §9.4 口径一致）。
const (
	DefaultScopeTenantID    = "default"
	DefaultScopeWorkspaceID = "default"
)

// ScopeBackfillTables 回填覆盖的核心业务表（计划书 §10 P2 口径：
// conversation / ticket / customer / knowledge / routing）。仅收录实际
// 携带 scope 列的表：ticket_comments/ticket_files 无 tenant_id/workspace_id
// （其 scope 经所属 ticket 传递），不入清单。
var ScopeBackfillTables = []string{
	// conversation
	"sessions",
	"messages",
	"conversation_events",
	// ticket
	"tickets",
	// customer
	"customers",
	// knowledge（文档表）
	"knowledge_docs",
	// routing
	"transfer_records",
	"waiting_records",
	"routing_assignments",
}

// BackfillDefaultScope 为 scope 字段为空的既有行补 default 租户/工作区。
// 返回每张表改动的行数（两列合计）；空库全部为 0。
func BackfillDefaultScope(db *gorm.DB) (map[string]int64, error) {
	result := make(map[string]int64, len(ScopeBackfillTables))
	if db == nil {
		return result, nil
	}
	for _, table := range ScopeBackfillTables {
		var total int64
		for _, column := range []string{"tenant_id", "workspace_id"} {
			value := DefaultScopeTenantID
			if column == "workspace_id" {
				value = DefaultScopeWorkspaceID
			}
			res := db.Table(table).
				Where(fmt.Sprintf("%s IS NULL OR %s = ''", column, column)).
				Update(column, value)
			if res.Error != nil {
				return nil, fmt.Errorf("backfill %s.%s: %w", table, column, res.Error)
			}
			total += res.RowsAffected
		}
		result[table] = total
	}
	return result, nil
}

// sumScopeBackfillCounts 汇总各表改动行数（日志用）。
func sumScopeBackfillCounts(counts map[string]int64) int64 {
	var total int64
	for _, n := range counts {
		total += n
	}
	return total
}
