package infra

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	"servify/apps/server/internal/modules/knowledge/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

type GormDocumentRepository struct {
	db *gorm.DB
}

func NewGormDocumentRepository(db *gorm.DB) *GormDocumentRepository {
	return &GormDocumentRepository{db: db}
}

func (r *GormDocumentRepository) Create(ctx context.Context, doc *domain.Document) error {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	model := &domain.KnowledgeDoc{
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		ProviderID:  doc.ProviderID,
		ExternalID:  doc.ExternalID,
		Title:       doc.Title,
		Content:     doc.Content,
		Category:    doc.Category,
		Tags:        strings.Join(doc.Tags, ","),
		IsPublic:    doc.IsPublic,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	doc.ID = strconv.FormatUint(uint64(model.ID), 10)
	return nil
}

func (r *GormDocumentRepository) Update(ctx context.Context, doc *domain.Document) error {
	id, err := parseDocumentID(doc.ID)
	if err != nil {
		return err
	}
	result := applyKnowledgeScope(r.db.WithContext(ctx).Model(&domain.KnowledgeDoc{}), ctx).Where("id = ?", id).Updates(map[string]interface{}{
		"provider_id": doc.ProviderID,
		"external_id": doc.ExternalID,
		"title":       doc.Title,
		"content":     doc.Content,
		"category":    doc.Category,
		"tags":        strings.Join(doc.Tags, ","),
		"is_public":   doc.IsPublic,
		"updated_at":  doc.UpdatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormDocumentRepository) Delete(ctx context.Context, id string) error {
	docID, err := parseDocumentID(id)
	if err != nil {
		return err
	}
	result := applyKnowledgeScope(r.db.WithContext(ctx), ctx).Delete(&domain.KnowledgeDoc{}, docID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormDocumentRepository) Get(ctx context.Context, id string) (*domain.Document, error) {
	docID, err := parseDocumentID(id)
	if err != nil {
		return nil, err
	}
	var model domain.KnowledgeDoc
	if err := applyKnowledgeScope(r.db.WithContext(ctx), ctx).First(&model, docID).Error; err != nil {
		return nil, err
	}
	return documentFromModel(model), nil
}

func (r *GormDocumentRepository) List(ctx context.Context, filter knowledgeapp.ListDocumentsFilter) ([]domain.Document, int64, error) {
	q := applyKnowledgeScope(r.db.WithContext(ctx).Model(&domain.KnowledgeDoc{}), ctx)
	if filter.PublicOnly {
		q = q.Where("is_public = ?", true)
	}
	if c := strings.TrimSpace(filter.Category); c != "" {
		q = q.Where("category = ?", c)
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		like := "%" + s + "%"
		q = q.Where("title LIKE ? OR content LIKE ? OR tags LIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.PageSize
	var rows []domain.KnowledgeDoc
	if err := q.Order("created_at DESC").Limit(filter.PageSize).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	out := make([]domain.Document, 0, len(rows))
	for _, row := range rows {
		out = append(out, *documentFromModel(row))
	}
	return out, total, nil
}

type GormIndexJobRepository struct {
	db *gorm.DB
}

func NewGormIndexJobRepository(db *gorm.DB) *GormIndexJobRepository {
	return &GormIndexJobRepository{db: db}
}

func (r *GormIndexJobRepository) Create(ctx context.Context, job *domain.IndexJob) error {
	if job == nil {
		return fmt.Errorf("index job required")
	}
	model, err := indexJobModelFromDomain(job)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	job.ID = model.ID
	return nil
}

func (r *GormIndexJobRepository) Update(ctx context.Context, job *domain.IndexJob) error {
	if job == nil {
		return fmt.Errorf("index job required")
	}
	model, err := indexJobModelFromDomain(job)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&domain.KnowledgeIndexJob{}).Where("id = ?", model.ID).Updates(map[string]interface{}{
		"document_id":  model.DocumentID,
		"status":       model.Status,
		"error":        model.Error,
		"updated_at":   model.UpdatedAt,
		"completed_at": model.CompletedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormIndexJobRepository) Get(ctx context.Context, id string) (*domain.IndexJob, error) {
	var model domain.KnowledgeIndexJob
	if err := r.db.WithContext(ctx).First(&model, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return nil, err
	}
	return indexJobFromModel(model), nil
}

func documentFromModel(model domain.KnowledgeDoc) *domain.Document {
	return &domain.Document{
		ID:         strconv.FormatUint(uint64(model.ID), 10),
		ProviderID: model.ProviderID,
		ExternalID: model.ExternalID,
		Title:      model.Title,
		Content:    model.Content,
		Category:   model.Category,
		Tags:       splitTags(model.Tags),
		IsPublic:   model.IsPublic,
		CreatedAt:  model.CreatedAt,
		UpdatedAt:  model.UpdatedAt,
	}
}

func indexJobModelFromDomain(job *domain.IndexJob) (*domain.KnowledgeIndexJob, error) {
	if job == nil {
		return nil, fmt.Errorf("index job required")
	}
	documentID, err := parseDocumentID(job.DocumentID)
	if err != nil {
		return nil, err
	}
	return &domain.KnowledgeIndexJob{
		ID:          strings.TrimSpace(job.ID),
		DocumentID:  documentID,
		Status:      string(job.Status),
		Error:       job.Error,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		CompletedAt: job.CompletedAt,
	}, nil
}

func indexJobFromModel(model domain.KnowledgeIndexJob) *domain.IndexJob {
	return &domain.IndexJob{
		ID:          model.ID,
		DocumentID:  strconv.FormatUint(uint64(model.DocumentID), 10),
		Status:      domain.IndexJobStatus(model.Status),
		Error:       model.Error,
		CreatedAt:   model.CreatedAt,
		UpdatedAt:   model.UpdatedAt,
		CompletedAt: model.CompletedAt,
	}
}

func parseDocumentID(raw string) (uint, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid document id")
	}
	return uint(id), nil
}

func applyKnowledgeScope(tx *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	if tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
