package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/knowledgeprovider"
	memorykp "servify/apps/server/internal/platform/knowledgeprovider/memory"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
)

type memDocRepo struct {
	docs map[string]*domain.Document
}

func (r *memDocRepo) Create(ctx context.Context, doc *domain.Document) error {
	if r.docs == nil {
		r.docs = map[string]*domain.Document{}
	}
	if doc.ID == "" {
		doc.ID = fmt.Sprintf("doc-%d", len(r.docs)+1)
	}
	cp := *doc
	r.docs[doc.ID] = &cp
	return nil
}
func (r *memDocRepo) Update(ctx context.Context, doc *domain.Document) error {
	cp := *doc
	r.docs[doc.ID] = &cp
	return nil
}
func (r *memDocRepo) Delete(ctx context.Context, id string) error {
	delete(r.docs, id)
	return nil
}
func (r *memDocRepo) Get(ctx context.Context, id string) (*domain.Document, error) {
	doc, ok := r.docs[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *doc
	return &cp, nil
}
func (r *memDocRepo) List(ctx context.Context, filter ListDocumentsFilter) ([]domain.Document, int64, error) {
	out := make([]domain.Document, 0, len(r.docs))
	for _, doc := range r.docs {
		if filter.Category != "" && doc.Category != filter.Category {
			continue
		}
		if filter.PublicOnly && !doc.IsPublic {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(doc.Title+doc.Content), strings.ToLower(filter.Search)) {
			continue
		}
		out = append(out, *doc)
	}
	return out, int64(len(out)), nil
}

type memJobRepo struct {
	jobs map[string]*domain.IndexJob
}

func (r *memJobRepo) Create(ctx context.Context, job *domain.IndexJob) error {
	if r.jobs == nil {
		r.jobs = map[string]*domain.IndexJob{}
	}
	if job.ID == "" {
		job.ID = fmt.Sprintf("job-%d", len(r.jobs)+1)
	}
	cp := *job
	r.jobs[job.ID] = &cp
	return nil
}
func (r *memJobRepo) Update(ctx context.Context, job *domain.IndexJob) error {
	cp := *job
	r.jobs[job.ID] = &cp
	return nil
}
func (r *memJobRepo) Get(ctx context.Context, id string) (*domain.IndexJob, error) {
	job, ok := r.jobs[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *job
	return &cp, nil
}
func (r *memJobRepo) ListByDocument(ctx context.Context, documentID string, limit int) ([]domain.IndexJob, error) {
	out := make([]domain.IndexJob, 0)
	for _, job := range r.jobs {
		if job.DocumentID == documentID {
			out = append(out, *job)
		}
	}
	return out, nil
}

func TestServiceCreateDocument(t *testing.T) {
	provider := &mockkp.Provider{}
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil, provider)
	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		Title:    " KB Title ",
		Content:  " KB Content ",
		Tags:     []string{" a ", "", "b"},
		IsPublic: true,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if doc.ID == "" {
		t.Fatal("expected generated id")
	}
	if len(doc.Tags) != 2 {
		t.Fatalf("unexpected tags: %+v", doc.Tags)
	}
	if !doc.IsPublic {
		t.Fatal("expected document to keep public flag")
	}
	if provider.Documents[doc.ID].Title != "KB Title" {
		t.Fatalf("expected provider sync on create, got %+v", provider.Documents)
	}
	if doc.ExternalID == "" {
		t.Fatalf("expected external id to be recorded, got %+v", doc)
	}
}

func TestServiceUpdateDocumentSyncsProvider(t *testing.T) {
	provider := &mockkp.Provider{}
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil, provider)

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Old content",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}

	newTitle := "Billing Updated"
	newContent := "New content"
	newTags := []string{"faq", "billing"}
	updated, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{
		Title:   &newTitle,
		Content: &newContent,
		Tags:    &newTags,
	})
	if err != nil {
		t.Fatalf("update doc: %v", err)
	}

	if updated.Title != newTitle || updated.Content != newContent {
		t.Fatalf("unexpected updated doc: %+v", updated)
	}
	if provider.Documents[doc.ID].Title != newTitle || provider.Documents[doc.ID].Content != newContent {
		t.Fatalf("expected provider sync on update, got %+v", provider.Documents)
	}
	if updated.ExternalID == "" {
		t.Fatalf("expected external id to remain populated, got %+v", updated)
	}
}

