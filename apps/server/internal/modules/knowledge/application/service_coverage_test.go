package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/knowledgeprovider"
	memorykp "servify/apps/server/internal/platform/knowledgeprovider/memory"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
)

type flakyDocRepo struct {
	memDocRepo
	createErr error
	deleteErr error
	getErr    error
	listErr   error

	updateCalls    int
	failUpdateCall int
	updateErr      error
}

func (r *flakyDocRepo) Create(ctx context.Context, doc *domain.Document) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.memDocRepo.Create(ctx, doc)
}

func (r *flakyDocRepo) Update(ctx context.Context, doc *domain.Document) error {
	r.updateCalls++
	if r.updateErr != nil || (r.failUpdateCall > 0 && r.updateCalls >= r.failUpdateCall) {
		if r.updateErr != nil {
			return r.updateErr
		}
		return fmt.Errorf("update failed on call %d", r.updateCalls)
	}
	return r.memDocRepo.Update(ctx, doc)
}

func (r *flakyDocRepo) Delete(ctx context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return r.memDocRepo.Delete(ctx, id)
}

func (r *flakyDocRepo) Get(ctx context.Context, id string) (*domain.Document, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.memDocRepo.Get(ctx, id)
}

func (r *flakyDocRepo) List(ctx context.Context, filter ListDocumentsFilter) ([]domain.Document, int64, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.memDocRepo.List(ctx, filter)
}

type flakyJobRepo struct {
	memJobRepo
	createErr error
	getErr    error

	updateCalls int
	failUpdate  int
}

func (r *flakyJobRepo) Create(ctx context.Context, job *domain.IndexJob) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.memJobRepo.Create(ctx, job)
}

func (r *flakyJobRepo) Update(ctx context.Context, job *domain.IndexJob) error {
	r.updateCalls++
	if r.failUpdate > 0 && r.updateCalls >= r.failUpdate {
		return fmt.Errorf("update failed on call %d", r.updateCalls)
	}
	return r.memJobRepo.Update(ctx, job)
}

func (r *flakyJobRepo) Get(ctx context.Context, id string) (*domain.IndexJob, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.memJobRepo.Get(ctx, id)
}

type recordingDocRepo struct {
	memDocRepo
	captured []ListDocumentsFilter
}

func (r *recordingDocRepo) List(ctx context.Context, filter ListDocumentsFilter) ([]domain.Document, int64, error) {
	r.captured = append(r.captured, filter)
	return r.memDocRepo.List(ctx, filter)
}

type failingUpsertProvider struct {
	mockkp.Provider
	upsertErr error
}

func (p *failingUpsertProvider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if p.upsertErr != nil {
		return "", p.upsertErr
	}
	return p.Provider.UpsertDocument(ctx, doc)
}

func TestServiceCreateDocumentValidationErrors(t *testing.T) {
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil)
	ctx := context.Background()

	if _, err := svc.CreateDocument(ctx, CreateDocumentRequest{Title: "  ", Content: "c"}); err == nil || err.Error() != "title required" {
		t.Fatalf("expected title required, got %v", err)
	}
	if _, err := svc.CreateDocument(ctx, CreateDocumentRequest{Title: "t", Content: " "}); err == nil || err.Error() != "content required" {
		t.Fatalf("expected content required, got %v", err)
	}
}

func TestServiceCreateDocumentRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	req := CreateDocumentRequest{Title: "t", Content: "c"}

	repo := &flakyDocRepo{createErr: errors.New("create boom")}
	if _, err := NewService(repo, &memJobRepo{}, nil).CreateDocument(ctx, req); err == nil || err.Error() != "create boom" {
		t.Fatalf("expected create error, got %v", err)
	}

	repo = &flakyDocRepo{updateErr: errors.New("update boom")}
	if _, err := NewService(repo, &memJobRepo{}, nil).CreateDocument(ctx, req); err == nil || err.Error() != "update boom" {
		t.Fatalf("expected update error, got %v", err)
	}

	provider := &failingUpsertProvider{upsertErr: errors.New("upsert boom")}
	if _, err := NewService(&flakyDocRepo{}, &memJobRepo{}, provider).CreateDocument(ctx, req); err == nil || err.Error() != "upsert boom" {
		t.Fatalf("expected sync error, got %v", err)
	}
}

