package domain

import "time"

// V1.0 收敛 B3-1b（docs/v1-convergence-plan.md §3.1/§5.3/§8.3）：AI 首答
// 持久化与反馈闭环的数据模型。

// AIAnswer 一次 AI 首答的持久化记录（ai_answers）：查询/答案/置信/产生方式
// 与引用来源快照。反馈闭环（answer_feedback）与检索分析（top 问答、无命中
// 率、低置信率）都锚在这张表上。
type AIAnswer struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TenantID    string `gorm:"index" json:"tenant_id"`
	WorkspaceID string `gorm:"index" json:"workspace_id"`
	// SessionID 来源会话（REST 直问可空：无会话上下文的管理面查询）。
	SessionID  string  `gorm:"index" json:"session_id"`
	Query      string  `gorm:"type:text" json:"query"`
	Answer     string  `gorm:"type:text" json:"answer"`
	Confidence float64 `json:"confidence"`
	// Strategy 答案产生方式：weknora/dify/llm/fallback/transfer（与
	// ai/delivery AIResponse.Strategy 同口径）。
	Strategy string `gorm:"index" json:"strategy"`
	// SourcesJSON 引用来源快照（[]SourceSnapshot 序列化），空串=零命中。
	SourcesJSON string    `gorm:"type:text" json:"-"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

// SourceSnapshot 落库的引用来源精简快照：不存 content 全文（答案本身已在
// Answer 列），只存展示与回链所需字段。
type SourceSnapshot struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Source     string  `json:"source,omitempty"`
	Score      float64 `json:"score"`
}

// AnswerFeedback 对一次 AI 首答的评价（answer_feedback，§5.3）："是否有
// 帮助" + 可选意见。同一答案允许多条（不同主体/不同时点），不做 upsert。
type AnswerFeedback struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TenantID    string `gorm:"index" json:"tenant_id"`
	WorkspaceID string `gorm:"index" json:"workspace_id"`
	AnswerID    uint   `gorm:"index;not null" json:"answer_id"`
	// Helpful true=有帮助 / false=没帮助。
	Helpful bool   `json:"helpful"`
	Comment string `gorm:"type:text" json:"comment,omitempty"`
	// CreatedBy 提交主体标识（用户名或访客会话），审计回看用。
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
