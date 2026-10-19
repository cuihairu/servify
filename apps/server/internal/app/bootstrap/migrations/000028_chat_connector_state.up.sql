-- 000028: 聊天平台接入契约 Phase 1 能力快照与游标/ACK 状态
-- （docs/chat-platform-integration-contract.md）。能力快照按 (tenant,
-- workspace, platform, installation, conversation) 逐群留存最近一次平台授权
-- 观察（fail-closed：无快照或 unknown 权限一律禁摄入）；游标表按 (tenant,
-- workspace, platform, installation) 保存轮询/流式拉取位置，空游标=清空重来。
-- 与 platform/channel 的 ChatConnectorCapability / ChatConnectorCursor 逐列
-- 对齐；sqlite 路径走 AutoMigrate 不经过本文件。
CREATE TABLE IF NOT EXISTS "chat_connector_capabilities" (
  "id" bigserial PRIMARY KEY,
  "tenant_id" text NOT NULL,
  "workspace_id" text NOT NULL,
  "platform" text NOT NULL,
  "installation_id" text NOT NULL,
  "conversation_platform_id" text NOT NULL,
  "receive_mode" text NOT NULL,
  "message_scope" text NOT NULL,
  "history_read" boolean NOT NULL DEFAULT false,
  "event_types_json" text NOT NULL,
  "threads" boolean NOT NULL DEFAULT false,
  "replies" boolean NOT NULL DEFAULT false,
  "attachments" boolean NOT NULL DEFAULT false,
  "edits" boolean NOT NULL DEFAULT false,
  "deletes" boolean NOT NULL DEFAULT false,
  "reactions" boolean NOT NULL DEFAULT false,
  "send_text" boolean NOT NULL DEFAULT false,
  "send_rich" boolean NOT NULL DEFAULT false,
  "rate_limit" text NOT NULL,
  "verified_at" timestamptz,
  "created_at" timestamptz,
  "updated_at" timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS "uq_chat_connector_capabilities_scope"
  ON "chat_connector_capabilities" ("tenant_id", "workspace_id", "platform", "installation_id", "conversation_platform_id");

CREATE TABLE IF NOT EXISTS "chat_connector_cursors" (
  "id" bigserial PRIMARY KEY,
  "tenant_id" text NOT NULL,
  "workspace_id" text NOT NULL,
  "platform" text NOT NULL,
  "installation_id" text NOT NULL,
  "cursor" text NOT NULL,
  "created_at" timestamptz,
  "updated_at" timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS "uq_chat_connector_cursors_scope"
  ON "chat_connector_cursors" ("tenant_id", "workspace_id", "platform", "installation_id");
