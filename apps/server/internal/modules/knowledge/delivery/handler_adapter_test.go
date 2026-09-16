package delivery

import (
	"context"
	"fmt"
	"testing"
	"time"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+t.Name()+"")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&knowledgedomain.KnowledgeDoc{}, &knowledgedomain.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestHandlerServiceAdapterCRUD(t *testing.T) {
	db := newDeliveryTestDB(t)
	adapter := NewHandlerService(db)
	ctx := context.Background()

	created, err := adapter.Create(ctx, &knowledgeapp.KnowledgeDocCreateRequest{
		Title:    " Billing ",
		Content:  " Billing details ",
		Category: "faq",
		Tags:     []string{" billing ", "", "kb"},
		IsPublic: true,
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected persisted document id")
	}
	if created.Title != "Billing" || created.Content != "Billing details" {
		t.Fatalf("unexpected created doc: %+v", created)
	}
	if created.Tags != "billing,kb" {
		t.Fatalf("unexpected tags csv: %q", created.Tags)
	}
	if !created.IsPublic || created.Category != "faq" {
		t.Fatalf("unexpected flags: %+v", created)
	}

	docs, total, err := adapter.List(ctx, &knowledgeapp.KnowledgeDocListRequest{
		Page:       1,
		PageSize:   10,
		Category:   "faq",
		Search:     "billing",
		PublicOnly: true,
	})
	if err != nil {
		t.Fatalf("list docs: %v", err)
	}
	if total != 1 || len(docs) != 1 || docs[0].ID != created.ID {
		t.Fatalf("unexpected list result total=%d docs=%+v", total, docs)
	}

	docs, total, err = adapter.List(ctx, nil)
	if err != nil {
		t.Fatalf("list docs without request: %v", err)
	}
	if total != 1 || len(docs) != 1 {
		t.Fatalf("unexpected nil-request list total=%d docs=%+v", total, docs)
	}

	got, err := adapter.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get doc: %v", err)
	}
	if got.Title != "Billing" || got.Tags != "billing,kb" {
		t.Fatalf("unexpected fetched doc: %+v", got)
	}

	newTitle := "Billing v2"
	newTags := []string{" faq ", "  "}
	newPublic := false
	updated, err := adapter.Update(ctx, created.ID, &knowledgeapp.KnowledgeDocUpdateRequest{
		Title:    &newTitle,
		Tags:     &newTags,
		IsPublic: &newPublic,
	})
	if err != nil {
		t.Fatalf("update doc: %v", err)
	}
	if updated.Title != "Billing v2" || updated.Tags != "faq" || updated.IsPublic {
		t.Fatalf("unexpected updated doc: %+v", updated)
	}

	if err := adapter.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	if _, err := adapter.Get(ctx, created.ID); err == nil {
		t.Fatal("expected get error after delete")
	}
}

