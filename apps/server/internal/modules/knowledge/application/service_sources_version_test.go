package application

// V1.0 收敛 B3-1a（docs/v1-convergence-plan.md §8.1/§8.2）：来源登记、
// 文档挂源与版本号、索引任务版本关联的服务层行为。

import (
	"context"
	"fmt"
	"testing"

	"servify/apps/server/internal/modules/knowledge/domain"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
)

type memSourceRepo struct {
	sources   map[uint]*domain.Source
	nextID    uint
	countDocs func(sourceID uint) int64
}

func (r *memSourceRepo) Create(ctx context.Context, source *domain.Source) error {
	if r.sources == nil {
		r.sources = map[uint]*domain.Source{}
	}
	r.nextID++
	source.ID = r.nextID
	cp := *source
	r.sources[source.ID] = &cp
	return nil
}
func (r *memSourceRepo) Update(ctx context.Context, source *domain.Source) error {
	if _, ok := r.sources[source.ID]; !ok {
		return fmt.Errorf("not found")
	}
	cp := *source
	r.sources[source.ID] = &cp
	return nil
}
func (r *memSourceRepo) Delete(ctx context.Context, id uint) error {
	if _, ok := r.sources[id]; !ok {
		return fmt.Errorf("not found")
	}
	delete(r.sources, id)
	return nil
}
func (r *memSourceRepo) Get(ctx context.Context, id uint) (*domain.Source, error) {
	source, ok := r.sources[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *source
	return &cp, nil
}
func (r *memSourceRepo) List(ctx context.Context, filter ListSourcesFilter) ([]domain.Source, error) {
	out := make([]domain.Source, 0)
	for _, source := range r.sources {
		if filter.Type == "" || source.Type == filter.Type {
			out = append(out, *source)
		}
	}
	return out, nil
}
func (r *memSourceRepo) CountDocuments(ctx context.Context, sourceID uint) (int64, error) {
	// 文档引用计数借 memDocRepo 的注册回调注入（见引用计数测试）。
	if r.countDocs != nil {
		return r.countDocs(sourceID), nil
	}
	return 0, nil
}

func TestServiceCreateDocumentWithSource(t *testing.T) {
	sources := &memSourceRepo{}
	docRepo := &memDocRepo{}
	svc := NewService(docRepo, &memJobRepo{}, sources, nil)

	source, err := svc.CreateSource(context.Background(), CreateSourceRequest{
		Name: "退款政策站", Type: "website", Description: "官网 FAQ 抓取",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID: "doc-src-1", Title: "Refund", Content: "Refund policy", SourceID: source.ID,
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if doc.SourceID != source.ID {
		t.Fatalf("source id = %d want %d", doc.SourceID, source.ID)
	}
	if doc.Version != 1 {
		t.Fatalf("initial version = %d want 1", doc.Version)
	}
	// 落库往返：Get 保留挂源与版本。
	loaded, err := svc.GetDocument(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("get doc: %v", err)
	}
	if loaded.SourceID != source.ID || loaded.Version != 1 {
		t.Fatalf("loaded source/version = %d/%d want %d/1", loaded.SourceID, loaded.Version, source.ID)
	}
}

func TestServiceCreateDocumentRejectsUnknownSource(t *testing.T) {
	svc := NewService(&memDocRepo{}, &memJobRepo{}, &memSourceRepo{}, nil)
	_, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		Title: "T", Content: "C", SourceID: 99,
	})
	if err == nil || err.Error() != "knowledge source 99 not found" {
		t.Fatalf("unexpected error: %v", err)
	}
	// sources 仓储未装配时挂源必须显式报错，不静默丢归属。
	svcNoSources := NewService(&memDocRepo{}, &memJobRepo{}, nil, nil)
	_, err = svcNoSources.CreateDocument(context.Background(), CreateDocumentRequest{
		Title: "T", Content: "C", SourceID: 1,
	})
	if err == nil || err.Error() != "knowledge sources repository is not configured" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceUpdateDocumentVersionBumpsOnlyOnContentChange(t *testing.T) {
	svc := NewService(&memDocRepo{}, &memJobRepo{}, &memSourceRepo{}, nil)
	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID: "doc-v", Title: "V", Content: "v1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	category := "policy"
	if _, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{Category: &category}); err != nil {
		t.Fatalf("metadata update: %v", err)
	}
	loaded, err := svc.GetDocument(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Version != 1 {
		t.Fatalf("metadata-only update bumped version to %d, want 1", loaded.Version)
	}

	content := "v2"
	if _, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{Content: &content}); err != nil {
		t.Fatalf("content update: %v", err)
	}
	if loaded, err = svc.GetDocument(context.Background(), doc.ID); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Version != 2 {
		t.Fatalf("content update version = %d want 2", loaded.Version)
	}

	title := "V2"
	if _, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{Title: &title}); err != nil {
		t.Fatalf("title update: %v", err)
	}
	if loaded, err = svc.GetDocument(context.Background(), doc.ID); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Version != 3 {
		t.Fatalf("title update version = %d want 3", loaded.Version)
	}

	same := "v2"
	if _, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{Content: &same}); err != nil {
		t.Fatalf("same-content update: %v", err)
	}
	if loaded, err = svc.GetDocument(context.Background(), doc.ID); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Version != 3 {
		t.Fatalf("no-op content update version = %d want 3", loaded.Version)
	}
}

