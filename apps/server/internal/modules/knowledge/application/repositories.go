package application

import (
	"context"

	"servify/apps/server/internal/modules/knowledge/domain"
)

type DocumentRepository interface {
	Create(ctx context.Context, doc *domain.Document) error
	Update(ctx context.Context, doc *domain.Document) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*domain.Document, error)
	List(ctx context.Context, filter ListDocumentsFilter) ([]domain.Document, int64, error)
}

type IndexJobRepository interface {
	Create(ctx context.Context, job *domain.IndexJob) error
	Update(ctx context.Context, job *domain.IndexJob) error
	Get(ctx context.Context, id string) (*domain.IndexJob, error)
	// ListByDocument 按文档列索引任务（B3-1a §8.2：状态可见），新任务在前。
	ListByDocument(ctx context.Context, documentID string, limit int) ([]domain.IndexJob, error)
}

// SourceRepository 来源登记仓储（B3-1a §8.1）。
type SourceRepository interface {
	Create(ctx context.Context, source *domain.Source) error
	Update(ctx context.Context, source *domain.Source) error
	Delete(ctx context.Context, id uint) error
	Get(ctx context.Context, id uint) (*domain.Source, error)
	List(ctx context.Context, filter ListSourcesFilter) ([]domain.Source, error)
	// CountDocuments 引用该来源的文档数（删除前守卫用）。
	CountDocuments(ctx context.Context, sourceID uint) (int64, error)
}