func TestHandlerServiceAdapterWithProvider(t *testing.T) {
	db := newDeliveryTestDB(t)
	provider := &mockkp.Provider{}
	adapter := NewHandlerServiceWithProvider(db, provider)
	ctx := context.Background()

	created, err := adapter.Create(ctx, &knowledgeapp.KnowledgeDocCreateRequest{
		Title:   "Synced",
		Content: "Synced content",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected persisted document id")
	}
	key := fmt.Sprintf("%d", created.ID)
	if provider.Documents[key].Title != "Synced" {
		t.Fatalf("expected provider upsert, got %+v", provider.Documents)
	}
}

func TestHandlerServiceAdapterRequestErrors(t *testing.T) {
	db := newDeliveryTestDB(t)
	adapter := NewHandlerService(db)
	ctx := context.Background()

	if _, err := adapter.Create(ctx, nil); err == nil || err.Error() != "request required" {
		t.Fatalf("expected create request error, got %v", err)
	}
	if _, err := adapter.Update(ctx, 1, nil); err == nil || err.Error() != "request required" {
		t.Fatalf("expected update request error, got %v", err)
	}
	if _, err := adapter.Create(ctx, &knowledgeapp.KnowledgeDocCreateRequest{Title: "", Content: "c"}); err == nil {
		t.Fatal("expected create validation error")
	}
	if _, err := adapter.Get(ctx, 9999); err == nil {
		t.Fatal("expected get not found error")
	}
	if _, err := adapter.Update(ctx, 9999, &knowledgeapp.KnowledgeDocUpdateRequest{}); err == nil {
		t.Fatal("expected update not found error")
	}
	if err := adapter.Delete(ctx, 9999); err == nil {
		t.Fatal("expected delete not found error")
	}
}

type stubDocRepo struct {
	docs     map[string]*knowledgedomain.Document
	nextID   string
	listErr  error
	nilGetID string
}

func (r *stubDocRepo) seed(doc *knowledgedomain.Document) {
	if r.docs == nil {
		r.docs = map[string]*knowledgedomain.Document{}
	}
	r.docs[doc.ID] = doc
}

func (r *stubDocRepo) Create(ctx context.Context, doc *knowledgedomain.Document) error {
	if doc.ID == "" {
		doc.ID = r.nextID
	}
	r.seed(doc)
	return nil
}

func (r *stubDocRepo) Update(ctx context.Context, doc *knowledgedomain.Document) error {
	r.seed(doc)
	return nil
}

func (r *stubDocRepo) Delete(ctx context.Context, id string) error {
	delete(r.docs, id)
	return nil
}

func (r *stubDocRepo) Get(ctx context.Context, id string) (*knowledgedomain.Document, error) {
	if id == r.nilGetID {
		return nil, nil
	}
	doc, ok := r.docs[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return doc, nil
}

func (r *stubDocRepo) List(ctx context.Context, filter knowledgeapp.ListDocumentsFilter) ([]knowledgedomain.Document, int64, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	out := make([]knowledgedomain.Document, 0, len(r.docs))
	for _, doc := range r.docs {
		out = append(out, *doc)
	}
	return out, int64(len(out)), nil
}

func TestHandlerServiceAdapterInvalidDomainIDs(t *testing.T) {
	repo := &stubDocRepo{nextID: "not-a-number"}
	adapter := NewHandlerServiceAdapter(knowledgeapp.NewService(repo, nil, nil))
	ctx := context.Background()

	if _, err := adapter.Create(ctx, &knowledgeapp.KnowledgeDocCreateRequest{Title: "t", Content: "c"}); err == nil {
		t.Fatal("expected invalid document id error on create")
	}

	repo.seed(&knowledgedomain.Document{ID: "bad", Title: "t", Content: "c", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	repo.docs["7"] = repo.docs["bad"]
	if _, _, err := adapter.List(ctx, &knowledgeapp.KnowledgeDocListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected invalid document id error on list")
	}
	if _, err := adapter.Get(ctx, 7); err == nil {
		t.Fatal("expected invalid document id error on get")
	}
	if _, err := adapter.Update(ctx, 7, &knowledgeapp.KnowledgeDocUpdateRequest{}); err == nil {
		t.Fatal("expected invalid document id error on update")
	}
}

func TestHandlerServiceAdapterNilDomainDoc(t *testing.T) {
	repo := &stubDocRepo{nilGetID: "5"}
	adapter := NewHandlerServiceAdapter(knowledgeapp.NewService(repo, nil, nil))

	doc, err := adapter.Get(context.Background(), 5)
	if err != nil {
		t.Fatalf("expected no error for nil domain doc, got %v", err)
	}
	if doc != nil {
		t.Fatalf("expected nil model doc, got %+v", doc)
	}
}

func TestJoinTagsCSV(t *testing.T) {
	if got := joinTagsCSV(nil); got != "" {
		t.Fatalf("expected empty csv, got %q", got)
	}
	if got := joinTagsCSV([]string{" a ", " ", "", "b"}); got != "a,b" {
		t.Fatalf("expected trimmed csv, got %q", got)
	}
}

func TestHandlerServiceAdapterListServiceError(t *testing.T) {
	repo := &stubDocRepo{listErr: fmt.Errorf("list boom")}
	adapter := NewHandlerServiceAdapter(knowledgeapp.NewService(repo, nil, nil))

	if _, _, err := adapter.List(context.Background(), &knowledgeapp.KnowledgeDocListRequest{Page: 1, PageSize: 10}); err == nil || err.Error() != "list boom" {
		t.Fatalf("expected list error, got %v", err)
	}
}