func TestServiceUpdateDocumentErrorPaths(t *testing.T) {
	ctx := context.Background()

	notFound := &flakyDocRepo{getErr: errors.New("not found")}
	if _, err := NewService(notFound, &memJobRepo{}, nil).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{}); err == nil || err.Error() != "not found" {
		t.Fatalf("expected get error, got %v", err)
	}

	seeded := &memDocRepo{}
	seeded.docs = map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}

	emptyTitle := ""
	if _, err := NewService(seeded, &memJobRepo{}, nil).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{Title: &emptyTitle}); err == nil || err.Error() != "title required" {
		t.Fatalf("expected title required, got %v", err)
	}
	emptyContent := ""
	if _, err := NewService(seeded, &memJobRepo{}, nil).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{Content: &emptyContent}); err == nil || err.Error() != "content required" {
		t.Fatalf("expected content required, got %v", err)
	}

	updateFails := &flakyDocRepo{memDocRepo: memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}, updateErr: errors.New("update boom")}
	if _, err := NewService(updateFails, &memJobRepo{}, nil).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{}); err == nil || err.Error() != "update boom" {
		t.Fatalf("expected update error, got %v", err)
	}

	syncFails := &flakyDocRepo{memDocRepo: memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}}
	provider := &failingUpsertProvider{upsertErr: errors.New("upsert boom")}
	if _, err := NewService(syncFails, &memJobRepo{}, provider).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{}); err == nil || err.Error() != "upsert boom" {
		t.Fatalf("expected sync error, got %v", err)
	}

	allFields := &memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}
	newCategory := "guide"
	newIsPublic := true
	updatedAll, err := NewService(allFields, &memJobRepo{}, nil).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{
		Category: &newCategory,
		IsPublic: &newIsPublic,
	})
	if err != nil {
		t.Fatalf("update all fields: %v", err)
	}
	if updatedAll.Category != "guide" || !updatedAll.IsPublic {
		t.Fatalf("unexpected updated doc: %+v", updatedAll)
	}

	secondUpdateFails := &flakyDocRepo{memDocRepo: memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}, failUpdateCall: 2}
	if _, err := NewService(secondUpdateFails, &memJobRepo{}, nil).UpdateDocument(ctx, "doc-1", UpdateDocumentRequest{}); err == nil || err.Error() != "update failed on call 2" {
		t.Fatalf("expected second update error, got %v", err)
	}
}

func TestServiceDeleteDocumentErrorPaths(t *testing.T) {
	ctx := context.Background()

	getFails := &flakyDocRepo{getErr: errors.New("not found")}
	if err := NewService(getFails, &memJobRepo{}, nil).DeleteDocument(ctx, "doc-1"); err == nil || err.Error() != "not found" {
		t.Fatalf("expected get error, got %v", err)
	}

	docRepo := &memDocRepo{}
	if _, err := NewService(docRepo, &memJobRepo{}, nil).CreateDocument(ctx, CreateDocumentRequest{ID: "doc-1", Title: "t", Content: "c"}); err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	provider := &mockkp.Provider{}
	if err := NewService(docRepo, &memJobRepo{}, provider).DeleteDocument(ctx, "doc-1"); err == nil || err.Error() != "knowledge provider deletion is not supported: missing external document id" {
		t.Fatalf("expected missing external id error, got %v", err)
	}

	docRepo = &memDocRepo{}
	synced, err := NewService(docRepo, &memJobRepo{}, &mockkp.Provider{}).CreateDocument(ctx, CreateDocumentRequest{ID: "doc-2", Title: "t", Content: "c"})
	if err != nil {
		t.Fatalf("seed synced doc: %v", err)
	}
	deleteFails := &mockkp.Provider{DeleteError: errors.New("delete boom")}
	if err := NewService(docRepo, &memJobRepo{}, deleteFails).DeleteDocument(ctx, synced.ID); err == nil || err.Error() != "delete boom" {
		t.Fatalf("expected provider delete error, got %v", err)
	}
}

func TestServiceListDocumentsNormalizesPagination(t *testing.T) {
	repo := &recordingDocRepo{}
	svc := NewService(repo, &memJobRepo{}, nil)

	cases := []struct {
		in   ListDocumentsFilter
		want ListDocumentsFilter
	}{
		{ListDocumentsFilter{Page: 0, PageSize: 0}, ListDocumentsFilter{Page: 1, PageSize: 20}},
		{ListDocumentsFilter{Page: -3, PageSize: -1}, ListDocumentsFilter{Page: 1, PageSize: 20}},
		{ListDocumentsFilter{Page: 2, PageSize: 500}, ListDocumentsFilter{Page: 2, PageSize: 100}},
	}
	for _, tc := range cases {
		if _, _, err := svc.ListDocuments(context.Background(), tc.in); err != nil {
			t.Fatalf("list docs: %v", err)
		}
		got := repo.captured[len(repo.captured)-1]
		if got.Page != tc.want.Page || got.PageSize != tc.want.PageSize {
			t.Fatalf("filter = %+v want %+v", got, tc.want)
		}
	}

	listFails := &flakyDocRepo{listErr: errors.New("list boom")}
	if _, _, err := NewService(listFails, &memJobRepo{}, nil).ListDocuments(context.Background(), ListDocumentsFilter{}); err == nil || err.Error() != "list boom" {
		t.Fatalf("expected list error, got %v", err)
	}
}

