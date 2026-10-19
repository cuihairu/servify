-- 000027: 聊天平台接入契约 Phase 1 收件箱（docs/chat-platform-integration-
-- contract.md）。外部事件键按 (tenant_id, workspace_id, idempotency_key)
-- 幂等去重，unknown 事件 quarantined 留 triage，来源引用（会话/发送者/消息
-- 平台 id）与原始负载引用随行留存。与
-- platform/channel.ChatConnectorInbox 逐列对齐；sqlite 路径走 AutoMigrate
-- 不经过本文件。
CREATE TABLE IF NOT EXISTS "chat_connector_inboxes" (
  "id" bigserial PRIMARY KEY,
  "tenant_id" text NOT NULL,
  "workspace_id" text NOT NULL,
  "idempotency_key" text NOT NULL,
  "platform" text NOT NULL,
  "installation_id" text NOT NULL,
  "event_id" text NOT NULL,
  "event_type" text NOT NULL,
  "status" text NOT NULL,
  "conversation_platform_id" text NOT NULL,
  "sender_platform_id" text NOT NULL,
  "message_platform_id" text NOT NULL,
  "raw_payload_store" text,
  "raw_payload_object_id" text,
  "occurred_at" timestamptz,
  "received_at" timestamptz,
  "created_at" timestamptz,
  "updated_at" timestamptz
);
CREATE INDEX IF NOT EXISTS "idx_chat_connector_inboxes_installation_id"
  ON "chat_connector_inboxes" ("installation_id");
CREATE UNIQUE INDEX IF NOT EXISTS "uq_chat_connector_inboxes_scope_key"
  ON "chat_connector_inboxes" ("tenant_id", "workspace_id", "idempotency_key");
