package knowledgeprovider

import (
	"context"
	"errors"
)

var ErrOperationNotSupported = errors.New("knowledge provider operation is not supported")

// KnowledgeProvider defines the contract for retrieval and indexing providers.
type KnowledgeProvider interface {
	Search(ctx context.Context, req SearchRequest) ([]KnowledgeHit, error)
	UpsertDocument(ctx context.Context, doc KnowledgeDocument) (string, error)
	DeleteDocument(ctx context.Context, id string) error
	HealthCheck(ctx context.Context) error
}

// RebuildableProvider is an optional extension for providers that can rebuild an index.
type RebuildableProvider interface {
	RebuildIndex(ctx context.Context, req RebuildRequest) error
}

// NamedProvider is an optional extension for providers that declare their own
// identity; the value lands in knowledge_docs.provider_id (API 透传的落库映射
// 字段)。未实现的 provider 由应用层回落 "pgvector"（历史默认），保证既有
// 测试桩与存量落库值不变。
type NamedProvider interface {
	ProviderName() string
}
