package delivery

import (
	"context"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
)

// HandlerService is the only knowledge contract that HTTP handlers should depend on.
type HandlerService interface {
	List(ctx context.Context, req *knowledgeapp.KnowledgeDocListRequest) ([]knowledgedomain.KnowledgeDoc, int64, error)
	Get(ctx context.Context, id uint) (*knowledgedomain.KnowledgeDoc, error)
	Create(ctx context.Context, req *knowledgeapp.KnowledgeDocCreateRequest) (*knowledgedomain.KnowledgeDoc, error)
	Update(ctx context.Context, id uint, req *knowledgeapp.KnowledgeDocUpdateRequest) (*knowledgedomain.KnowledgeDoc, error)
	Delete(ctx context.Context, id uint) error

	// 来源登记与索引任务读口（B3-1a §8.1/§8.2）。
	ListSources(ctx context.Context, sourceType string) ([]knowledgedomain.KnowledgeSource, error)
	CreateSource(ctx context.Context, req *KnowledgeSourceCreateRequest) (*knowledgedomain.KnowledgeSource, error)
	DeleteSource(ctx context.Context, id uint) error
	ListIndexJobs(ctx context.Context, documentID string, limit int) ([]knowledgeapp.IndexJobDTO, error)
	IndexDocument(ctx context.Context, documentID string) (*knowledgeapp.IndexJobResult, error)
	RetryIndexJob(ctx context.Context, jobID string) (*knowledgeapp.IndexJobResult, error)
}

// Request contract aliases: handlers may not import the application package
// directly (module-boundaries), so they consume these delivery-level names.
type (
	KnowledgeDocCreateRequest = knowledgeapp.KnowledgeDocCreateRequest
	KnowledgeDocUpdateRequest = knowledgeapp.KnowledgeDocUpdateRequest
	KnowledgeDocListRequest   = knowledgeapp.KnowledgeDocListRequest
	IndexJobDTO               = knowledgeapp.IndexJobDTO
	IndexJobResult            = knowledgeapp.IndexJobResult
)

// KnowledgeSourceCreateRequest 来源登记请求契约（B3-1a §8.1）。
type KnowledgeSourceCreateRequest struct {
	Name        string `json:"name" binding:"required"`
	Type        string `json:"type" binding:"required"`
	Description string `json:"description"`
}