func TestServiceSourceLifecycle(t *testing.T) {
	sources := &memSourceRepo{}
	docRepo := &memDocRepo{}
	svc := NewService(docRepo, &memJobRepo{}, sources, nil)

	if _, err := svc.CreateSource(context.Background(), CreateSourceRequest{Name: "x", Type: "video"}); err == nil {
		t.Fatal("expected unknown source type to be rejected")
	}
	source, err := svc.CreateSource(context.Background(), CreateSourceRequest{Name: "FAQ", Type: "faq"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := svc.ListSources(context.Background(), ListSourcesFilter{Type: "faq"})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v err %v", list, err)
	}
	if _, err := svc.ListSources(context.Background(), ListSourcesFilter{Type: "video"}); err == nil {
		t.Fatal("expected filter by unknown type to be rejected")
	}

	// 引用守卫：有文档挂载时拒绝删除。
	if _, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID: "doc-ref", Title: "T", Content: "C", SourceID: source.ID,
	}); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	sources.countDocs = func(sourceID uint) int64 { return 1 }
	if err := svc.DeleteSource(context.Background(), source.ID); err == nil {
		t.Fatal("expected delete to be rejected while referenced")
	}
	// 引用清空后可删。
	sources.countDocs = nil
	if err := svc.DeleteSource(context.Background(), source.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestRunIndexJobRecordsDocumentVersion(t *testing.T) {
	provider := &mockkp.Provider{}
	jobRepo := &memJobRepo{}
	svc := NewService(&memDocRepo{}, jobRepo, &memSourceRepo{}, provider)

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		ID: "doc-ix", Title: "Ix", Content: "v1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	content := "v2"
	if _, err := svc.UpdateDocument(context.Background(), doc.ID, UpdateDocumentRequest{Content: &content}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := svc.QueueIndexJob(context.Background(), QueueIndexJobRequest{JobID: "job-ix", DocumentID: doc.ID}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	result, err := svc.RunIndexJob(context.Background(), RunIndexJobRequest{JobID: "job-ix"})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if result.DocumentVersion != 2 {
		t.Fatalf("indexed version = %d want 2", result.DocumentVersion)
	}
	jobs, err := svc.ListIndexJobs(context.Background(), doc.ID, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %v err %v", jobs, err)
	}
	if jobs[0].DocumentVersion != 2 || jobs[0].Status != string(domain.IndexJobDone) {
		t.Fatalf("job = %+v want version 2 done", jobs[0])
	}
}
