package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"servify/apps/server/internal/models"
	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	knowledgeinfra "servify/apps/server/internal/modules/knowledge/infra"

	"gorm.io/gorm"
)

type KnowledgeDocService struct {
	module *knowledgeapp.Service
}

func NewKnowledgeDocService(db *gorm.DB) *KnowledgeDocService {
	return &KnowledgeDocService{
		module: knowledgeapp.NewService(
			knowledgeinfra.NewGormDocumentRepository(db),
			knowledgeinfra.NewGormIndexJobRepository(db),
			nil,
		),
	}
}

// KnowledgeDoc 请求契约定义已迁至 modules/knowledge/application，
// 此处保留类型别名供 legacy 引用方使用。
type KnowledgeDocCreateRequest = knowledgeapp.KnowledgeDocCreateRequest

type KnowledgeDocUpdateRequest = knowledgeapp.KnowledgeDocUpdateRequest

type KnowledgeDocListRequest = knowledgeapp.KnowledgeDocListRequest

func (s *KnowledgeDocService) Create(ctx context.Context, req *KnowledgeDocCreateRequest) (*models.KnowledgeDoc, error) {
	if req == nil {
		return nil, errors.New("request required")
	}
	doc, err := s.module.CreateDocument(ctx, knowledgeapp.CreateDocumentRequest{
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

func (s *KnowledgeDocService) Get(ctx context.Context, id uint) (*models.KnowledgeDoc, error) {
	doc, err := s.module.GetDocument(ctx, strconv.FormatUint(uint64(id), 10))
	if err != nil {
		return nil, err
	}
	return knowledgeDocFromDomain(doc)
}

func (s *KnowledgeDocService) List(ctx context.Context, req *KnowledgeDocListRequest) ([]models.KnowledgeDoc, int64, error) {
	filter := knowledgeapp.ListDocumentsFilter{}
	if req != nil {
		filter.Page = req.Page
		filter.PageSize = req.PageSize
		filter.Category = req.Category
		filter.Search = req.Search
		filter.PublicOnly = req.PublicOnly
	}
	docs, total, err := s.module.ListDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	out := make([]models.KnowledgeDoc, 0, len(docs))
	for _, doc := range docs {
		model, err := knowledgeDocFromDomain(&doc)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *model)
	}
	return out, total, nil
}

func (s *KnowledgeDocService) Update(ctx context.Context, id uint, req *KnowledgeDocUpdateRequest) (*models.KnowledgeDoc, error) {
	if req == nil {
		return nil, errors.New("request required")
	}
	doc, err := s.module.UpdateDocument(ctx, strconv.FormatUint(uint64(id), 10), knowledgeapp.UpdateDocumentRequest{
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

func (s *KnowledgeDocService) Delete(ctx context.Context, id uint) error {
	return s.module.DeleteDocument(ctx, strconv.FormatUint(uint64(id), 10))
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

func knowledgeDocFromDomain(doc *knowledgedomain.Document) (*models.KnowledgeDoc, error) {
	if doc == nil {
		return nil, nil
	}
	id, err := strconv.ParseUint(doc.ID, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid document id: %w", err)
	}
	return &models.KnowledgeDoc{
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
