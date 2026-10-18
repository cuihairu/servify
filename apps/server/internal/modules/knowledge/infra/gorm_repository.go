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

type GormKnowledgeCatalogRepository struct{ db *gorm.DB }

func NewGormKnowledgeCatalogRepository(db *gorm.DB) *GormKnowledgeCatalogRepository {
	return &GormKnowledgeCatalogRepository{db: db}
}

func (r *GormKnowledgeCatalogRepository) Create(ctx context.Context, base *domain.KnowledgeBase) error {
	if base == nil {
		return gorm.ErrInvalidValue
	}
	tenantID, workspaceID := platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
	if tenantID == "" || workspaceID == "" {
		return fmt.Errorf("tenant and workspace scope required")
	}
	model := &domain.KnowledgeBase{TenantID: tenantID, WorkspaceID: workspaceID, Name: base.Name, IsPublic: base.IsPublic, CreatedAt: base.CreatedAt, UpdatedAt: base.UpdatedAt}
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	base.ID = model.ID
	return nil
}

func (r *GormKnowledgeCatalogRepository) List(ctx context.Context) ([]domain.KnowledgeBase, error) {
	tenantID, workspaceID := platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
	if tenantID == "" || workspaceID == "" {
		return nil, fmt.Errorf("tenant and workspace scope required")
	}
	var models []domain.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND workspace_id = ?", tenantID, workspaceID).Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]domain.KnowledgeBase, 0, len(models))
	for _, m := range models {
		out = append(out, domain.KnowledgeBase{ID: m.ID, Name: m.Name, IsPublic: m.IsPublic, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt})
	}
	return out, nil
}

func (r *GormKnowledgeCatalogRepository) ListByDocument(ctx context.Context, documentID string) ([]domain.Chunk, error) {
	id, err := parseDocumentID(documentID)
	if err != nil {
		return nil, err
	}
	var rows []domain.KnowledgeChunk
	tenantID, workspaceID := platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
	if tenantID == "" || workspaceID == "" {
		return nil, fmt.Errorf("tenant and workspace scope required")
	}
	q := r.db.WithContext(ctx).Joins("JOIN knowledge_docs ON knowledge_docs.id = knowledge_chunks.document_id").Where("knowledge_docs.tenant_id = ? AND knowledge_docs.workspace_id = ?", tenantID, workspaceID)
	if err := q.Where("knowledge_chunks.document_id = ?", id).Order("knowledge_chunks.sequence ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Chunk, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Chunk{ID: row.ID, DocumentID: row.DocumentID, Sequence: row.Sequence, Content: row.Content, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (r *GormDocumentRepository) Create(ctx context.Context, doc *domain.Document) error {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	if tenantID == "" {
		tenantID = "default"
	}
	if workspaceID == "" {
		workspaceID = "default"
	}
	model := &domain.KnowledgeDoc{
		TenantID:        tenantID,
		WorkspaceID:     workspaceID,
		ProviderID:      doc.ProviderID,
		ExternalID:      doc.ExternalID,
		Title:           doc.Title,
		Content:         doc.Content,
		Category:        doc.Category,
		Tags:            strings.Join(doc.Tags, ","),
		IsPublic:        doc.IsPublic,
		KnowledgeBaseID: doc.KnowledgeBaseID,
		SourceID:        doc.SourceID,
		Version:         doc.Version,
		CreatedAt:       doc.CreatedAt,
		UpdatedAt:       doc.UpdatedAt,
	}
	db := r.db.WithContext(ctx)
	base := &domain.KnowledgeBase{TenantID: tenantID, WorkspaceID: workspaceID, Name: "Default"}
	if doc.KnowledgeBaseID != 0 {
		if err := db.Where("id = ? AND tenant_id = ? AND workspace_id = ?", doc.KnowledgeBaseID, tenantID, workspaceID).First(base).Error; err != nil {
			return gorm.ErrRecordNotFound
		}
	} else if err := db.Where("tenant_id = ? AND workspace_id = ? AND name = ?", tenantID, workspaceID, "Default").FirstOrCreate(base).Error; err != nil {
		return err
	}
	model.KnowledgeBaseID = base.ID
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(model).Error; err != nil {
			return err
		}
		return tx.Create(&domain.KnowledgeChunk{DocumentID: model.ID, Sequence: model.ChunkIndex, Content: model.Content, Embedding: model.Embedding, CreatedAt: model.CreatedAt}).Error
	}); err != nil {
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
		"source_id":   doc.SourceID,
		"version":     doc.Version,
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
		"document_id":      model.DocumentID,
		"status":           model.Status,
		"error":            model.Error,
		"document_version": model.DocumentVersion,
		"updated_at":       model.UpdatedAt,
		"completed_at":     model.CompletedAt,
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

// ListByDocument 按文档列索引任务，新任务在前（B3-1a §8.2：索引状态可见）。
func (r *GormIndexJobRepository) ListByDocument(ctx context.Context, documentID string, limit int) ([]domain.IndexJob, error) {
	docID, err := parseDocumentID(documentID)
	if err != nil {
		return nil, err
	}
	var rows []domain.KnowledgeIndexJob
	if err := r.db.WithContext(ctx).
		Where("document_id = ?", docID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.IndexJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, *indexJobFromModel(row))
	}
	return out, nil
}

func documentFromModel(model domain.KnowledgeDoc) *domain.Document {
	return &domain.Document{
		ID:              strconv.FormatUint(uint64(model.ID), 10),
		KnowledgeBaseID: model.KnowledgeBaseID,
		ProviderID:      model.ProviderID,
		ExternalID:      model.ExternalID,
		Title:           model.Title,
		Content:         model.Content,
		Category:        model.Category,
		Tags:            splitTags(model.Tags),
		IsPublic:        model.IsPublic,
		SourceID:        model.SourceID,
		Version:         model.Version,
		CreatedAt:       model.CreatedAt,
		UpdatedAt:       model.UpdatedAt,
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
		ID:              strings.TrimSpace(job.ID),
		DocumentID:      documentID,
		Status:          string(job.Status),
		Error:           job.Error,
		DocumentVersion: job.DocumentVersion,
		CreatedAt:       job.CreatedAt,
		UpdatedAt:       job.UpdatedAt,
		CompletedAt:     job.CompletedAt,
	}, nil
}

func indexJobFromModel(model domain.KnowledgeIndexJob) *domain.IndexJob {
	return &domain.IndexJob{
		ID:              model.ID,
		DocumentID:      strconv.FormatUint(uint64(model.DocumentID), 10),
		Status:          domain.IndexJobStatus(model.Status),
		Error:           model.Error,
		DocumentVersion: model.DocumentVersion,
		CreatedAt:       model.CreatedAt,
		UpdatedAt:       model.UpdatedAt,
		CompletedAt:     model.CompletedAt,
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
