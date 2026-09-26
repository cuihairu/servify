// Package domain 持有翻译模块的持久化模型（pg 版本化迁移与 sqlite
// AutoMigrate 双轨共用；pg 建表见 bootstrap/migrations/000015）。
package domain

import "time"

// TranslationLanguagePreference 会话翻译语言偏好
// （docs/realtime-translation-design.md Phase 1）：每个客服会话至多一行，
// target_lang 为该会话消息自动翻译的目标语言（BCP-47 常用子集，小写
// 规范化）。hub 在消息落库后按该偏好异步翻译并广播 message-translated 帧。
type TranslationLanguagePreference struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	TenantID              string    `gorm:"index:idx_translation_prefs_scope" json:"tenant_id"`
	WorkspaceID           string    `gorm:"index:idx_translation_prefs_scope" json:"workspace_id"`
	ConversationSessionID string    `gorm:"uniqueIndex" json:"conversation_session_id"`
	TargetLang            string    `json:"target_lang"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}
