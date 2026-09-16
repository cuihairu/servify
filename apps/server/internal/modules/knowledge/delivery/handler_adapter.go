package delivery

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	knowledgeinfra "servify/apps/server/internal/modules/knowledge/infra"
	"servify/apps/server/internal/platform/knowledgeprovider"

	"gorm.io/gorm"
)

// HandlerServiceAdapter exposes module-backed knowledge operations to HTTP handlers.
type HandlerServiceAdapter struct {
	service *knowledgeapp.Service
}

func NewHandlerService(db *gorm.DB) *HandlerServiceAdapter {
	return NewHandlerServiceWithProvider(db, nil)
}

func NewHandlerServiceWithProvider(db *gorm.DB, provider knowledgeprovider.KnowledgeProvider) *HandlerServiceAdapter {
	return NewHandlerServiceAdapter(NewService(db, provider))
}

func NewService(db *gorm.DB, provider knowledgeprovider.KnowledgeProvider) *knowledgeapp.Service {
	return knowledgeapp.NewService(
		knowledgeinfra.NewGormDocumentRepository(db),
		knowledgeinfra.NewGormIndexJobRepository(db),
		provider,
	)
}

func NewHandlerServiceAdapter(service *knowledgeapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) List(ctx context.Context, req *knowledgeapp.KnowledgeDocListRequest) ([]knowledgedomain.KnowledgeDoc, int64, error) {
	filter := knowledgeapp.ListDocumentsFilter{}
	if req != nil {
		filter.Page = req.Page
		filter.PageSize = req.PageSize
		filter.Category = req.Category
		filter.Search = req.Search
		filter.PublicOnly = req.PublicOnly
	}
	docs, total, err := a.service.ListDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	out := make([]knowledgedomain.KnowledgeDoc, 0, len(docs))
	for _, doc := range docs {
		model, err := knowledgeDocFromDomain(&doc)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *model)
	}
	return out, total, nil
}

func (a *HandlerServiceAdapter) Get(ctx context.Context, id uint) (*knowledgedomain.KnowledgeDoc, error) {
	doc, err := a.service.GetDocument(ctx, strconv.FormatUint(uint64(id), 10))
	if err != nil {
		return nil, err
	}
	return knowledgeDocFromDomain(doc)
}

func (a *HandlerServiceAdapter) Create(ctx context.Context, req *knowledgeapp.KnowledgeDocCreateRequest) (*knowledgedomain.KnowledgeDoc, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	doc, err := a.service.CreateDocument(ctx, knowledgeapp.CreateDocumentRequest{
		Title:    req.Title,
		Content:  req.Content,
		Category: req.Category,
		Tags:     req.Tags,
		IsPublic: req.IsPublic,
	})
	if err != nil {
		return nil, err
	}
	return knowledgeDocFromDomain(doc)
}

func (a *HandlerServiceAdapter) Update(ctx context.Context, id uint, req *knowledgeapp.KnowledgeDocUpdateRequest) (*knowledgedomain.KnowledgeDoc, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	doc, err := a.service.UpdateDocument(ctx, strconv.FormatUint(uint64(id), 10), knowledgeapp.UpdateDocumentRequest{
		Title:    req.Title,
		Content:  req.Content,
		Category: req.Category,
		Tags:     req.Tags,
		IsPublic: req.IsPublic,
	})
	if err != nil {
		return nil, err
	}
	return knowledgeDocFromDomain(doc)
}

func (a *HandlerServiceAdapter) Delete(ctx context.Context, id uint) error {
	return a.service.DeleteDocument(ctx, strconv.FormatUint(uint64(id), 10))
}

func knowledgeDocFromDomain(doc *knowledgedomain.Document) (*knowledgedomain.KnowledgeDoc, error) {
	if doc == nil {
		return nil, nil
	}
	id, err := strconv.ParseUint(doc.ID, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid document id: %w", err)
	}
	return &knowledgedomain.KnowledgeDoc{
		ID:        uint(id),
		Title:     doc.Title,
		Content:   doc.Content,
		Category:  doc.Category,
		Tags:      joinTagsCSV(doc.Tags),
		IsPublic:  doc.IsPublic,
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}, nil
}

func joinTagsCSV(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return strings.Join(out, ",")
}