func TestServiceDeleteDocumentSyncsProvider(t *testing.T) {
	provider := &trackingDeleteProvider{}
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil, provider)

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Billing details",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}

	if err := svc.DeleteDocument(context.Background(), doc.ID); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	if provider.deletedID != doc.ExternalID {
		t.Fatalf("expected provider delete to use external id %q, got %q", doc.ExternalID, provider.deletedID)
	}
	if _, ok := provider.Documents[doc.ID]; ok {
		t.Fatalf("expected provider delete, got %+v", provider.Documents)
	}
	if _, err := svc.GetDocument(context.Background(), doc.ID); err == nil {
		t.Fatal("expected repository delete")
	}
}

func TestServiceDeleteDocumentFailsWhenProviderDeletionUnsupported(t *testing.T) {
	provider := &unsupportedDeleteProvider{}
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil, provider)

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Billing details",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}

	err = svc.DeleteDocument(context.Background(), doc.ID)
	if err == nil {
		t.Fatal("expected delete error when provider deletion unsupported")
	}
	if !errors.Is(err, knowledgeprovider.ErrOperationNotSupported) {
		t.Fatalf("expected unsupported operation error, got %v", err)
	}
	if _, err := svc.GetDocument(context.Background(), doc.ID); err != nil {
		t.Fatalf("expected repository document to remain after failed delete, got %v", err)
	}
}

func TestServiceRunIndexJob(t *testing.T) {
	docRepo := &memDocRepo{}
	jobRepo := &memJobRepo{}
	provider := &mockkp.Provider{}
	svc := NewService(docRepo, jobRepo, nil, provider)

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Billing details",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	job, err := svc.QueueIndexJob(context.Background(), QueueIndexJobRequest{
		JobID:      "job-1",
		DocumentID: doc.ID,
	})
	if err != nil {
		t.Fatalf("queue job: %v", err)
	}
	result, err := svc.RunIndexJob(context.Background(), RunIndexJobRequest{JobID: job.ID})
	if err != nil {
		t.Fatalf("run job: %v", err)
	}
	if result.Status != string(domain.IndexJobDone) {
		t.Fatalf("unexpected status: %+v", result)
	}
	if provider.Documents["doc-1"].Title != "Billing" {
		t.Fatalf("expected provider upsert, got %+v", provider.Documents)
	}
}

func TestServiceListDocuments(t *testing.T) {
	docRepo := &memDocRepo{}
	svc := NewService(docRepo, &memJobRepo{}, nil, nil)
	_, _ = svc.CreateDocument(context.Background(), CreateDocumentRequest{ID: "doc-1", Title: "Billing", Content: "billing content", Category: "faq"})
	_, _ = svc.CreateDocument(context.Background(), CreateDocumentRequest{ID: "doc-2", Title: "Support", Content: "support content", Category: "guide", IsPublic: true})

	docs, total, err := svc.ListDocuments(context.Background(), ListDocumentsFilter{
		Category: "faq",
	})
	if err != nil {
		t.Fatalf("list docs: %v", err)
	}
	if total != 1 || len(docs) != 1 {
		t.Fatalf("unexpected docs: total=%d docs=%d", total, len(docs))
	}

	docs, total, err = svc.ListDocuments(context.Background(), ListDocumentsFilter{
		PublicOnly: true,
	})
	if err != nil {
		t.Fatalf("list public docs: %v", err)
	}
	if total != 1 || len(docs) != 1 || docs[0].ID != "doc-2" {
		t.Fatalf("unexpected public docs: total=%d docs=%+v", total, docs)
	}
}

func TestServiceRunIndexJobProviderSwitchRegression(t *testing.T) {
	providers := map[string]knowledgeprovider.KnowledgeProvider{
		"mock":   &mockkp.Provider{},
		"memory": memorykp.NewProvider("tenant-a", "kb-a"),
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			docRepo := &memDocRepo{}
			jobRepo := &memJobRepo{}
			svc := NewService(docRepo, jobRepo, nil, provider)

			doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
				ID:      "doc-switch",
				Title:   "Billing",
				Content: "Billing details",
			})
			if err != nil {
				t.Fatalf("create doc: %v", err)
			}
			job, err := svc.QueueIndexJob(context.Background(), QueueIndexJobRequest{
				JobID:      "job-switch",
				DocumentID: doc.ID,
			})
			if err != nil {
				t.Fatalf("queue job: %v", err)
			}
			result, err := svc.RunIndexJob(context.Background(), RunIndexJobRequest{JobID: job.ID})
			if err != nil {
				t.Fatalf("run job: %v", err)
			}
			if result.Status != string(domain.IndexJobDone) {
				t.Fatalf("unexpected status: %+v", result)
			}
		})
	}
}

