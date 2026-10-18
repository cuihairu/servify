package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/knowledge/domain"
)

// CatalogService manages independently addressable knowledge collections and chunks.
type CatalogService struct {
	bases  KnowledgeBaseRepository
	chunks ChunkRepository
}

func NewCatalogService(bases KnowledgeBaseRepository, chunks ChunkRepository) *CatalogService {
	return &CatalogService{bases: bases, chunks: chunks}
}

func (s *CatalogService) CreateKnowledgeBase(ctx context.Context, name string, isPublic bool) (*domain.KnowledgeBase, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("knowledge base name required")
	}
	base := &domain.KnowledgeBase{Name: name, IsPublic: isPublic, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.bases.Create(ctx, base); err != nil {
		return nil, err
	}
	return base, nil
}

func (s *CatalogService) ListKnowledgeBases(ctx context.Context) ([]domain.KnowledgeBase, error) {
	return s.bases.List(ctx)
}

func (s *CatalogService) ListChunks(ctx context.Context, documentID string) ([]domain.Chunk, error) {
	if strings.TrimSpace(documentID) == "" {
		return nil, fmt.Errorf("document id required")
	}
	return s.chunks.ListByDocument(ctx, documentID)
}
