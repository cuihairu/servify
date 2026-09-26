// Package assistdomain 是 assist 模块自有的 GORM 持久化模型。
// 历史上位于 internal/models，P1-7 边界收口迁入模块（legacy 侧通过
// internal/models 的类型别名过渡引用）。
//
// 包声明名用唯一的 assistdomain 而非 domain：assist_handler.go 与
// assist_recording_handler.go 的 swag 注解（@Success {object} models.RemoteAssistX）
// 会经 models 的类型别名解析到本包，swag 按包声明名索引限定符、不认
// import 别名，跨模块重名的 domain 声明无法定位（与 customerapi /
// qualitydomain 同一约束，见 docs/modules-dependency-map.md 第 1 节）。
package assistdomain

import "time"

// RemoteAssistSession 远程协助会话（信令走 WS/RTC，本表承载审计、录制元数据与标注锚点）。
type RemoteAssistSession struct {
	ID                    uint       `gorm:"primaryKey" json:"id"`
	TenantID              string     `json:"tenant_id"`
	WorkspaceID           string     `json:"workspace_id"`
	ConversationSessionID string     `gorm:"index" json:"conversation_session_id"`
	AgentUserID           uint       `json:"agent_user_id"`
	Status                string     `json:"status"` // active|ended|failed
	StartedAt             time.Time  `json:"started_at"`
	EndedAt               *time.Time `json:"ended_at,omitempty"`
	// 对方同意状态：pending（发起后待访客表态）/ granted / declined。
	// 存量行为空串 = 未走同意流程（兼容期与 pending 同权放行录制回写）。
	ConsentStatus string     `json:"consent_status"`
	ConsentAt     *time.Time `json:"consent_at,omitempty"`
	// 录制元数据：文件经既有 /api/v1/upload 上传，这里只落 key 与展示信息
	RecordingKey        string    `json:"recording_key,omitempty"`
	RecordingMime       string    `json:"recording_mime,omitempty"`
	RecordingDurationMs int64     `json:"recording_duration_ms,omitempty"`
	RecordingSize       int64     `json:"recording_size,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// RemoteAssistAnnotation 远程协助标注（Canvas 覆盖层逐笔落库，坐标 JSON）。
type RemoteAssistAnnotation struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	AssistSessionID uint      `gorm:"not null;index:idx_assist_annotations_session" json:"assist_session_id"`
	TimestampMs     int64     `json:"timestamp_ms"`
	Shape           string    `json:"shape"` // rect|freehand|arrow
	Payload         string    `gorm:"type:text" json:"payload"`
	CreatedBy       uint      `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}
