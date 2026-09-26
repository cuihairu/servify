// Package domain 持有翻译模块的持久化模型（pg 版本化迁移与 sqlite
// AutoMigrate 双轨共用；pg 建表见 bootstrap/migrations/000015、角色维度
// 见 000016）。
package domain

import "time"

// 翻译偏好的 viewer 角色：同一会话两个读向各自一条偏好——agent 角色是
// 坐席读译文的目标语言（访客 → 坐席方向），visitor 角色是访客读译文的
// 目标语言（坐席 → 访客方向）。
const (
	ViewerRoleAgent   = "agent"
	ViewerRoleVisitor = "visitor"
)

// TranslationLanguagePreference 会话翻译语言偏好
// （docs/realtime-translation-design.md Phase 1）：(conversation_session_id,
// viewer_role) 至多一行，target_lang 为该方向消息自动翻译的目标语言
// （BCP-47 常用子集，小写规范化）。hub / 坐席发送口在消息落库后按该偏好
// 异步翻译并广播 message-translated 帧。
type TranslationLanguagePreference struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	TenantID              string    `gorm:"index:idx_translation_prefs_scope" json:"tenant_id"`
	WorkspaceID           string    `gorm:"index:idx_translation_prefs_scope" json:"workspace_id"`
	ConversationSessionID string    `gorm:"uniqueIndex:idx_translation_prefs_session_viewer" json:"conversation_session_id"`
	ViewerRole            string    `gorm:"uniqueIndex:idx_translation_prefs_session_viewer" json:"viewer_role"`
	TargetLang            string    `json:"target_lang"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}
