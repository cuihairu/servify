-- 000031: Telegram EventID 旧格式回填（docs/chat-platform-integration-
-- contract.md）。EventID 由裸 messageID 改为 chatID:messageID（telegram/
-- adapter.go 事件标识组合）后，此前已落 inbox 的旧格式行未被迁移：同一
-- update 崩溃重投会重算为新键、判为新行再次 commit 并重复 forward
-- （at-least-once 失效）；已 committed-but-unforwarded 旧行重放则因
-- IdempotencyKey 不匹配返回 ErrReplayMismatch。本迁移把旧行重写为
-- chatID:messageID 格式（event_id 与 idempotency_key 同步），恢复去重与重放
-- 契约。与 bootstrap.BackfillTelegramEventIDs（sqlite/AutoMigrate 路径）同
-- 语义；sqlite 不经过本文件。
--
-- 幂等：只改 event_id 匹配 ^[0-9]+$（裸 message id）的 telegram
-- message.created 行，重复执行零改动；空库为天然 no-op。旧行重写目标键已被
-- chat 作用行占用（该 update 在格式改动前后各落过一次）时删旧行——存活的
-- chat 作用行才是进入正常流的那次，删除后其留存对象由 retention 清扫。
-- updated_at 不更新（回填惯例，与 000017_scope_backfill 一致）。

-- 目标键已被 chat 作用行占用的旧行删除（否则重写触发唯一索引冲突）。
DELETE FROM "chat_connector_inboxes" AS legacy
USING "chat_connector_inboxes" AS scoped
WHERE legacy.platform = 'telegram'
  AND legacy.event_type = 'message.created'
  AND legacy.event_id ~ '^[0-9]+$'
  AND scoped.platform = 'telegram'
  AND scoped.event_type = 'message.created'
  AND scoped.event_id = legacy.conversation_platform_id || ':' || legacy.event_id
  AND scoped.tenant_id = legacy.tenant_id
  AND scoped.workspace_id = legacy.workspace_id
  AND scoped.installation_id = legacy.installation_id;

-- 其余旧行重写为 chat 作用格式（幂等键 = installation\x1f<chatID:messageID>）。
UPDATE "chat_connector_inboxes"
SET event_id = conversation_platform_id || ':' || event_id,
    idempotency_key = installation_id || chr(31) || conversation_platform_id || ':' || event_id
WHERE platform = 'telegram'
  AND event_type = 'message.created'
  AND event_id ~ '^[0-9]+$';
