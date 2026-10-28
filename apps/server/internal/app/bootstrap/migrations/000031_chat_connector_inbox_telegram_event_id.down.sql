-- 000031 down: 把迁移改写的 chat 作用 Telegram EventID 还原为裸 messageID。
-- 唯一安全口径：event_id 含 ':' 且冒号后全数字的 telegram message.created 行
-- （迁移只产出该形态）。幂等键还原为 installation\x1f<messageID>。注意该
-- 反向并非普遍可逆：不同 chat 同号 message 还原后会共享同一幂等键而冲突
-- （这正是格式变更的动机），故 down 仅在回滚方确认无此碰撞时安全——仓库
-- 的回滚口径本就是恢复备份（migrate_runner.go），down 文件仅供完整性/审计。
UPDATE "chat_connector_inboxes"
SET event_id = split_part(event_id, ':', 2),
    idempotency_key = installation_id || chr(31) || split_part(event_id, ':', 2)
WHERE platform = 'telegram'
  AND event_type = 'message.created'
  AND event_id LIKE '%:%'
  AND split_part(event_id, ':', 2) ~ '^[0-9]+$'
  AND split_part(event_id, ':', 3) = '';
