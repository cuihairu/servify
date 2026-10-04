package application

// 覆盖率补充：service.go 来源登记与索引任务列表的错误/归一化分支——
// UpdateDocument 挂源校验、CreateSource 参数与仓储错误、ListSources/
// DeleteSource 未装配仓储、ListIndexJobs 参数归一化与仓储错误。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/modules/knowledge/domain"
)

// erringSourceRepo 在 memSourceRepo 之上注入可配置错误，驱动服务层错误分支。
type erringSourceRepo struct {
	*memSourceRepo
	createErr error
	getErr    error
	countErr  error
}

func (r *erringSourceRepo) Create(ctx context.Context, source *domain.Source) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.memSourceRepo.Create(ctx, source)
}

func (r *erringSourceRepo) Get(ctx context.Context, id uint) (*domain.Source, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.memSourceRepo.Get(ctx, id)
}

func (r *erringSourceRepo) CountDocuments(ctx context.Context, sourceID uint) (int64, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.memSourceRepo.CountDocuments(ctx, sourceID)
}

// erringJobRepo 注入 ListByDocument 错误。
type erringJobRepo struct {
	*memJobRepo
	listErr error
}

func (r *erringJobRepo) ListByDocument(ctx context.Context, documentID string, limit int) ([]domain.IndexJob, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.memJobRepo.ListByDocument(ctx, documentID, limit)
}

func TestServiceUpdateDocumentSourceAssignmentExtra(t *testing.T) {
	sources := &memSourceRepo{}
	docRepo := &memDocRepo{}
	svc := NewService(docRepo, &memJobRepo{}, sources, nil)
	ctx := context.Background()

	source, err := svc.CreateSource(ctx, CreateSourceRequest{Name: "帮助中心", Type: "faq"})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	doc, err := svc.CreateDocument(ctx, CreateDocumentRequest{ID: "doc-us", Title: "T", Content: "C"})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}

	// 挂未知来源：validateSource 错误上浮，归属不变。
	missing := uint(99)
	if _, err := svc.UpdateDocument(ctx, doc.ID, UpdateDocumentRequest{SourceID: &missing}); err == nil || err.Error() != "knowledge source 99 not found" {
		t.Fatalf("unexpected error: %v", err)
	}
	// sources 仓储未装配时挂源必须显式报错，不静默丢归属。
	noSources := NewService(docRepo, &memJobRepo{}, nil, nil)
	if _, err := noSources.UpdateDocument(ctx, doc.ID, UpdateDocumentRequest{SourceID: &missing}); err == nil || err.Error() != "knowledge sources repository is not configured" {
		t.Fatalf("unexpected error: %v", err)
	}

	// 挂已登记来源：归属写回。
	if _, err := svc.UpdateDocument(ctx, doc.ID, UpdateDocumentRequest{SourceID: &source.ID}); err != nil {
		t.Fatalf("update with source: %v", err)
	}
	loaded, err := svc.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("get doc: %v", err)
	}
	if loaded.SourceID != source.ID {
		t.Fatalf("source id = %d want %d", loaded.SourceID, source.ID)
	}
}

func TestServiceSourceErrorPathsExtra(t *testing.T) {
	ctx := context.Background()

	// CreateSource：空名称拒绝。
	svc := NewService(&memDocRepo{}, &memJobRepo{}, &memSourceRepo{}, nil)
	if _, err := svc.CreateSource(ctx, CreateSourceRequest{Name: "  ", Type: "faq"}); err == nil || err.Error() != "source name required" {
		t.Fatalf("unexpected error: %v", err)
	}

	// CreateSource：仓储写入失败上浮。
	failing := &erringSourceRepo{memSourceRepo: &memSourceRepo{}, createErr: errors.New("create boom")}
	svcFailing := NewService(&memDocRepo{}, &memJobRepo{}, failing, nil)
	if _, err := svcFailing.CreateSource(ctx, CreateSourceRequest{Name: "x", Type: "faq"}); err == nil || err.Error() != "create boom" {
		t.Fatalf("unexpected error: %v", err)
	}

	// ListSources / DeleteSource：仓储未装配。
	noSources := NewService(&memDocRepo{}, &memJobRepo{}, nil, nil)
	if _, err := noSources.ListSources(ctx, ListSourcesFilter{}); err == nil || err.Error() != "knowledge sources repository is not configured" {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := noSources.DeleteSource(ctx, 1); err == nil || err.Error() != "knowledge sources repository is not configured" {
		t.Fatalf("unexpected error: %v", err)
	}

	// DeleteSource：来源不存在。
	if err := svc.DeleteSource(ctx, 7); err == nil {
		t.Fatal("expected not found error")
	}

	// DeleteSource：引用计数查询失败上浮。
	sources := &erringSourceRepo{memSourceRepo: &memSourceRepo{}}
	svcCountErr := NewService(&memDocRepo{}, &memJobRepo{}, sources, nil)
	source, err := svcCountErr.CreateSource(ctx, CreateSourceRequest{Name: "x", Type: "api"})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	sources.countErr = errors.New("count boom")
	if err := svcCountErr.DeleteSource(ctx, source.ID); err == nil || err.Error() != "count boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceListIndexJobsExtra(t *testing.T) {
	ctx := context.Background()

	// 空 documentID 拒绝。
	svc := NewService(&memDocRepo{}, &memJobRepo{}, &memSourceRepo{}, nil)
	if _, err := svc.ListIndexJobs(ctx, "   ", 10); err == nil || err.Error() != "document id required" {
		t.Fatalf("unexpected error: %v", err)
	}

	// limit<=0 归一化为默认 20；limit>100 收敛到 100（空仓储即可走通）。
	for _, limit := range []int{0, -3, 500} {
		if jobs, err := svc.ListIndexJobs(ctx, "doc-1", limit); err != nil || jobs == nil {
			t.Fatalf("limit %d: jobs=%v err=%v", limit, jobs, err)
		}
	}

	// ListByDocument 仓储错误上浮。
	jobRepo := &erringJobRepo{memJobRepo: &memJobRepo{}, listErr: errors.New("list boom")}
	svcListErr := NewService(&memDocRepo{}, jobRepo, &memSourceRepo{}, nil)
	if _, err := svcListErr.ListIndexJobs(ctx, "doc-1", 10); err == nil || err.Error() != "list boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}
