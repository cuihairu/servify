// Package qualitydomain 是 quality 模块自有的 GORM 持久化模型。
// 历史上位于 internal/models，P1-7 边界收口迁入模块（legacy 侧通过
// internal/models 的类型别名过渡引用）。
//
// 包声明名用唯一的 qualitydomain 而非 domain：quality_handler.go 的 swag
// 注解（@Success 200 {object} models.QualityReview）会经 models 的类型
// 别名解析到本包，swag 按包声明名索引限定符、不认 import 别名，跨模块
// 重名的 domain 声明无法定位（与 modules/customer/api 声明 customerapi
// 同一约束，见 docs/modules-dependency-map.md 第 1 节）。
package qualitydomain

import "time"

// QualityReview 是单个已结束会话的质检记录：规则违规 + LLM 打分 + 人工复核。
// session_id 唯一是 worker 幂等闩；JSON 列一律 type:text（sqlite/pg 双轨方言安全）。
type QualityReview struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	TenantID        string     `gorm:"index:idx_quality_reviews_scope" json:"tenant_id"`
	WorkspaceID     string     `gorm:"index:idx_quality_reviews_scope" json:"workspace_id"`
	SessionID       string     `gorm:"uniqueIndex:uniq_quality_reviews_session;not null" json:"session_id"`
	CustomerID      *uint      `gorm:"index" json:"customer_id"`
	AgentID         *uint      `gorm:"index" json:"agent_id"`                                            // users.id，与 Session.AgentID 同语义
	Status          string     `gorm:"index:idx_quality_reviews_status;default:'pending'" json:"status"` // pending|skipped|scored|failed|confirmed
	Trigger         string     `gorm:"default:'worker'" json:"trigger"`                                  // worker|manual|rescore
	MessageCount    int        `json:"message_count"`
	DurationSeconds int        `json:"duration_seconds"`
	ViolationsJSON  string     `gorm:"type:text" json:"violations_json"`
	ViolationCount  int        `json:"violation_count"`
	MaxSeverity     string     `gorm:"index" json:"max_severity"` // ''|low|medium|high
	DimensionsJSON  string     `gorm:"type:text" json:"dimensions_json"`
	LLMTotalScore   *float64   `json:"llm_total_score"`
	LLMSummary      string     `gorm:"type:text" json:"llm_summary"`
	LLMProvider     string     `json:"llm_provider"`
	LLMModel        string     `json:"llm_model"`
	AttemptCount    int        `json:"attempt_count"`
	NextRetryAt     *time.Time `gorm:"index:idx_quality_reviews_retry" json:"next_retry_at"`
	LastError       string     `gorm:"type:text" json:"last_error"`
	ManualScore     *float64   `json:"manual_score"`
	ManualResult    string     `json:"manual_result"` // ''|pass|violation
	ReviewNote      string     `gorm:"type:text" json:"review_note"`
	ReviewedBy      *uint      `json:"reviewed_by"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	ScoredAt        *time.Time `json:"scored_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
