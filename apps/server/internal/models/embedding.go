package models

import (
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
)

// Embedding 是 pgvector 向量列的容错值对象（空向量写 NULL，扫描容忍
// nil / "" / "[]"）。定义已迁至 modules/knowledge/domain（它只服务于
// KnowledgeDoc 的向量列，随 P1-7 knowledge 刀迁移），此处保留类型别名
// 供 legacy 引用方（pgvector provider 等）过渡使用。
type Embedding = knowledgedomain.Embedding

// NewEmbedding 转发到 knowledgedomain.NewEmbedding，legacy 调用方过渡保留。
func NewEmbedding(vec []float32) Embedding {
	return knowledgedomain.NewEmbedding(vec)
}
