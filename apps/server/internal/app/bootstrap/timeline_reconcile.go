package bootstrap

import (
	"gorm.io/gorm"
)

// ReconcileConversationTimeline 回填缺失的 conversation.created 时间线投影行
// （todo 197）：业务写入与事件发布之间没有事务边界——会话落库后、发布前
// 崩溃，时间线就永久缺「会话创建」行（sessions 表是事实源，该事件无法从
// 流重放，因为它从未进过流）。启动期以 sessions 表对账：为没有 created 投影
// 行的会话补一行，event_id 确定性生成（reconcile:conversation-created:<id>），
// actor/summary 与事件总线投影注册表口径一致（system / 会话创建），
// occurred_at 拷贝 started_at。幂等谓词是 (conversation_id, event_type)
// 不存在——旧格式 NULL event_id 的投影行同样视为已覆盖，重复执行零新增。
// 对账在两条 schema 路径（versioned/automigrate）迁移完成后运行，覆盖
// postgres 与 sqlite。
func ReconcileConversationTimeline(db *gorm.DB) (int64, error) {
	// 无表即无事可对账：skip 模式的自管库（含尚未建表的全新库）本就没有
	// 会话与时间线，静默返回 0；表齐但 SQL 失败仍照常上抛。
	if !db.Migrator().HasTable("conversation_events") || !db.Migrator().HasTable("sessions") {
		return 0, nil
	}
	res := db.Exec(`
INSERT INTO conversation_events
	(event_id, tenant_id, workspace_id, conversation_id, event_type, actor_type, summary, payload, occurred_at)
SELECT
	'reconcile:conversation-created:' || s.id,
	s.tenant_id,
	s.workspace_id,
	s.id,
	'conversation.created',
	'system',
	'会话创建',
	'',
	s.started_at
FROM sessions s
WHERE NOT EXISTS (
	SELECT 1 FROM conversation_events e
	WHERE e.conversation_id = s.id AND e.event_type = 'conversation.created'
)`)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
