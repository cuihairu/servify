-- 000029: 聊天平台接入契约 安装映射存储（docs/chat-platform-integration-
-- contract.md：tenant/workspace 作用域只从已存安装映射取，绝不信任入站载荷）。
-- 表只存凭据的引用（环境变量名），不存凭据本身——契约要求凭据由运行时环境
-- 或本机 secret store 供给，行内容可安全入日志与支持包。一行对应一个安装，
-- Enabled 默认 false（fail-closed：存了不等于跑）。conversation 三个字段内嵌
-- 是因为一个安装只映射一个会话（Memex update 不带群组 id，2026-10-20 已代决）。
-- 与 platform/channel 的 ChatConnectorInstallation 逐列对齐；sqlite 路径走
-- AutoMigrate 不经过本文件。
CREATE TABLE IF NOT EXISTS "chat_connector_installations" (
  "id" bigserial PRIMARY KEY,
  "tenant_id" text NOT NULL,
  "workspace_id" text NOT NULL,
  "platform" text NOT NULL,
  "installation_id" text NOT NULL,
  "conversation_platform_id" text NOT NULL,
  "conversation_kind" text NOT NULL,
  "conversation_is_group" boolean NOT NULL,
  "updates_endpoint" text NOT NULL,
  "token_env_var" text NOT NULL,
  "long_poll_ms" integer NOT NULL DEFAULT 25000,
  "enabled" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz,
  "updated_at" timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS "uq_chat_connector_installations_scope"
  ON "chat_connector_installations" ("tenant_id", "workspace_id", "platform", "installation_id");
