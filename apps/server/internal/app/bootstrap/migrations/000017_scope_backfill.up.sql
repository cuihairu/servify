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
--
-- 前置建表（2026-10-07 CI Integration 红修复）：conversation_events（B1 事件
-- 流水）与 routing_assignments（B2-1 评分审计）两个模型进了 MigrationModels
-- 冻结清单却漏写各自承诺的编号迁移——sqlite/AutoMigrate 测试路径会建表掩盖
-- 缺口，postgres 版本化路径全新库上本迁移的 UPDATE 直接 42P01，迁移被标脏
-- v17 后容器致命循环起不来。编号序不可变（v17 先于任何新迁移执行），只能在
-- 本迁移就地补建：DDL 取自 cmd/gen-baseline 对空库的真实捕获（与基线同源），
-- IF NOT EXISTS 保证既有库零改动；模型清单与迁移链的整类缺口由
-- TestMigrationChainCoversAllModels 静态对账把关。

-- conversation：B1 会话服务过程事件流水（模型漏配编号迁移，此处补建）
CREATE TABLE IF NOT EXISTS "conversation_events" ("id" bigserial,"tenant_id" text,"workspace_id" text,"conversation_id" varchar(64) NOT NULL,"event_type" varchar(64) NOT NULL,"actor_type" varchar(32) DEFAULT '',"actor_id" varchar(64) DEFAULT '',"summary" text,"payload" text,"occurred_at" timestamptz NOT NULL,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_conv_events_conversation" ON "conversation_events" ("conversation_id","event_type");
CREATE INDEX IF NOT EXISTS "idx_conv_events_scope" ON "conversation_events" ("tenant_id","workspace_id");

-- routing：B2-1 路由分配评分审计（模型漏配编号迁移，此处补建）
CREATE TABLE IF NOT EXISTS "routing_assignments" ("id" bigserial,"tenant_id" text,"workspace_id" text,"session_id" text,"from_agent_id" bigint,"to_agent_id" bigint,"total_score" decimal,"factors" text,"reasons" text,"strategy" text,"assigned_at" timestamptz,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_routing_assignments_to_agent_id" ON "routing_assignments" ("to_agent_id");
CREATE INDEX IF NOT EXISTS "idx_routing_assignments_from_agent_id" ON "routing_assignments" ("from_agent_id");
CREATE INDEX IF NOT EXISTS "idx_routing_assignments_session_id" ON "routing_assignments" ("session_id");
CREATE INDEX IF NOT EXISTS "idx_routing_assignments_scope" ON "routing_assignments" ("tenant_id","workspace_id");

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
