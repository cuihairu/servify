package delivery

import (
	"context"

	"servify/apps/server/internal/models"
	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
)

// HandlerService is the only knowledge contract that HTTP handlers should depend on.
type HandlerService interface {
	List(ctx context.Context, req *knowledgeapp.KnowledgeDocListRequest) ([]models.KnowledgeDoc, int64, error)
	Get(ctx context.Context, id uint) (*models.KnowledgeDoc, error)
	Create(ctx context.Context, req *knowledgeapp.KnowledgeDocCreateRequest) (*models.KnowledgeDoc, error)
	Update(ctx context.Context, id uint, req *knowledgeapp.KnowledgeDocUpdateRequest) (*models.KnowledgeDoc, error)
	Delete(ctx context.Context, id uint) error
}
