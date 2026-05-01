package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

type Service struct {
	documents DocumentRepository
	indexJobs IndexJobRepository
	provider  knowledgeprovider.KnowledgeProvider
}

func NewService(documents DocumentRepository, indexJobs IndexJobRepository, provider knowledgeprovider.KnowledgeProvider) *Service {
	return &Service{
		documents: documents,
		indexJobs: indexJobs,
		provider:  provider,
	}
}

func (s *Service) CreateDocument(ctx context.Context, req CreateDocumentRequest) (*domain.Document, error) {
	title := strings.TrimSpace(req.Title)
	content := strings.TrimSpace(req.Content)
	if title == "" {
		return nil, fmt.Errorf("title required")
	}
	if content == "" {
		return nil, fmt.Errorf("content required")
	}
	now := time.Now()
	doc := &domain.Document{
		ID:        strings.TrimSpace(req.ID),
		Title:     title,
		Content:   content,
		Category:  strings.TrimSpace(req.Category),
		Tags:      compact(req.Tags),
		IsPublic:  req.IsPublic,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.documents.Create(ctx, doc); err != nil {
		return nil, err
	}
	if err := s.syncDocument(ctx, doc); err != nil {
		return nil, err
	}
	if err := s.documents.Update(ctx, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *Service) UpdateDocument(ctx context.Context, id string, req UpdateDocumentRequest) (*domain.Document, error) {
	doc, err := s.documents.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Title != nil {
		doc.Title = strings.TrimSpace(*req.Title)
	}
	if req.Content != nil {
		doc.Content = strings.TrimSpace(*req.Content)
	}
	if req.Category != nil {
		doc.Category = strings.TrimSpace(*req.Category)
	}
	if req.Tags != nil {
		doc.Tags = compact(*req.Tags)
	}
	if req.IsPublic != nil {
		doc.IsPublic = *req.IsPublic
	}
	if doc.Title == "" {
		return nil, fmt.Errorf("title required")
	}
	if doc.Content == "" {
		return nil, fmt.Errorf("content required")
	}
	doc.UpdatedAt = time.Now()
	if err := s.documents.Update(ctx, doc); err != nil {
		return nil, err
	}
	if err := s.syncDocument(ctx, doc); err != nil {
		return nil, err
	}
	if err := s.documents.Update(ctx, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *Service) DeleteDocument(ctx context.Context, id string) error {
	doc, err := s.documents.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.provider != nil {
		externalID := strings.TrimSpace(doc.ExternalID)
		if externalID == "" {
			return fmt.Errorf("knowledge provider deletion is not supported: missing external document id")
		}
		if err := s.provider.DeleteDocument(ctx, externalID); err != nil {
			if errors.Is(err, knowledgeprovider.ErrOperationNotSupported) {
				return fmt.Errorf("knowledge provider deletion is not supported: %w", err)
			}
			return err
		}
	}
	return s.documents.Delete(ctx, id)
}

func (s *Service) GetDocument(ctx context.Context, id string) (*domain.Document, error) {
	return s.documents.Get(ctx, id)
}

func (s *Service) ListDocuments(ctx context.Context, filter ListDocumentsFilter) ([]domain.Document, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return s.documents.List(ctx, filter)
}

func (s *Service) QueueIndexJob(ctx context.Context, req QueueIndexJobRequest) (*domain.IndexJob, error) {
	if strings.TrimSpace(req.DocumentID) == "" {
		return nil, fmt.Errorf("document id required")
	}
	now := time.Now()
	job := &domain.IndexJob{
		ID:         strings.TrimSpace(req.JobID),
		DocumentID: strings.TrimSpace(req.DocumentID),
		Status:     domain.IndexJobQueued,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.indexJobs.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Service) RunIndexJob(ctx context.Context, req RunIndexJobRequest) (*IndexJobResult, error) {
	job, err := s.indexJobs.Get(ctx, req.JobID)
	if err != nil {
		return nil, err
	}
	doc, err := s.documents.Get(ctx, job.DocumentID)
	if err != nil {
		return nil, err
	}

	job.Status = domain.IndexJobRunning
	job.UpdatedAt = time.Now()
	if err := s.indexJobs.Update(ctx, job); err != nil {
		return nil, err
	}

	if s.provider != nil {
		externalID, err := s.provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
			ID:         doc.ID,
			ProviderID: doc.ProviderID,
			ExternalID: doc.ExternalID,
			Title:      doc.Title,
			Content:    doc.Content,
			Tags:       doc.Tags,
			Metadata:   map[string]interface{}{"category": doc.Category},
		})
		if err != nil {
			job.Status = domain.IndexJobFailed
			job.Error = err.Error()
			job.UpdatedAt = time.Now()
			_ = s.indexJobs.Update(ctx, job)
			return &IndexJobResult{
				JobID:      job.ID,
				DocumentID: job.DocumentID,
				Status:     string(job.Status),
				Error:      job.Error,
			}, err
		}
		if strings.TrimSpace(externalID) != "" {
			doc.ExternalID = strings.TrimSpace(externalID)
		}
	}

	completed := time.Now()
	job.Status = domain.IndexJobDone
	job.Error = ""
	job.UpdatedAt = completed
	job.CompletedAt = &completed
	if err := s.indexJobs.Update(ctx, job); err != nil {
		return nil, err
	}
	return &IndexJobResult{
		JobID:       job.ID,
		DocumentID:  job.DocumentID,
		Status:      string(job.Status),
		CompletedAt: job.CompletedAt,
	}, nil
}

func compact(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (s *Service) syncDocument(ctx context.Context, doc *domain.Document) error {
	if s.provider == nil || doc == nil {
		return nil
	}

	// 设置 provider ID
	if doc.ProviderID == "" {
		doc.ProviderID = "pgvector"
	}

	externalID, err := s.provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ID:         doc.ID,
		ProviderID: doc.ProviderID,
		ExternalID: doc.ExternalID,
		Title:      doc.Title,
		Content:    doc.Content,
		Tags:       doc.Tags,
		Metadata:   map[string]interface{}{"category": doc.Category},
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(externalID) != "" {
		doc.ExternalID = strings.TrimSpace(externalID)
	}
	return nil
}

func providerIdentity(provider knowledgeprovider.KnowledgeProvider) string {
	switch provider.(type) {
	case nil:
		return ""
	default:
		t := fmt.Sprintf("%T", provider)
		t = strings.TrimPrefix(t, "*")
		return strings.TrimSpace(t)
	}
}
