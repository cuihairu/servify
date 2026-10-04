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
	// SourceID 来源登记（V1.0 收敛 B3-1a，docs/v1-convergence-plan.md §8.1）：
	// 挂 knowledge_sources 元数据，0=未归属来源。
	SourceID uint `gorm:"index;default:0" json:"source_id,omitempty"`
	// Version 文档版本号（§8.2）：内容/元数据变更自增，索引任务关联执行时版本。
	Version int `gorm:"default:1" json:"version"`
	// 新增字段 - pgvector 支持
	Embedding  Embedding `gorm:"type:vector(1536)" json:"embedding,omitempty"`
	ChunkIndex int       `gorm:"default:0" json:"chunk_index,omitempty"`
	DocChunkID string    `gorm:"index" json:"doc_chunk_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// KnowledgeIndexJob 知识库索引任务（GORM 持久化模型）。
type KnowledgeIndexJob struct {
	ID         string `gorm:"primaryKey" json:"id"`
	DocumentID uint   `gorm:"index;not null" json:"document_id"`
	Status     string `gorm:"index;not null" json:"status"`
	Error      string `gorm:"type:text" json:"error"`
	// DocumentVersion 任务完成时索引的文档版本（§8.2：index_jobs 关联版本），
	// 0=尚未执行到写回阶段（排队/运行中/失败未及回存）。
	DocumentVersion int        `gorm:"default:0" json:"document_version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// KnowledgeSource 知识来源登记（§8.1：markdown/website/PDF/FAQ/API 的元数据，
// 文档挂 source）。V1 只做登记与归属，不按 source 定制 provider 行为。
type KnowledgeSource struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    string    `gorm:"index" json:"tenant_id"`
	WorkspaceID string    `gorm:"index" json:"workspace_id"`
	Name        string    `json:"name"`
	Type        string    `gorm:"index" json:"type"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
