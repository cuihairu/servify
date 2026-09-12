-- 000005: 坐席组（P2 坐席组层级 + 亲和路由）
-- 与 models.AgentGroup / models.AgentGroupMember 逐列对齐；
-- waiting_records 追加 target_group_id / claimed_at（C5 队列认领租约用）。

CREATE TABLE IF NOT EXISTS "agent_groups" ("id" bigserial,"created_at" timestamptz,"updated_at" timestamptz,"deleted_at" timestamptz,"tenant_id" text,"workspace_id" text,"name" text NOT NULL,"description" text,"priority" integer,"overflow_policy" text DEFAULT 'global',"enabled" boolean DEFAULT true,"parent_id" bigint,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "uniq_agent_groups_name" ON "agent_groups" ("tenant_id","workspace_id","name");
CREATE INDEX IF NOT EXISTS "idx_agent_groups_scope" ON "agent_groups" ("tenant_id","workspace_id");
CREATE INDEX IF NOT EXISTS "idx_agent_groups_deleted_at" ON "agent_groups" ("deleted_at");

CREATE TABLE IF NOT EXISTS "agent_group_members" ("id" bigserial,"created_at" timestamptz,"group_id" bigint NOT NULL,"agent_user_id" bigint NOT NULL,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "uniq_agent_group_members" ON "agent_group_members" ("group_id","agent_user_id");
CREATE INDEX IF NOT EXISTS "idx_agent_group_members_group_id" ON "agent_group_members" ("group_id");

ALTER TABLE "waiting_records" ADD COLUMN IF NOT EXISTS "target_group_id" bigint;
ALTER TABLE "waiting_records" ADD COLUMN IF NOT EXISTS "claimed_at" timestamptz;
CREATE INDEX IF NOT EXISTS "idx_waiting_records_claim" ON "waiting_records" ("claimed_at");