var _ knowledgeprovider.KnowledgeProvider = (*mockkp.Provider)(nil)

type unsupportedDeleteProvider struct {
	mockkp.Provider
}

func (p *unsupportedDeleteProvider) DeleteDocument(ctx context.Context, id string) error {
	return knowledgeprovider.ErrOperationNotSupported
}

type trackingDeleteProvider struct {
	mockkp.Provider
	deletedID string
}

func (p *trackingDeleteProvider) DeleteDocument(ctx context.Context, id string) error {
	p.deletedID = id
	return p.Provider.DeleteDocument(ctx, id)
}

// namedStubProvider 自报 provider 名（NamedProvider，模拟 dify/weknora 驱动）。
type namedStubProvider struct {
	mockkp.Provider
	name string
}

func (p *namedStubProvider) ProviderName() string { return p.name }

// reindexingProvider 记录每次 Upsert 收到的映射并返回全新 external id
// （模拟 ragflow/dify/weknora 删旧建新后外部侧的新文档 id）。
type reindexingProvider struct {
	mockkp.Provider
	calls []knowledgeprovider.KnowledgeDocument
}

func (p *reindexingProvider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	p.calls = append(p.calls, doc)
	return fmt.Sprintf("ext-%d", len(p.calls)), nil
}

// TestServiceSyncDocumentLabelsProviderID P1-1 外部映射一致性：provider_id
// 落库值为实际驱动自报名（不再硬编码 "pgvector"），更新路径保持既有标签。
func TestServiceSyncDocumentLabelsProviderID(t *testing.T) {
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil, &namedStubProvider{name: "dify"})
	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Billing details",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if doc.ProviderID != "dify" {
		t.Fatalf("provider id = %q, want dify", doc.ProviderID)
	}

	updated, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{})
	if err != nil {
		t.Fatalf("update doc: %v", err)
	}
	if updated.ProviderID != "dify" {
		t.Fatalf("provider id after update = %q, want dify (label sticky)", updated.ProviderID)
	}
}

// TestProviderNameFallback 未实现 NamedProvider 或自报名为空白时回落历史
// 默认 "pgvector"（存量测试桩与落库值语义不变）。
func TestProviderNameFallback(t *testing.T) {
	if got := providerName(nil); got != "pgvector" {
		t.Fatalf("providerName(nil) = %q, want pgvector", got)
	}
	if got := providerName(&namedStubProvider{name: "   "}); got != "pgvector" {
		t.Fatalf("providerName(blank name) = %q, want pgvector", got)
	}
}

// TestServiceRunIndexJobPersistsExternalMapping P1-1 一致性收口：重建索引
// 必须把上次落库的 external id 透传给 provider（删旧建新的删除输入），并把
// 新 external id 回存——否则下次重建无从删旧，外部残留逐次累积。
func TestServiceRunIndexJobPersistsExternalMapping(t *testing.T) {
	docRepo := &memDocRepo{}
	jobRepo := &memJobRepo{}
	provider := &reindexingProvider{}
	svc := NewService(docRepo, jobRepo, nil, provider)

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Billing details",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if doc.ExternalID != "ext-1" {
		t.Fatalf("initial external id = %q, want ext-1", doc.ExternalID)
	}

	job, err := svc.QueueIndexJob(context.Background(), QueueIndexJobRequest{
		JobID:      "job-1",
		DocumentID: doc.ID,
	})
	if err != nil {
		t.Fatalf("queue job: %v", err)
	}
	if _, err := svc.RunIndexJob(context.Background(), RunIndexJobRequest{JobID: job.ID}); err != nil {
		t.Fatalf("run job: %v", err)
	}

	if len(provider.calls) != 2 {
		t.Fatalf("provider upsert calls = %d, want 2", len(provider.calls))
	}
	if last := provider.calls[1]; last.ExternalID != "ext-1" {
		t.Fatalf("reindex upsert external id = %q, want persisted ext-1", last.ExternalID)
	}
	stored, err := svc.GetDocument(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("get doc: %v", err)
	}
	if stored.ExternalID != "ext-2" {
		t.Fatalf("persisted external id = %q, want ext-2 (re-upsert result must be stored)", stored.ExternalID)
	}
}
