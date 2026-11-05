-- 2026-11-06: 工单创建原子化与幂等（todo.md 工单创建原子化和幂等）。
--
-- ① tickets.idempotency_key：请求幂等键（调用方稳定重试锚点）。可空文本，
--    仅创建路径写入；同 scope（tenant_id, workspace_id, idempotency_key）
--    唯一约束把同键重试收敛到已建工单（不产生重复单）。NULL 不受唯一约束
--    影响（postgres 允许多 NULL），无幂等键的旧路径不受影响。
-- ② ticket_outbox：工单可靠事件 outbox。工单行、初始状态记录与事件行在同一
--    DB 事务提交；worker 异步领取 pending 行投递到事件总线，按 event_id
--    全局唯一去重（重复投递不会发布两次）。状态机 pending --worker-->
--    delivered / failed(带 next_attempt_at 回到 pending) / dead。
--
-- 与 models.Ticket / models.TicketOutbox 逐列对齐；sqlite 路径走 AutoMigrate
-- 不经过本文件。
ALTER TABLE "tickets" ADD COLUMN IF NOT EXISTS "idempotency_key" text;

CREATE UNIQUE INDEX IF NOT EXISTS "uq_tickets_scope_idempotency_key" ON "tickets" ("tenant_id","workspace_id","idempotency_key");

CREATE TABLE IF NOT EXISTS "ticket_outboxes" ("id" bigserial,"event_id" varchar(128) NOT NULL,"event_name" text NOT NULL,"aggregate_id" varchar(128),"payload" text,"status" text DEFAULT 'pending',"attempts" bigint DEFAULT 0,"last_error" text,"created_at" timestamptz,"updated_at" timestamptz,"delivered_at" timestamptz,"next_attempt_at" timestamptz,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "uq_ticket_outbox_event_id" ON "ticket_outboxes" ("event_id");
CREATE INDEX IF NOT EXISTS "idx_ticket_outbox_status_next_attempt" ON "ticket_outboxes" ("status","next_attempt_at");