func TestServiceQueueIndexJobErrorPaths(t *testing.T) {
	ctx := context.Background()

	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil)
	if _, err := svc.QueueIndexJob(ctx, QueueIndexJobRequest{JobID: "job-1", DocumentID: " "}); err == nil || err.Error() != "document id required" {
		t.Fatalf("expected document id required, got %v", err)
	}

	createFails := &flakyJobRepo{createErr: errors.New("job create boom")}
	if _, err := NewService(&memDocRepo{}, createFails, nil).QueueIndexJob(ctx, QueueIndexJobRequest{JobID: "job-1", DocumentID: "doc-1"}); err == nil || err.Error() != "job create boom" {
		t.Fatalf("expected job create error, got %v", err)
	}
}

func TestServiceRunIndexJobErrorPaths(t *testing.T) {
	ctx := context.Background()

	jobGetFails := &flakyJobRepo{getErr: errors.New("job not found")}
	if _, err := NewService(&memDocRepo{}, jobGetFails, nil).RunIndexJob(ctx, RunIndexJobRequest{JobID: "job-1"}); err == nil || err.Error() != "job not found" {
		t.Fatalf("expected job get error, got %v", err)
	}

	docGetFails := &flakyDocRepo{getErr: errors.New("doc not found")}
	if _, err := NewService(docGetFails, &memJobRepo{jobs: map[string]*domain.IndexJob{
		"job-1": {ID: "job-1", DocumentID: "doc-1", Status: domain.IndexJobQueued},
	}}, nil).RunIndexJob(ctx, RunIndexJobRequest{JobID: "job-1"}); err == nil || err.Error() != "doc not found" {
		t.Fatalf("expected doc get error, got %v", err)
	}

	updateFails := &flakyJobRepo{failUpdate: 1}
	updateFails.jobs = map[string]*domain.IndexJob{
		"job-1": {ID: "job-1", DocumentID: "doc-1", Status: domain.IndexJobQueued},
	}
	if _, err := NewService(&memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}, updateFails, nil).RunIndexJob(ctx, RunIndexJobRequest{JobID: "job-1"}); err == nil || err.Error() != "update failed on call 1" {
		t.Fatalf("expected running update error, got %v", err)
	}

	finalUpdateFails := &flakyJobRepo{failUpdate: 2}
	finalUpdateFails.jobs = map[string]*domain.IndexJob{
		"job-1": {ID: "job-1", DocumentID: "doc-1", Status: domain.IndexJobQueued},
	}
	if _, err := NewService(&memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}, finalUpdateFails, nil).RunIndexJob(ctx, RunIndexJobRequest{JobID: "job-1"}); err == nil || err.Error() != "update failed on call 2" {
		t.Fatalf("expected final update error, got %v", err)
	}

	docRepo := &memDocRepo{docs: map[string]*domain.Document{
		"doc-1": {ID: "doc-1", Title: "t", Content: "c"},
	}}
	jobRepo := &flakyJobRepo{}
	provider := &failingUpsertProvider{upsertErr: errors.New("upsert boom")}
	svc := NewService(docRepo, jobRepo, provider)
	if _, err := svc.QueueIndexJob(ctx, QueueIndexJobRequest{JobID: "job-1", DocumentID: "doc-1"}); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	result, err := svc.RunIndexJob(ctx, RunIndexJobRequest{JobID: "job-1"})
	if err == nil || err.Error() != "upsert boom" {
		t.Fatalf("expected provider upsert error, got %v", err)
	}
	if result == nil || result.Status != string(domain.IndexJobFailed) || result.Error != "upsert boom" || result.CompletedAt != nil {
		t.Fatalf("unexpected failed result: %+v", result)
	}
	stored, err := jobRepo.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("refresh job: %v", err)
	}
	if stored.Status != domain.IndexJobFailed || stored.Error != "upsert boom" {
		t.Fatalf("unexpected stored job: %+v", stored)
	}
}

func TestServiceSyncDocumentDirect(t *testing.T) {
	svc := NewService(&memDocRepo{}, &memJobRepo{}, nil)
	if err := svc.syncDocument(context.Background(), nil); err != nil {
		t.Fatalf("expected nil doc sync to be a no-op, got %v", err)
	}
}

func TestProviderIdentity(t *testing.T) {
	if got := providerIdentity(nil); got != "" {
		t.Fatalf("expected empty identity for nil provider, got %q", got)
	}
	if got := providerIdentity(&mockkp.Provider{}); got != "mock.Provider" {
		t.Fatalf("unexpected mock identity: %q", got)
	}
	if got := providerIdentity(memorykp.NewProvider("tenant-a", "kb-a")); got != "memory.Provider" {
		t.Fatalf("unexpected memory identity: %q", got)
	}
}
