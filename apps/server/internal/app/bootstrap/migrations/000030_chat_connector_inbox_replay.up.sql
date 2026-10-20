-- 000030: 聊天平台接入契约 replay 解锁面（docs/chat-platform-integration-
-- contract.md）。forwarded_at 记账已提交行何时进入正常流（NULL = replay 泳道：
-- 行已持久但会话摄入未settled）；raw_payload_sha256 把留存对象的明文摘要钉在
-- 行上（对象 AES-256-GCM 密封，行侧只能验摘要——换对象即被抓）。与
-- platform/channel.ChatConnectorInbox 逐列对齐；sqlite 路径走 AutoMigrate
-- 不经过本文件。
ALTER TABLE "chat_connector_inboxes" ADD COLUMN IF NOT EXISTS "forwarded_at" timestamptz;
ALTER TABLE "chat_connector_inboxes" ADD COLUMN IF NOT EXISTS "raw_payload_sha256" text;
