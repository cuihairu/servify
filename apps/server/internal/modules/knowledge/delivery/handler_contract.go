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
}

// Request contract aliases: handlers may not import the application package
// directly (module-boundaries), so they consume these delivery-level names.
type (
	KnowledgeDocCreateRequest = knowledgeapp.KnowledgeDocCreateRequest
	KnowledgeDocUpdateRequest = knowledgeapp.KnowledgeDocUpdateRequest
	KnowledgeDocListRequest   = knowledgeapp.KnowledgeDocListRequest
)
