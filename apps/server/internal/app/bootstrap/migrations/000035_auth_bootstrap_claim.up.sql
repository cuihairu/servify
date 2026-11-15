-- 2026-11-15: 首个管理员引导原子占位（Codex 深审 Critical-1：并发注册可双 admin）。
--
-- auth_bootstrap_claims：固定主键 id=1 的单行占位表。公开注册的 admin 引导在
-- 同一事务内先插占位行，主键冲突 = 引导已被并发请求消费 → 调用方降级
-- customer。占位行只在 admin 用户随事务成功创建后存活；回滚即释放占位。
-- 事后删光用户也不重开注册引导（管理员恢复走种子 DB 更新）。
--
-- 与 models.AuthBootstrapClaim 逐列对齐；sqlite 路径走 AutoMigrate 不经过本文件。
CREATE TABLE IF NOT EXISTS "auth_bootstrap_claims" ("id" bigserial,"claimed_at" timestamptz,PRIMARY KEY ("id"));