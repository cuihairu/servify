-- 000032: 聊天平台隔离审核内容快照。规范化 message text 有界随 inbox 行
-- 留存，未授权会话即使关闭原始负载留存仍有 triage 证据；完整原始字节仍
-- 只属于 raw payload 引用。与 platform/channel.ChatConnectorInbox 对齐。
ALTER TABLE "chat_connector_inboxes" ADD COLUMN IF NOT EXISTS "message_text" text;
