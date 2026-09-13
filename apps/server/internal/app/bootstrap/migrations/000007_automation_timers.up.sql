-- 000007: 自动化 delay 动作定时器（delay 入队 → timer worker 到期抢占执行）
-- 与 models.AutomationTimer 逐列对齐；actions_json 是到期后执行的嵌套动作快照，
-- 不回查触发器当前定义（触发器随后被删/改不影响已入队执行单）。

CREATE TABLE IF NOT EXISTS "automation_timers" ("id" bigserial,"trigger_id" bigint,"ticket_id" bigint,"actions_json" text,"due_at" timestamptz,"status" text DEFAULT 'pending',"last_error" text,"created_at" timestamptz,"executed_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_automation_timers_status_due_at" ON "automation_timers" ("status","due_at");
CREATE INDEX IF NOT EXISTS "idx_automation_timers_trigger_id" ON "automation_timers" ("trigger_id");
CREATE INDEX IF NOT EXISTS "idx_automation_timers_ticket_id" ON "automation_timers" ("ticket_id");
