package domain

import "time"

// KnowledgeDoc 知识库文档（GORM 持久化模型）。历史上位于 internal/models，
// P1-7 边界收口迁入模块（legacy 侧通过 internal/models 的类型别名过渡引用）。
// knowledge 的 handler 无 swag 注解，本包声明保持通用的 domain（约束见
// docs/modules-dependency-map.md 第 1 节）。
type KnowledgeDoc struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TenantID    string `gorm:"index" json:"tenant_id"`
	WorkspaceID string `gorm:"index" json:"workspace_id"`
	ProviderID  string `gorm:"index" json:"provider_id"`
	ExternalID  string `gorm:"index" json:"external_id"`
	Title       string `json:"title"`
	Content     string `gorm:"type:text" json:"content"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	IsPublic    bool   `gorm:"default:false;index" json:"is_public"`
	// 新增字段 - pgvector 支持
	Embedding  Embedding `gorm:"type:vector(1536)" json:"embedding,omitempty"`
	ChunkIndex int       `gorm:"default:0" json:"chunk_index,omitempty"`
	DocChunkID string    `gorm:"index" json:"doc_chunk_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// KnowledgeIndexJob 知识库索引任务（GORM 持久化模型）。
type KnowledgeIndexJob struct {
	ID          string     `gorm:"primaryKey" json:"id"`
	DocumentID  uint       `gorm:"index;not null" json:"document_id"`
	Status      string     `gorm:"index;not null" json:"status"`
	Error       string     `gorm:"type:text" json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
