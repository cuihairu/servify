-- V1.0 收敛 B2-4（docs/v1-convergence-plan.md §9.4/§10 P2）：核心业务表
-- tenant_id/workspace_id 回填——default workspace 语义。历史版本写入的行
-- 可能留空 scope（创建早于租户过滤接线、或写入路径无租户上下文），回填后
-- 这些行获得确定的归属。
--
-- 幂等：只改 NULL/'' 行，重复执行零改动；空库为天然 no-op（空库+既有库
-- 两态可跑）。tenant_id 与 workspace_id 两列独立回填，保留部分已有的 scope。
--
-- 表清单与 internal/app/bootstrap/scope_backfill.go 的 ScopeBackfillTables
-- 保持一致（该处有同步测试 TestScopeBackfillSQLCoversSameTables 把关）。

-- conversation
UPDATE sessions SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE sessions SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';
UPDATE messages SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE messages SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';
UPDATE conversation_events SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE conversation_events SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';

-- ticket（ticket_comments/ticket_files 无 scope 列，scope 经所属 ticket 传递）
UPDATE tickets SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE tickets SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';

-- customer
UPDATE customers SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE customers SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';

-- knowledge（文档表）
UPDATE knowledge_docs SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE knowledge_docs SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';

-- routing
UPDATE transfer_records SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE transfer_records SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';
UPDATE waiting_records SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE waiting_records SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';
UPDATE routing_assignments SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE routing_assignments SET workspace_id = 'default' WHERE workspace_id IS NULL OR workspace_id = '';
