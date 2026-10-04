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
	sources   SourceRepository
	provider  knowledgeprovider.KnowledgeProvider
}

func NewService(documents DocumentRepository, indexJobs IndexJobRepository, sources SourceRepository, provider knowledgeprovider.KnowledgeProvider) *Service {
	return &Service{
		documents: documents,
		indexJobs: indexJobs,
		sources:   sources,
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
	if err := s.validateSource(ctx, req.SourceID); err != nil {
		return nil, err
	}
	now := time.Now()
	doc := &domain.Document{
		ID:        strings.TrimSpace(req.ID),
		Title:     title,
		Content:   content,
		Category:  strings.TrimSpace(req.Category),
		Tags:      compact(req.Tags),
		IsPublic:  req.IsPublic,
		SourceID:  req.SourceID,
		Version:   1,
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
	contentChanged := false
	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		if trimmed != doc.Title {
			contentChanged = true
		}
		doc.Title = trimmed
	}
	if req.Content != nil {
		trimmed := strings.TrimSpace(*req.Content)
		if trimmed != doc.Content {
			contentChanged = true
		}
		doc.Content = trimmed
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
	if req.SourceID != nil {
		if err := s.validateSource(ctx, *req.SourceID); err != nil {
			return nil, err
		}
		doc.SourceID = *req.SourceID
	}
	// 版本号（B3-1a §8.2）：标题/内容变更才自增——这两者决定外部索引内容；
	// 纯元数据改动（分类/标签/可见性/来源）不产生新版本。
	if contentChanged {
		doc.Version++
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
		if doc.ProviderID == "" {
			doc.ProviderID = providerName(s.provider)
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
		// 外部映射回存：新 external id 不落库的话，下次重建索引就无从删旧
		// 外部文档（dify/weknora 删旧建新语义失效，外部残留逐次累积）。
		if strings.TrimSpace(externalID) != "" {
			doc.ExternalID = strings.TrimSpace(externalID)
		}
		if err := s.documents.Update(ctx, doc); err != nil {
			return nil, err
		}
	}

	completed := time.Now()
	job.Status = domain.IndexJobDone
	job.Error = ""
	// 版本关联（B3-1a §8.2）：任务落执行时文档版本，管理页可对照"文档当前
	// 版本 vs 已索引版本"发现落后。
	job.DocumentVersion = doc.Version
	job.UpdatedAt = completed
	job.CompletedAt = &completed
	if err := s.indexJobs.Update(ctx, job); err != nil {
		return nil, err
	}
	return &IndexJobResult{
		JobID:           job.ID,
		DocumentID:      job.DocumentID,
		Status:          string(job.Status),
		DocumentVersion: job.DocumentVersion,
		CompletedAt:     job.CompletedAt,
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

// providerName 返回 knowledge_docs.provider_id 的落库值：优先 provider 自报
// （NamedProvider），未实现时回落 "pgvector"（历史默认，保持存量语义）。
func providerName(p knowledgeprovider.KnowledgeProvider) string {
	if named, ok := p.(knowledgeprovider.NamedProvider); ok {
		if name := strings.TrimSpace(named.ProviderName()); name != "" {
			return name
		}
	}
	return "pgvector"
}

func (s *Service) syncDocument(ctx context.Context, doc *domain.Document) error {
	if s.provider == nil || doc == nil {
		return nil
	}

	// 设置 provider ID（外部映射落库字段：与实际驱动一致，未实现 NamedProvider
	// 的 provider 回落历史默认 "pgvector"，存量测试桩与落库值保持不变）
	if doc.ProviderID == "" {
		doc.ProviderID = providerName(s.provider)
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

// validateSource 校验文档挂载的来源登记（B3-1a §8.1）：0=未挂来源合法；
// 非 0 必须已登记（未装配 sources 仓储时报错，避免静默丢归属）。
func (s *Service) validateSource(ctx context.Context, sourceID uint) error {
	if sourceID == 0 {
		return nil
	}
	if s.sources == nil {
		return fmt.Errorf("knowledge sources repository is not configured")
	}
	if _, err := s.sources.Get(ctx, sourceID); err != nil {
		return fmt.Errorf("knowledge source %d not found", sourceID)
	}
	return nil
}

// CreateSource 登记知识来源（§8.1：markdown/website/PDF/FAQ/API 元数据）。
func (s *Service) CreateSource(ctx context.Context, req CreateSourceRequest) (*domain.Source, error) {
	name := strings.TrimSpace(req.Name)
	sourceType := strings.TrimSpace(req.Type)
	if name == "" {
		return nil, fmt.Errorf("source name required")
	}
	if !isKnownSourceType(sourceType) {
		return nil, fmt.Errorf("source type must be one of: %s", strings.Join(domain.SourceTypes, "/"))
	}
	now := time.Now()
	source := &domain.Source{
		Name:        name,
		Type:        sourceType,
		Description: strings.TrimSpace(req.Description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.sources.Create(ctx, source); err != nil {
		return nil, err
	}
	return source, nil
}

// ListSources 列来源登记（可选按 type 过滤）。
func (s *Service) ListSources(ctx context.Context, filter ListSourcesFilter) ([]domain.Source, error) {
	if s.sources == nil {
		return nil, fmt.Errorf("knowledge sources repository is not configured")
	}
	filter.Type = strings.TrimSpace(filter.Type)
	if filter.Type != "" && !isKnownSourceType(filter.Type) {
		return nil, fmt.Errorf("source type must be one of: %s", strings.Join(domain.SourceTypes, "/"))
	}
	return s.sources.List(ctx, filter)
}

// DeleteSource 删除来源登记：仍有文档挂载时拒绝（归属不悬空）。
func (s *Service) DeleteSource(ctx context.Context, id uint) error {
	if s.sources == nil {
		return fmt.Errorf("knowledge sources repository is not configured")
	}
	if _, err := s.sources.Get(ctx, id); err != nil {
		return err
	}
	references, err := s.sources.CountDocuments(ctx, id)
	if err != nil {
		return err
	}
	if references > 0 {
		return fmt.Errorf("knowledge source %d still referenced by %d document(s)", id, references)
	}
	return s.sources.Delete(ctx, id)
}

// ListIndexJobs 按文档列索引任务（§8.2：状态可见），limit 默认 20 上限 100。
func (s *Service) ListIndexJobs(ctx context.Context, documentID string, limit int) ([]IndexJobDTO, error) {
	if strings.TrimSpace(documentID) == "" {
		return nil, fmt.Errorf("document id required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	jobs, err := s.indexJobs.ListByDocument(ctx, documentID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]IndexJobDTO, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, IndexJobDTO{
			ID:              job.ID,
			DocumentID:      job.DocumentID,
			Status:          string(job.Status),
			Error:           job.Error,
			DocumentVersion: job.DocumentVersion,
			CreatedAt:       job.CreatedAt,
			UpdatedAt:       job.UpdatedAt,
			CompletedAt:     job.CompletedAt,
		})
	}
	return out, nil
}

func isKnownSourceType(sourceType string) bool {
	for _, known := range domain.SourceTypes {
		if sourceType == known {
			return true
		}
	}
	return false
}
