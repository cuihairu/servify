-- 000032: 内容快照列为可逆增量；仅完整性/审计用途，回滚按教义仍应走备份。
ALTER TABLE "chat_connector_inboxes" DROP COLUMN IF EXISTS "message_text";
